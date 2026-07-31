import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_exception.dart';
import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/models/models.dart';
import '../core/vpn_controller.dart';
import '../widgets/device_tile.dart';
import '../widgets/error_view.dart';
import '../widgets/loading_indicator.dart';
import '../widgets/primary_button.dart';

/// Focused device-management view for a single network: register this
/// device, inspect every device on the network with its live status and
/// traffic counters, and remove devices you administer.
class DevicesScreen extends StatefulWidget {
  const DevicesScreen({super.key, required this.networkId});

  final String networkId;

  @override
  State<DevicesScreen> createState() => _DevicesScreenState();
}

class _DevicesScreenState extends State<DevicesScreen> {
  late Future<_DevicesData> _future;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<_DevicesData> _load() async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final network = await api.getNetwork(widget.networkId);
    final devices = await api.listNetworkDevices(widget.networkId);
    final keys = await vpn.ensureKeypair();
    return _DevicesData(network: network, devices: devices, myPublicKey: keys.publicKey);
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

  Future<void> _registerDevice() async {
    final nameController =
        TextEditingController(text: await AppSettings.getDeviceName());
    final formKey = GlobalKey<FormState>();
    bool submitting = false;
    String? error;

    if (!mounted) return;
    final registered = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('Register this device'),
          content: Form(
            key: formKey,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (error != null) ...[
                  Text(error!, style: const TextStyle(color: Colors.red)),
                  const SizedBox(height: 8),
                ],
                TextFormField(
                  controller: nameController,
                  decoration: const InputDecoration(labelText: 'Device name'),
                  validator: (v) =>
                      (v == null || v.trim().isEmpty) ? 'Name is required' : null,
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: submitting
                  ? null
                  : () => Navigator.of(dialogContext).pop(false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: submitting
                  ? null
                  : () async {
                      if (!formKey.currentState!.validate()) return;
                      setDialogState(() {
                        submitting = true;
                        error = null;
                      });
                      try {
                        final api = context.read<AuthProvider>().api;
                        final vpn = context.read<VpnController>();
                        final keys = await vpn.ensureKeypair();
                        await api.registerDevice(
                          widget.networkId,
                          name: nameController.text.trim(),
                          os: _platformOs,
                          publicKey: keys.publicKey,
                        );
                        await AppSettings.setDeviceName(nameController.text.trim());
                        if (dialogContext.mounted) {
                          Navigator.of(dialogContext).pop(true);
                        }
                      } on ApiException catch (e) {
                        setDialogState(() {
                          submitting = false;
                          error = e.message;
                        });
                      }
                    },
              child: submitting
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Register'),
            ),
          ],
        ),
      ),
    );

    if (registered == true) await _refresh();
  }

  Future<void> _confirmDelete(Device device) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Remove device?'),
        content: Text('"${device.name}" will be disconnected and removed '
            'from this network.'),
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
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await context.read<AuthProvider>().api.deleteDevice(device.id);
      await _refresh();
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
      appBar: AppBar(title: const Text('Devices')),
      body: FutureBuilder<_DevicesData>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const LoadingIndicator();
          }
          if (snapshot.hasError) {
            final err = snapshot.error;
            return ErrorView(
              message:
                  err is ApiException ? err.message : 'Failed to load devices.',
              onRetry: _refresh,
            );
          }
          final data = snapshot.data!;
          final hasSelf =
              data.devices.any((d) => d.publicKey == data.myPublicKey);
          return RefreshIndicator(
            onRefresh: _refresh,
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                if (!hasSelf) ...[
                  Card(
                    child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'This device is not on the network yet',
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                          const SizedBox(height: 8),
                          PrimaryButton(
                            label: 'Register this device',
                            icon: Icons.add_link,
                            onPressed: _registerDevice,
                          ),
                        ],
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                ],
                if (data.devices.isEmpty)
                  const Padding(
                    padding: EdgeInsets.only(top: 64),
                    child: EmptyView(
                      icon: Icons.devices_other_outlined,
                      message: 'No devices on this network yet.',
                    ),
                  )
                else
                  Card(
                    child: Column(
                      children: data.devices.map((d) {
                        final isSelf = d.publicKey == data.myPublicKey;
                        return DeviceTile(
                          device: d,
                          subtitleOverride: [
                            if (d.virtualIp != null) d.virtualIp!,
                            if (d.latencyMs != null) '${d.latencyMs}ms',
                            if (isSelf) 'this device',
                          ].join(' · '),
                          trailing: IconButton(
                            icon: const Icon(Icons.delete_outline),
                            tooltip: 'Remove device',
                            onPressed: () => _confirmDelete(d),
                          ),
                        );
                      }).toList(),
                    ),
                  ),
              ],
            ),
          );
        },
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _registerDevice,
        icon: const Icon(Icons.add),
        label: const Text('Register device'),
      ),
    );
  }
}

class _DevicesData {
  _DevicesData({
    required this.network,
    required this.devices,
    required this.myPublicKey,
  });

  final Network network;
  final List<Device> devices;
  final String myPublicKey;
}
