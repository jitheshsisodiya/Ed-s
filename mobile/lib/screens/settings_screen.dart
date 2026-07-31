import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:provider/provider.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core/app_settings.dart';
import '../core/auth_provider.dart';
import '../core/vpn_controller.dart';
import '../widgets/primary_button.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _serverUrlController = TextEditingController();
  final _deviceNameController = TextEditingController();
  String? _publicKey;
  String _appVersion = '';
  bool _loading = true;
  bool _savingServerUrl = false;
  bool _savingDeviceName = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _serverUrlController.dispose();
    _deviceNameController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final api = context.read<AuthProvider>().api;
    final vpn = context.read<VpnController>();
    final url = await api.baseUrl;
    final name = await AppSettings.getDeviceName();
    final keys = await vpn.ensureKeypair();
    PackageInfo? info;
    try {
      info = await PackageInfo.fromPlatform();
    } catch (_) {
      info = null;
    }
    if (!mounted) return;
    setState(() {
      _serverUrlController.text = url;
      _deviceNameController.text = name;
      _publicKey = keys.publicKey;
      _appVersion = info != null ? '${info.version} (${info.buildNumber})' : '';
      _loading = false;
    });
  }

  Future<void> _saveServerUrl() async {
    final url = _serverUrlController.text.trim();
    if (url.isEmpty) return;
    setState(() => _savingServerUrl = true);
    try {
      await context.read<AuthProvider>().api.setBaseUrl(url);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Server URL saved.')),
        );
      }
    } finally {
      if (mounted) setState(() => _savingServerUrl = false);
    }
  }

  Future<void> _saveDeviceName() async {
    final name = _deviceNameController.text.trim();
    if (name.isEmpty) return;
    setState(() => _savingDeviceName = true);
    await AppSettings.setDeviceName(name);
    if (mounted) {
      setState(() => _savingDeviceName = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text(
            'Default device name saved. It applies the next time you '
            'register a device on a network.',
          ),
        ),
      );
    }
  }

  Future<void> _rotateKey() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Rotate device key?'),
        content: const Text(
          'This generates a brand new WireGuard keypair for this device. '
          'Any network this device is already registered on will keep '
          'pointing at the OLD public key until you remove and re-register '
          'this device there - the API does not currently support updating '
          'a device\'s key in place.',
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
            child: const Text('Rotate key'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    final vpn = context.read<VpnController>();
    if (vpn.state == AppVpnState.connected) {
      await vpn.disconnect();
    }
    final keys = await vpn.ensureKeypair(rotate: true);
    if (!mounted) return;
    setState(() => _publicKey = keys.publicKey);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('New device key generated.')),
    );
  }

  Future<void> _showEnableMfa() async {
    final auth = context.read<AuthProvider>();
    final result = await auth.enableMfa();
    if (result == null) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(auth.errorMessage ?? 'Could not start MFA setup.')),
        );
      }
      return;
    }
    if (!mounted) return;
    final codeController = TextEditingController();
    final formKey = GlobalKey<FormState>();
    bool submitting = false;
    String? error;

    await showDialog<void>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('Enable two-factor authentication'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text('Scan this QR code with your authenticator app:'),
                const SizedBox(height: 12),
                if (result['otpauthUrl'] != null && result['otpauthUrl']!.isNotEmpty)
                  Center(
                    child: Container(
                      padding: const EdgeInsets.all(8),
                      color: Colors.white,
                      child: QrImageView(data: result['otpauthUrl']!, size: 180),
                    ),
                  ),
                const SizedBox(height: 8),
                SelectableText('Secret: ${result['secret']}'),
                const SizedBox(height: 16),
                if (error != null) ...[
                  Text(error!, style: const TextStyle(color: Colors.red)),
                  const SizedBox(height: 8),
                ],
                Form(
                  key: formKey,
                  child: TextFormField(
                    controller: codeController,
                    keyboardType: TextInputType.number,
                    decoration: const InputDecoration(
                      labelText: 'Enter 6-digit code to confirm',
                    ),
                    validator: (v) =>
                        (v == null || v.trim().length < 6) ? 'Enter 6 digits' : null,
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: submitting ? null : () => Navigator.of(dialogContext).pop(),
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
                      final ok = await auth.verifyMfaSetup(codeController.text.trim());
                      if (ok) {
                        if (dialogContext.mounted) Navigator.of(dialogContext).pop();
                        if (mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(content: Text('Two-factor authentication enabled.')),
                          );
                        }
                      } else {
                        setDialogState(() {
                          submitting = false;
                          error = auth.errorMessage ?? 'Invalid code.';
                        });
                      }
                    },
              child: submitting
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Confirm'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _logout() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Log out?'),
        content: const Text(
          'You will need to sign in again to manage your networks. Any '
          'active VPN tunnel will be disconnected.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Log out'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    final vpn = context.read<VpnController>();
    if (vpn.state == AppVpnState.connected) {
      await vpn.disconnect();
    }
    if (!mounted) return;
    await context.read<AuthProvider>().logout();
    if (mounted) context.go('/login');
  }

  @override
  Widget build(BuildContext context) {
    final auth = context.watch<AuthProvider>();

    if (_loading) {
      return Scaffold(
        appBar: AppBar(title: const Text('Settings')),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _SectionCard(
            title: 'Account',
            children: [
              ListTile(
                leading: const Icon(Icons.account_circle_outlined),
                title: Text(auth.lastKnownEmail ?? 'Signed in'),
                subtitle: const Text('Account email'),
              ),
              ListTile(
                leading: const Icon(Icons.shield_outlined),
                title: const Text('Two-factor authentication'),
                subtitle: const Text('Protect your account with an authenticator app'),
                trailing: FilledButton.tonal(
                  onPressed: _showEnableMfa,
                  child: const Text('Enable'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          _SectionCard(
            title: 'Server',
            subtitle: 'Point the app at your own self-hosted NexusVPN backend.',
            children: [
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: TextField(
                  controller: _serverUrlController,
                  decoration: const InputDecoration(
                    labelText: 'API base URL',
                    helperText: 'e.g. https://vpn.example.com/api/v1',
                  ),
                  keyboardType: TextInputType.url,
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Align(
                  alignment: Alignment.centerRight,
                  child: PrimaryButton(
                    label: 'Save server URL',
                    loading: _savingServerUrl,
                    onPressed: _saveServerUrl,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          _SectionCard(
            title: 'Device',
            children: [
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: TextField(
                  controller: _deviceNameController,
                  decoration: const InputDecoration(labelText: 'Default device name'),
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Align(
                  alignment: Alignment.centerRight,
                  child: PrimaryButton(
                    label: 'Save device name',
                    loading: _savingDeviceName,
                    onPressed: _saveDeviceName,
                  ),
                ),
              ),
              const Divider(height: 1),
              ListTile(
                leading: const Icon(Icons.vpn_key_outlined),
                title: const Text('WireGuard public key'),
                subtitle: Text(
                  _publicKey ?? '',
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
                ),
              ),
              ListTile(
                leading: Icon(Icons.autorenew, color: Theme.of(context).colorScheme.error),
                title: const Text('Rotate device key'),
                subtitle: const Text('Generates a new keypair for this device'),
                onTap: _rotateKey,
              ),
            ],
          ),
          const SizedBox(height: 16),
          _SectionCard(
            title: 'About',
            children: [
              ListTile(
                leading: const Icon(Icons.info_outline),
                title: const Text('App version'),
                subtitle: Text(_appVersion.isEmpty ? 'Unknown' : _appVersion),
              ),
              const ListTile(
                leading: Icon(Icons.code_outlined),
                title: Text('NexusVPN'),
                subtitle: Text('Open-source mesh VPN, MIT licensed'),
              ),
            ],
          ),
          const SizedBox(height: 24),
          PrimaryButton(
            label: 'Log out',
            icon: Icons.logout,
            destructive: true,
            loading: auth.busy,
            onPressed: _logout,
          ),
          const SizedBox(height: 24),
        ],
      ),
    );
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.title, this.subtitle, required this.children});

  final String title;
  final String? subtitle;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: theme.textTheme.titleMedium),
                  if (subtitle != null) ...[
                    const SizedBox(height: 2),
                    Text(subtitle!, style: theme.textTheme.bodySmall),
                  ],
                ],
              ),
            ),
            ...children,
          ],
        ),
      ),
    );
  }
}
