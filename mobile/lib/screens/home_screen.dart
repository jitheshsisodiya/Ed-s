import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:provider/provider.dart';

import '../core/app_exception.dart';
import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/deck_theme.dart';
import '../core/models/models.dart';
import '../core/vpn_controller.dart';
import '../widgets/device_sheet.dart';
import '../widgets/loading_indicator.dart';
import '../widgets/network_tree.dart';
import '../widgets/reactor_core.dart';
import '../widgets/scrambler.dart';

/// The deck.
///
/// An identity strip carrying the one switch and this device's address, then
/// the tree of networks and machines filling everything below it. Same
/// structure as the desktop, sized for a thumb.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late Future<_DeckData> _future;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<_DeckData> _load() async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final prefs = context.read<Preferences>();

    final networks = await api.listNetworks();
    if (networks.isEmpty) {
      return const _DeckData(networks: [], target: null, self: null, devices: []);
    }

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

    return _DeckData(
      networks: networks,
      target: target,
      self: self,
      devices: devices,
      publicKey: keys.publicKey,
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

  /// Registers this device on a network so it has a key and an address
  /// there. Nobody is asked to do this; it happens on the way to connecting.
  Future<Device> _register(Network network) async {
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

  Future<void> _connect(_DeckData data, Network network) async {
    final vpn = context.read<VpnController>();
    final prefs = context.read<Preferences>();
    final messenger = ScaffoldMessenger.of(context);

    setState(() => _busy = true);
    try {
      final self = data.target?.id == network.id && data.self != null
          ? data.self!
          : await _register(network);
      await prefs.setLastNetworkId(network.id);
      await vpn.connect(network: network, selfDevice: self);
      if (mounted) await _refresh();
    } catch (e) {
      messenger.showSnackBar(_snack(humanError(e)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _disconnect() async {
    final vpn = context.read<VpnController>();
    setState(() => _busy = true);
    try {
      await vpn.disconnect();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: Deck.systemOverlay,
      child: Scaffold(
        body: SafeArea(
          child: FutureBuilder<_DeckData>(
            future: _future,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const LoadingIndicator();
              }
              if (snapshot.hasError) {
                return _ErrorPane(
                  message: humanError(snapshot.error),
                  onRetry: _refresh,
                );
              }
              final data = snapshot.data!;

              return Consumer2<VpnController, Preferences>(
                builder: (context, vpn, prefs, _) {
                  final phase = _phaseOf(vpn.state, _busy);
                  final connected = phase == DeckPhase.active;

                  return Column(
                    children: [
                      _TopBar(phase: phase, onMenu: () => _showMenu(context, data)),
                      _IdentityStrip(
                        phase: phase,
                        name: data.self?.name ?? 'This device',
                        address: connected ? (data.self?.virtualIp ?? '') : '',
                        busy: _busy || phase == DeckPhase.linking,
                        onToggle: () {
                          if (connected || phase == DeckPhase.dropped) {
                            _disconnect();
                          } else if (data.target != null) {
                            _connect(data, data.target!);
                          }
                        },
                      ),
                      if (vpn.lastError != null && vpn.state == AppVpnState.error)
                        _Banner(message: humanError(vpn.lastError!)),
                      Expanded(
                        child: RefreshIndicator(
                          onRefresh: _refresh,
                          color: Deck.live,
                          backgroundColor: Deck.deck800,
                          child: NetworkTree(
                            networks: data.networks,
                            devices: connected ? data.devices : const [],
                            activeNetworkId: connected ? (data.target?.id ?? '') : '',
                            selfPublicKey: data.publicKey,
                            busy: _busy,
                            connected: connected,
                            onConnect: (n) => _connect(data, n),
                            onDisconnect: _disconnect,
                            onShare: (n) => context.push('/networks/${n.id}'),
                            onAction: (d) => showDeviceSheet(
                              context,
                              device: d,
                              networkName: data.target?.name ?? '',
                              isSelf: d.publicKey == data.publicKey,
                            ),
                          ),
                        ),
                      ),
                    ],
                  );
                },
              );
            },
          ),
        ),
      ),
    );
  }

  void _showMenu(BuildContext context, _DeckData data) {
    showModalBottomSheet<void>(
      context: context,
      builder: (sheet) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _MenuItem(
              icon: Icons.hub_outlined,
              label: 'Networks',
              onTap: () {
                Navigator.pop(sheet);
                context.push('/networks').then((_) => _refresh());
              },
            ),
            _MenuItem(
              icon: Icons.devices_other_outlined,
              label: 'Manage devices',
              enabled: data.target != null,
              onTap: () {
                Navigator.pop(sheet);
                context.push('/networks/${data.target!.id}/devices');
              },
            ),
            _MenuItem(
              icon: Icons.tune,
              label: 'Settings',
              onTap: () {
                Navigator.pop(sheet);
                context.push('/settings');
              },
            ),
          ],
        ),
      ),
    );
  }

  static DeckPhase _phaseOf(AppVpnState state, bool busy) {
    if (busy) return DeckPhase.linking;
    return switch (state) {
      AppVpnState.connected => DeckPhase.active,
      AppVpnState.connecting => DeckPhase.linking,
      AppVpnState.reconnecting => DeckPhase.dropped,
      AppVpnState.error => DeckPhase.dropped,
      AppVpnState.disconnected => DeckPhase.idle,
    };
  }

  static SnackBar _snack(String message) => SnackBar(
        content: Text(message),
        backgroundColor: Deck.deck700,
        behavior: SnackBarBehavior.floating,
      );
}

/* ------------------------------------------------------------------ */

class _TopBar extends StatelessWidget {
  const _TopBar({required this.phase, required this.onMenu});

  final DeckPhase phase;
  final VoidCallback onMenu;

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 40,
      decoration: const BoxDecoration(
        color: Deck.deck800,
        border: Border(bottom: BorderSide(color: Deck.line)),
      ),
      padding: const EdgeInsets.only(left: 12, right: 4),
      child: Row(
        children: [
          Text.rich(
            TextSpan(
              text: 'NEXUS',
              children: [TextSpan(text: 'VPN', style: TextStyle(color: Deck.live))],
            ),
            style: Deck.mono(size: 11, weight: FontWeight.w600, spacing: 2.2),
          ),
          const Spacer(),
          _StatePip(phase: phase),
          IconButton(
            icon: const Icon(Icons.more_vert, size: 19),
            color: Deck.inkDim,
            onPressed: onMenu,
            tooltip: 'Menu',
          ),
        ],
      ),
    );
  }
}

/// The state word with a pip that breathes while live and flashes when lost.
class _StatePip extends StatefulWidget {
  const _StatePip({required this.phase});

  final DeckPhase phase;

  @override
  State<_StatePip> createState() => _StatePipState();
}

class _StatePipState extends State<_StatePip> with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1300),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final animated =
        widget.phase == DeckPhase.active || widget.phase == DeckPhase.dropped;
    _c.duration = Duration(milliseconds: widget.phase == DeckPhase.dropped ? 700 : 2600);

    return Row(
      children: [
        FadeTransition(
          opacity: animated
              ? Tween<double>(begin: 1, end: 0.3).animate(_c)
              : const AlwaysStoppedAnimation(1),
          child: Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(color: widget.phase.color, shape: BoxShape.circle),
          ),
        ),
        const SizedBox(width: 7),
        Text(
          widget.phase.word.toUpperCase(),
          style: Deck.mono(size: 10, color: widget.phase.color, spacing: 1.5),
        ),
      ],
    );
  }
}

class _IdentityStrip extends StatelessWidget {
  const _IdentityStrip({
    required this.phase,
    required this.name,
    required this.address,
    required this.busy,
    required this.onToggle,
  });

  final DeckPhase phase;
  final String name;
  final String address;
  final bool busy;
  final VoidCallback onToggle;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: Deck.deck800.withValues(alpha: 0.6),
        border: const Border(bottom: BorderSide(color: Deck.line)),
      ),
      padding: const EdgeInsets.fromLTRB(16, 18, 16, 20),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ReactorCore(
            phase: phase,
            label: switch (phase) {
              DeckPhase.idle => 'off',
              DeckPhase.linking => '···',
              DeckPhase.active => 'on',
              DeckPhase.dropped => 'retry',
            },
            onPressed: busy ? null : onToggle,
          ),
          const SizedBox(height: 16),
          Text(
            name,
            style: const TextStyle(fontSize: 15, color: Deck.ink),
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: 2),
          // The address takes the state's colour too: left cyan while the
          // tunnel is lost it would read as "all fine" next to a crimson
          // core, which is the one moment the strip must not disagree with
          // itself.
          Scrambler(
            value: address,
            active: phase == DeckPhase.active,
            style: Deck.mono(size: 24, color: phase.color, spacing: 1),
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
    return Container(
      width: double.infinity,
      color: Deck.fail.withValues(alpha: 0.12),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Icon(Icons.warning_amber_rounded, size: 16, color: Deck.fail),
          const SizedBox(width: 10),
          Expanded(
            child: Text(message, style: const TextStyle(fontSize: 12.5, color: Deck.ink)),
          ),
        ],
      ),
    );
  }
}

class _ErrorPane extends StatelessWidget {
  const _ErrorPane({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.warning_amber_rounded, size: 22, color: Deck.fail),
            const SizedBox(height: 14),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 13, height: 1.5, color: Deck.inkDim),
            ),
            const SizedBox(height: 18),
            OutlinedButton(onPressed: onRetry, child: const Text('Try again')),
          ],
        ),
      ),
    );
  }
}

class _MenuItem extends StatelessWidget {
  const _MenuItem({
    required this.icon,
    required this.label,
    required this.onTap,
    this.enabled = true,
  });

  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, size: 18, color: enabled ? Deck.inkDim : Deck.inkFaint),
      title: Text(
        label,
        style: TextStyle(fontSize: 14, color: enabled ? Deck.ink : Deck.inkFaint),
      ),
      onTap: enabled ? onTap : null,
    );
  }
}

/* ------------------------------------------------------------------ */

class _DeckData {
  const _DeckData({
    required this.networks,
    required this.target,
    required this.self,
    required this.devices,
    this.publicKey = '',
  });

  final List<Network> networks;
  final Network? target;
  final Device? self;
  final List<Device> devices;
  final String publicKey;
}

/// Turns whatever went wrong into something a person can act on.
String humanError(Object? error) {
  if (error is ApiException) {
    if (error.isNetworkFailure) {
      return 'Cannot reach your server. Check that you are online and that the '
          'server address in Settings is right.';
    }
    if (error.isUnauthorized) {
      return 'Your session has expired. Sign in again to continue.';
    }
    return error.message;
  }
  final raw = error?.toString() ?? 'Something went wrong.';
  if (raw.toLowerCase().contains('permission')) {
    return 'NexusVPN needs your permission to create a VPN connection. '
        'Allow it when your phone asks, then try again.';
  }
  return raw;
}
