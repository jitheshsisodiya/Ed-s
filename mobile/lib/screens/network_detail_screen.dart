import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:provider/provider.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core/app_exception.dart';
import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/models/models.dart';
import '../core/vpn_controller.dart';
import '../widgets/device_tile.dart';
import '../widgets/error_view.dart';
import '../widgets/loading_indicator.dart';
import '../widgets/primary_button.dart';
import '../widgets/status_badge.dart';

class NetworkDetailScreen extends StatefulWidget {
  const NetworkDetailScreen({super.key, required this.networkId});

  final String networkId;

  @override
  State<NetworkDetailScreen> createState() => _NetworkDetailScreenState();
}

class _NetworkDetailScreenState extends State<NetworkDetailScreen> {
  late Future<_NetworkDetailData> _future;
  Device? _selfDevice;
  bool _registering = false;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<_NetworkDetailData> _load() async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final results = await Future.wait([
      api.getNetwork(widget.networkId),
      api.listMembers(widget.networkId),
      api.listNetworkDevices(widget.networkId),
    ]);
    final network = results[0] as Network;
    final members = results[1] as List<Member>;
    final devices = results[2] as List<Device>;

    final keys = await vpn.ensureKeypair();
    Device? selfDevice;
    for (final d in devices) {
      if (d.publicKey == keys.publicKey) {
        selfDevice = d;
        break;
      }
    }
    _selfDevice = selfDevice;

    return _NetworkDetailData(network: network, members: members, devices: devices);
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

  Future<void> _registerThisDevice() async {
    setState(() => _registering = true);
    try {
      final api = context.read<AuthProvider>().api;
      final vpn = context.read<VpnController>();
      final keys = await vpn.ensureKeypair();
      String deviceName = await AppSettings.getDeviceName(fallback: '');
      String? osVersion;
      if (deviceName.isEmpty) {
        try {
          final info = await PackageInfo.fromPlatform();
          deviceName = '${info.appName} on $_platformOs';
        } catch (_) {
          deviceName = 'My Device';
        }
      }
      await api.registerDevice(
        widget.networkId,
        name: deviceName,
        os: _platformOs,
        osVersion: osVersion,
        publicKey: keys.publicKey,
      );
      await _refresh();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(e.message)));
      }
    } finally {
      if (mounted) setState(() => _registering = false);
    }
  }

  Future<void> _rotateInvite(Network network) async {
    try {
      final api = context.read<AuthProvider>().api;
      await api.rotateInviteCode(network.id);
      await _refresh();
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('Invite code rotated.')));
      }
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(e.message)));
      }
    }
  }

  Future<void> _confirmDeleteNetwork(Network network) async {
    // Captured before the dialog: reading an inherited widget after an
    // await is unsafe, since this State may have been disposed by then.
    final api = context.read<AuthProvider>().api;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete network?'),
        content: Text(
          'This permanently deletes "${network.name}" and removes all '
          'devices from it. This cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await api.deleteNetwork(network.id);
      if (mounted) context.pop();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(e.message)));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Network'),
        actions: [
          IconButton(
            icon: const Icon(Icons.devices_outlined),
            tooltip: 'Manage devices',
            onPressed: () => context.push('/networks/${widget.networkId}/devices'),
          ),
        ],
      ),
      body: FutureBuilder<_NetworkDetailData>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const LoadingIndicator();
          }
          if (snapshot.hasError) {
            final err = snapshot.error;
            return ErrorView(
              message:
                  err is ApiException ? err.message : 'Failed to load network.',
              onRetry: _refresh,
            );
          }
          final data = snapshot.data!;
          return RefreshIndicator(
            onRefresh: _refresh,
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                _HeaderCard(network: data.network),
                const SizedBox(height: 16),
                _ConnectCard(
                  network: data.network,
                  selfDevice: _selfDevice,
                  registering: _registering,
                  onRegister: _registerThisDevice,
                ),
                const SizedBox(height: 16),
                _InviteCard(
                  network: data.network,
                  onRotate: data.network.canManage
                      ? () => _rotateInvite(data.network)
                      : null,
                ),
                const SizedBox(height: 16),
                _MembersCard(networkId: widget.networkId, members: data.members),
                const SizedBox(height: 16),
                _DevicesCard(
                  devices: data.devices,
                  onManage: () =>
                      context.push('/networks/${widget.networkId}/devices'),
                ),
                if (data.network.canManage) ...[
                  const SizedBox(height: 16),
                  Card(
                    color: Theme.of(context).colorScheme.errorContainer,
                    child: ListTile(
                      leading: Icon(
                        Icons.delete_outline,
                        color: Theme.of(context).colorScheme.onErrorContainer,
                      ),
                      title: Text(
                        'Delete network',
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.onErrorContainer,
                        ),
                      ),
                      onTap: () => _confirmDeleteNetwork(data.network),
                    ),
                  ),
                ],
                const SizedBox(height: 32),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _NetworkDetailData {
  _NetworkDetailData({
    required this.network,
    required this.members,
    required this.devices,
  });

  final Network network;
  final List<Member> members;
  final List<Device> devices;
}

class _HeaderCard extends StatelessWidget {
  const _HeaderCard({required this.network});

  final Network network;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(network.name, style: theme.textTheme.headlineSmall),
                ),
                Chip(label: Text(network.role)),
              ],
            ),
            if (network.description != null && network.description!.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(network.description!, style: theme.textTheme.bodyMedium),
            ],
            const SizedBox(height: 10),
            Wrap(
              spacing: 16,
              runSpacing: 4,
              children: [
                _MetaChip(icon: Icons.route_outlined, label: network.cidr),
                _MetaChip(
                  icon: Icons.people_outline,
                  label: '${network.memberCount} members',
                ),
                _MetaChip(
                  icon: Icons.devices_outlined,
                  label: '${network.deviceCount} devices',
                ),
                if (network.dnsServers.isNotEmpty)
                  _MetaChip(
                    icon: Icons.dns_outlined,
                    label: network.dnsServers.join(', '),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _MetaChip extends StatelessWidget {
  const _MetaChip({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: theme.colorScheme.outline),
        const SizedBox(width: 4),
        Text(label, style: theme.textTheme.bodySmall),
      ],
    );
  }
}

class _ConnectCard extends StatelessWidget {
  const _ConnectCard({
    required this.network,
    required this.selfDevice,
    required this.registering,
    required this.onRegister,
  });

  final Network network;
  final Device? selfDevice;
  final bool registering;
  final VoidCallback onRegister;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (selfDevice == null) {
      return Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Connect this device', style: theme.textTheme.titleMedium),
              const SizedBox(height: 8),
              Text(
                'This device is not registered on this network yet. '
                'Register it to get a virtual IP and start the WireGuard '
                'tunnel.',
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 12),
              PrimaryButton(
                label: 'Register this device',
                icon: Icons.add_link,
                loading: registering,
                onPressed: onRegister,
              ),
            ],
          ),
        ),
      );
    }

    return Consumer<VpnController>(
      builder: (context, vpn, _) {
        final isThisNetworkActive =
            vpn.activeNetwork?.id == network.id && vpn.activeDevice?.id == selfDevice!.id;
        final displayState = isThisNetworkActive ? vpn.state : AppVpnState.disconnected;

        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text('VPN tunnel', style: theme.textTheme.titleMedium),
                    const Spacer(),
                    _VpnStateBadge(state: displayState),
                  ],
                ),
                const SizedBox(height: 8),
                Row(
                  children: [
                    Icon(Icons.lan_outlined, size: 16, color: theme.colorScheme.outline),
                    const SizedBox(width: 4),
                    Text(
                      selfDevice!.virtualIp ?? 'No IP assigned yet',
                      style: theme.textTheme.bodySmall,
                    ),
                    if (selfDevice!.latencyMs != null) ...[
                      const SizedBox(width: 12),
                      Icon(Icons.speed_outlined, size: 16, color: theme.colorScheme.outline),
                      const SizedBox(width: 4),
                      Text('${selfDevice!.latencyMs} ms', style: theme.textTheme.bodySmall),
                    ],
                  ],
                ),
                if (vpn.lastError != null && displayState == AppVpnState.error) ...[
                  const SizedBox(height: 8),
                  Text(
                    vpn.lastError!,
                    style: TextStyle(color: theme.colorScheme.error, fontSize: 12),
                  ),
                ],
                const SizedBox(height: 12),
                PrimaryButton(
                  label: displayState == AppVpnState.connected ||
                          displayState == AppVpnState.reconnecting
                      ? 'Disconnect'
                      : 'Connect',
                  icon: displayState == AppVpnState.connected
                      ? Icons.link_off
                      : Icons.power_settings_new,
                  destructive: displayState == AppVpnState.connected,
                  loading: displayState == AppVpnState.connecting,
                  onPressed: () async {
                    if (displayState == AppVpnState.connected ||
                        displayState == AppVpnState.reconnecting) {
                      await vpn.disconnect();
                    } else {
                      try {
                        await vpn.connect(network: network, selfDevice: selfDevice!);
                      } catch (_) {
                        // vpn.lastError already carries the message; the
                        // badge + card below will show it.
                      }
                    }
                  },
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _VpnStateBadge extends StatelessWidget {
  const _VpnStateBadge({required this.state});

  final AppVpnState state;

  @override
  Widget build(BuildContext context) {
    switch (state) {
      case AppVpnState.connected:
        return StatusBadge(label: 'Connected', color: Colors.green.shade600);
      case AppVpnState.connecting:
        return StatusBadge(label: 'Connecting…', color: Colors.orange.shade700);
      case AppVpnState.reconnecting:
        return StatusBadge(label: 'Reconnecting…', color: Colors.orange.shade700);
      case AppVpnState.error:
        return StatusBadge(label: 'Error', color: Colors.red.shade600);
      case AppVpnState.disconnected:
        return StatusBadge(label: 'Disconnected', color: Colors.grey.shade600);
    }
  }
}

class _InviteCard extends StatelessWidget {
  const _InviteCard({required this.network, required this.onRotate});

  final Network network;
  final VoidCallback? onRotate;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final code = network.inviteCode;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text('Invite', style: theme.textTheme.titleMedium),
                const Spacer(),
                if (onRotate != null)
                  IconButton(
                    icon: const Icon(Icons.refresh),
                    tooltip: 'Rotate invite code',
                    onPressed: onRotate,
                  ),
              ],
            ),
            if (code == null || code.isEmpty)
              Text(
                'No invite code available.',
                style: theme.textTheme.bodyMedium,
              )
            else
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: QrImageView(data: code, size: 96),
                  ),
                  const SizedBox(width: 16),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        SelectableText(
                          code,
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontFamily: 'monospace',
                          ),
                        ),
                        const SizedBox(height: 8),
                        OutlinedButton.icon(
                          onPressed: () async {
                            await Clipboard.setData(ClipboardData(text: code));
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(content: Text('Invite code copied')),
                              );
                            }
                          },
                          icon: const Icon(Icons.copy, size: 16),
                          label: const Text('Copy'),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

class _MembersCard extends StatelessWidget {
  const _MembersCard({required this.networkId, required this.members});

  final String networkId;
  final List<Member> members;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final auth = context.watch<AuthProvider>();
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
              child: Text('Members (${members.length})',
                  style: theme.textTheme.titleMedium),
            ),
            if (members.isEmpty)
              const Padding(
                padding: EdgeInsets.all(16),
                child: Text('No members found.'),
              )
            else
              ...members.map(
                (m) => ListTile(
                  dense: true,
                  leading: CircleAvatar(
                    child: Text(
                      m.displayName.isNotEmpty
                          ? m.displayName.substring(0, 1).toUpperCase()
                          : '?',
                    ),
                  ),
                  title: Text(
                    m.displayName.isEmpty ? m.email : m.displayName,
                  ),
                  subtitle: Text(
                    m.email == auth.lastKnownEmail ? '${m.email} (you)' : m.email,
                  ),
                  trailing: Chip(label: Text(m.role)),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _DevicesCard extends StatelessWidget {
  const _DevicesCard({required this.devices, required this.onManage});

  final List<Device> devices;
  final VoidCallback onManage;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final preview = devices.take(4).toList();
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
              child: Row(
                children: [
                  Text('Devices (${devices.length})',
                      style: theme.textTheme.titleMedium),
                  const Spacer(),
                  TextButton(onPressed: onManage, child: const Text('Manage')),
                ],
              ),
            ),
            if (preview.isEmpty)
              const Padding(
                padding: EdgeInsets.all(16),
                child: Text('No devices registered on this network yet.'),
              )
            else
              ...preview.map((d) => DeviceTile(device: d)),
          ],
        ),
      ),
    );
  }
}
