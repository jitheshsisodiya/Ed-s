import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:provider/provider.dart';

import '../core/app_exception.dart';
import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/models/models.dart';
import '../core/vpn_controller.dart';
import '../widgets/connect_button.dart';
import '../widgets/device_tile.dart';
import '../widgets/error_view.dart';
import '../widgets/loading_indicator.dart';

/// The screen the app opens on: one control that connects and disconnects,
/// the network it acts on, and who else is reachable.
///
/// Everything else — creating networks, invites, members, per-device
/// management — lives one tap away, because none of it is what someone opens
/// the app to do.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late Future<_HomeData> _future;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<_HomeData> _load() async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final prefs = context.read<Preferences>();

    final networks = await api.listNetworks();
    if (networks.isEmpty) {
      return const _HomeData(networks: [], target: null, self: null, peers: []);
    }

    // One network needs no choosing; otherwise the last one used.
    final target = networks.length == 1
        ? networks.first
        : networks.firstWhere(
            (n) => n.id == prefs.lastNetworkId,
            orElse: () => networks.first,
          );

    final keys = await vpn.ensureKeypair();
    final devices = await api.listNetworkDevices(target.id);
    Device? self;
    for (final d in devices) {
      if (d.publicKey == keys.publicKey) {
        self = d;
        break;
      }
    }

    return _HomeData(
      networks: networks,
      target: target,
      self: self,
      peers: devices.where((d) => d.id != self?.id).toList(),
    );
  }

  Future<void> _refresh() async {
    final future = _load();
    setState(() => _future = future);
    await future;
  }

  String get _platformOs {
    if (kIsWeb) return 'web';
    if (Platform.isAndroid) return 'android';
    if (Platform.isIOS) return 'ios';
    if (Platform.isMacOS) return 'macos';
    if (Platform.isWindows) return 'windows';
    if (Platform.isLinux) return 'linux';
    return 'unknown';
  }

  /// Registers this phone on [network] so it has a key and an address there.
  /// Nobody is asked to do this: it happens on the way to connecting.
  Future<Device> _registerThisDevice(Network network) async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final keys = await vpn.ensureKeypair();
    var name = await AppSettings.getDeviceName(fallback: '');
    if (name.isEmpty) {
      try {
        final info = await PackageInfo.fromPlatform();
        name = '${info.appName} on $_platformOs';
      } catch (_) {
        name = 'My Device';
      }
    }
    return api.registerDevice(
      network.id,
      name: name,
      os: _platformOs,
      publicKey: keys.publicKey,
    );
  }

  Future<void> _toggle(_HomeData data) async {
    final vpn = context.read<VpnController>();
    final prefs = context.read<Preferences>();
    final messenger = ScaffoldMessenger.of(context);
    final network = data.target;
    if (network == null) return;

    setState(() => _busy = true);
    try {
      if (vpn.state == AppVpnState.connected ||
          vpn.state == AppVpnState.reconnecting) {
        await vpn.disconnect();
        return;
      }
      final self = data.self ?? await _registerThisDevice(network);
      await prefs.setLastNetworkId(network.id);
      await vpn.connect(network: network, selfDevice: self);
      if (data.self == null && mounted) await _refresh();
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_humanError(e))));
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(_humanError(e))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('NexusVPN'),
        actions: [
          IconButton(
            icon: const Icon(Icons.hub_outlined),
            tooltip: 'Networks',
            onPressed: () => context.push('/networks'),
          ),
          IconButton(
            icon: const Icon(Icons.settings_outlined),
            tooltip: 'Settings',
            onPressed: () => context.push('/settings'),
          ),
        ],
      ),
      body: FutureBuilder<_HomeData>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const LoadingIndicator();
          }
          if (snapshot.hasError) {
            return ErrorView(
              message: _humanError(snapshot.error),
              onRetry: _refresh,
            );
          }
          final data = snapshot.data!;
          if (data.target == null) return _NoNetworks(onChanged: _refresh);

          return RefreshIndicator(
            onRefresh: _refresh,
            child: Consumer2<VpnController, Preferences>(
              builder: (context, vpn, prefs, _) {
                final connected = vpn.state == AppVpnState.connected ||
                    vpn.state == AppVpnState.reconnecting;
                final connecting =
                    _busy || vpn.state == AppVpnState.connecting;

                return ListView(
                  padding: const EdgeInsets.fromLTRB(16, 24, 16, 32),
                  children: [
                    Center(
                      child: ConnectButton(
                        connected: connected,
                        busy: connecting,
                        onPressed: connecting ? null : () => _toggle(data),
                      ),
                    ),
                    const SizedBox(height: 24),
                    Center(
                      child: Text(
                        connected
                            ? data.target!.name
                            : connecting
                                ? 'Connecting'
                                : 'Not connected',
                        style: Theme.of(context).textTheme.headlineSmall,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Center(
                      child: Text(
                        _subtitle(data, connected, connecting),
                        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                              color: Theme.of(context).colorScheme.outline,
                            ),
                        textAlign: TextAlign.center,
                      ),
                    ),
                    if (vpn.lastError != null &&
                        vpn.state == AppVpnState.error) ...[
                      const SizedBox(height: 16),
                      _Banner(message: vpn.lastError!),
                    ],
                    const SizedBox(height: 28),
                    _DevicesSection(
                      peers: data.peers,
                      connected: connected,
                      advanced: prefs.advanced,
                      onManage: () =>
                          context.push('/networks/${data.target!.id}/devices'),
                    ),
                    if (prefs.advanced && data.self != null) ...[
                      const SizedBox(height: 20),
                      _AdvancedCard(network: data.target!, self: data.self!),
                    ],
                  ],
                );
              },
            ),
          );
        },
      ),
    );
  }

  String _subtitle(_HomeData data, bool connected, bool connecting) {
    if (connecting) return 'Setting up a secure tunnel…';
    if (connected) {
      final online = data.peers.where((d) => d.isOnline).length;
      return 'Secure · $online of ${data.peers.length} devices online';
    }
    return 'Connect to ${data.target!.name}';
  }
}

/// Turns whatever went wrong into something a person can act on.
String _humanError(Object? error) {
  if (error is ApiException) {
    if (error.isNetworkFailure) {
      return "Can't reach your server. Check that you are online and that the "
          'server address in Settings is right.';
    }
    if (error.isUnauthorized) {
      return 'Your session has expired. Sign in again to continue.';
    }
    return error.message;
  }
  final raw = error?.toString() ?? 'Something went wrong.';
  if (raw.contains('permission')) {
    return 'NexusVPN needs your permission to create a VPN connection. '
        'Allow it when your phone asks, then try again.';
  }
  return raw;
}

class _HomeData {
  const _HomeData({
    required this.networks,
    required this.target,
    required this.self,
    required this.peers,
  });

  final List<Network> networks;
  final Network? target;
  final Device? self;
  final List<Device> peers;
}

class _NoNetworks extends StatelessWidget {
  const _NoNetworks({required this.onChanged});

  final Future<void> Function() onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.hub_outlined, size: 44, color: theme.colorScheme.outline),
            const SizedBox(height: 16),
            Text('No networks yet', style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              'Create one for your own devices, or join a friend’s with '
              'their invite code.',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.outline,
              ),
            ),
            const SizedBox(height: 20),
            FilledButton(
              onPressed: () async {
                await context.push('/networks');
                await onChanged();
              },
              child: const Text('Get started'),
            ),
          ],
        ),
      ),
    );
  }
}

class _DevicesSection extends StatelessWidget {
  const _DevicesSection({
    required this.peers,
    required this.connected,
    required this.advanced,
    required this.onManage,
  });

  final List<Device> peers;
  final bool connected;
  final bool advanced;
  final VoidCallback onManage;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text('Devices', style: theme.textTheme.titleMedium),
            const Spacer(),
            TextButton(onPressed: onManage, child: const Text('Manage')),
          ],
        ),
        if (peers.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 20),
            child: Text(
              'No other devices yet. Install NexusVPN on another computer '
              'and sign in with the same account — it will appear here.',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.outline,
              ),
            ),
          )
        else
          Card(
            child: Column(
              children: [
                for (final peer in peers)
                  DeviceTile(device: peer, advanced: advanced),
              ],
            ),
          ),
      ],
    );
  }
}

class _AdvancedCard extends StatelessWidget {
  const _AdvancedCard({required this.network, required this.self});

  final Network network;
  final Device self;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Connection details', style: theme.textTheme.titleMedium),
            const SizedBox(height: 10),
            _Kv('Your address', self.virtualIp ?? 'Not assigned yet'),
            _Kv('Network range', network.cidr),
            _Kv('Interface', kWgInterfaceName),
            if (self.natType != null) _Kv('NAT type', self.natType!),
            if (network.dnsServers.isNotEmpty)
              _Kv('DNS', network.dnsServers.join(', ')),
          ],
        ),
      ),
    );
  }
}

class _Kv extends StatelessWidget {
  const _Kv(this.label, this.value);

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 130,
            child: Text(
              label,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.outline,
              ),
            ),
          ),
          Expanded(
            child: SelectableText(value, style: theme.textTheme.bodySmall),
          ),
        ],
      ),
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.error_outline, color: theme.colorScheme.onErrorContainer),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              _humanError(message),
              style: TextStyle(color: theme.colorScheme.onErrorContainer),
            ),
          ),
        ],
      ),
    );
  }
}
