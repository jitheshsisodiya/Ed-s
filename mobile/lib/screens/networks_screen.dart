import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';

import '../core/app_exception.dart';
import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/invite.dart';
import '../core/models/models.dart';
import '../widgets/error_view.dart';
import '../widgets/loading_indicator.dart';
import '../widgets/network_card.dart';

class NetworksScreen extends StatefulWidget {
  const NetworksScreen({super.key});

  @override
  State<NetworksScreen> createState() => _NetworksScreenState();
}

class _NetworksScreenState extends State<NetworksScreen> {
  late Future<List<Network>> _future;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<List<Network>> _load() {
    return context.read<AuthProvider>().api.listNetworks();
  }

  Future<void> _refresh() async {
    final future = _load();
    setState(() => _future = future);
    await future;
  }

  Future<void> _showCreateDialog() async {
    final nameController = TextEditingController();
    final descController = TextEditingController();
    final cidrController = TextEditingController(text: '10.77.0.0/24');
    final dnsController = TextEditingController();
    final formKey = GlobalKey<FormState>();
    bool submitting = false;
    String? error;

    final created = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('Create network'),
          content: Form(
            key: formKey,
            child: SingleChildScrollView(
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
                    decoration: const InputDecoration(labelText: 'Name'),
                    validator: (v) => (v == null || v.trim().isEmpty)
                        ? 'Name is required'
                        : null,
                  ),
                  TextFormField(
                    controller: descController,
                    decoration:
                        const InputDecoration(labelText: 'Description (optional)'),
                  ),
                  TextFormField(
                    controller: cidrController,
                    decoration: const InputDecoration(
                      labelText: 'CIDR',
                      helperText: 'e.g. 10.77.0.0/24',
                    ),
                    validator: (v) => (v == null || v.trim().isEmpty)
                        ? 'CIDR is required'
                        : null,
                  ),
                  TextFormField(
                    controller: dnsController,
                    decoration: const InputDecoration(
                      labelText: 'DNS servers (optional, comma separated)',
                    ),
                  ),
                ],
              ),
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
                        final dns = dnsController.text
                            .split(',')
                            .map((s) => s.trim())
                            .where((s) => s.isNotEmpty)
                            .toList();
                        await context.read<AuthProvider>().api.createNetwork(
                              name: nameController.text.trim(),
                              description: descController.text.trim().isEmpty
                                  ? null
                                  : descController.text.trim(),
                              cidr: cidrController.text.trim(),
                              dnsServers: dns.isEmpty ? null : dns,
                            );
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
                  : const Text('Create'),
            ),
          ],
        ),
      ),
    );

    if (created == true) {
      await _refresh();
    }
  }

  Future<void> _showJoinDialog() async {
    final codeController = TextEditingController();
    final formKey = GlobalKey<FormState>();
    bool submitting = false;
    String? error;

    final joined = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('Join network'),
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
                  controller: codeController,
                  decoration: InputDecoration(
                    labelText: 'Invite code',
                    suffixIcon: IconButton(
                      icon: const Icon(Icons.qr_code_scanner),
                      tooltip: 'Scan QR code',
                      onPressed: () async {
                        final scanned = await Navigator.of(context).push<String>(
                          MaterialPageRoute(
                            builder: (_) => const _QrScanScreen(),
                          ),
                        );
                        if (scanned != null) {
                          // A scan yields whatever was encoded — a bare
                          // code from an older build, or a link from a
                          // current one. Both mean the same thing.
                          codeController.text = parseInviteCode(scanned);
                        }
                      },
                    ),
                  ),
                  validator: (v) => parseInviteCode(v ?? '').isEmpty
                      ? 'Paste the invite code or link you were sent'
                      : null,
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
                        await context
                            .read<AuthProvider>()
                            .api
                            .joinNetwork(
                              parseInviteCode(codeController.text),
                            );
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
                  : const Text('Join'),
            ),
          ],
        ),
      ),
    );

    if (joined == true) {
      await _refresh();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Networks'),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings_outlined),
            tooltip: 'Settings',
            onPressed: () => context.push('/settings'),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: FutureBuilder<List<Network>>(
          future: _future,
          builder: (context, snapshot) {
            if (snapshot.connectionState == ConnectionState.waiting) {
              return const LoadingIndicator();
            }
            if (snapshot.hasError) {
              final err = snapshot.error;
              return ListView(
                children: [
                  const SizedBox(height: 80),
                  ErrorView(
                    message: err is ApiException
                        ? err.message
                        : 'Failed to load networks.',
                    onRetry: _refresh,
                  ),
                ],
              );
            }
            final networks = snapshot.data ?? const [];
            if (networks.isEmpty) {
              return ListView(
                children: [
                  const SizedBox(height: 80),
                  EmptyView(
                    icon: Icons.hub_outlined,
                    message:
                        'You are not part of any networks yet.\nCreate one or join with an invite code.',
                    action: Wrap(
                      spacing: 12,
                      children: [
                        OutlinedButton.icon(
                          onPressed: _showJoinDialog,
                          icon: const Icon(Icons.login),
                          label: const Text('Join'),
                        ),
                        FilledButton.icon(
                          onPressed: _showCreateDialog,
                          icon: const Icon(Icons.add),
                          label: const Text('Create'),
                        ),
                      ],
                    ),
                  ),
                ],
              );
            }
            return ListView.builder(
              padding: const EdgeInsets.symmetric(vertical: 8),
              itemCount: networks.length,
              itemBuilder: (context, index) {
                final n = networks[index];
                return NetworkCard(
                  network: n,
                  advanced: context.watch<Preferences>().advanced,
                  onTap: () => context.push('/networks/${n.id}'),
                );
              },
            );
          },
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () async {
          final choice = await showModalBottomSheet<String>(
            context: context,
            builder: (context) => SafeArea(
              child: Wrap(
                children: [
                  ListTile(
                    leading: const Icon(Icons.add),
                    title: const Text('Create a network'),
                    onTap: () => Navigator.of(context).pop('create'),
                  ),
                  ListTile(
                    leading: const Icon(Icons.login),
                    title: const Text('Join with invite code'),
                    onTap: () => Navigator.of(context).pop('join'),
                  ),
                ],
              ),
            ),
          );
          if (choice == 'create') await _showCreateDialog();
          if (choice == 'join') await _showJoinDialog();
        },
        icon: const Icon(Icons.add),
        label: const Text('Network'),
      ),
    );
  }
}

/// Full-screen QR scanner used to join a network by scanning an invite QR
/// code shared from another device.
class _QrScanScreen extends StatelessWidget {
  const _QrScanScreen();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Scan invite QR code')),
      body: MobileScanner(
        onDetect: (capture) {
          final barcodes = capture.barcodes;
          if (barcodes.isEmpty) return;
          final value = barcodes.first.rawValue;
          if (value != null && value.isNotEmpty) {
            Navigator.of(context).pop(value);
          }
        },
      ),
    );
  }
}
