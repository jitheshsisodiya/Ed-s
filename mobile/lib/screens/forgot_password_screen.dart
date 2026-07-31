import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

import '../core/auth_provider.dart';
import '../widgets/primary_button.dart';

/// Two-stage flow: request a reset email (`/auth/password/forgot`), then -
/// once the user has the token from that email - set a new password
/// (`/auth/password/reset`).
class ForgotPasswordScreen extends StatefulWidget {
  const ForgotPasswordScreen({super.key});

  @override
  State<ForgotPasswordScreen> createState() => _ForgotPasswordScreenState();
}

class _ForgotPasswordScreenState extends State<ForgotPasswordScreen> {
  final _requestFormKey = GlobalKey<FormState>();
  final _resetFormKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _tokenController = TextEditingController();
  final _newPasswordController = TextEditingController();

  bool _submitting = false;
  bool _requestSent = false;
  String? _error;
  String? _info;

  @override
  void dispose() {
    _emailController.dispose();
    _tokenController.dispose();
    _newPasswordController.dispose();
    super.dispose();
  }

  Future<void> _requestReset() async {
    if (!_requestFormKey.currentState!.validate()) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    final auth = context.read<AuthProvider>();
    final ok = await auth.forgotPassword(_emailController.text.trim());
    if (!mounted) return;
    setState(() {
      _submitting = false;
      if (ok) {
        _requestSent = true;
        _info = 'If that account exists, a reset email was sent. '
            'Enter the token from that email below along with a new '
            'password.';
      } else {
        _error = auth.errorMessage ?? 'Could not send reset email.';
      }
    });
  }

  Future<void> _resetPassword() async {
    if (!_resetFormKey.currentState!.validate()) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    final auth = context.read<AuthProvider>();
    final ok = await auth.resetPassword(
      _tokenController.text.trim(),
      _newPasswordController.text,
    );
    if (!mounted) return;
    setState(() => _submitting = false);
    if (ok) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Password updated. Please sign in.')),
      );
      context.go('/login');
    } else {
      setState(() => _error = auth.errorMessage ?? 'Could not reset password.');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Reset password')),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  if (_error != null) ...[
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.errorContainer,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(
                        _error!,
                        style: TextStyle(
                          color:
                              Theme.of(context).colorScheme.onErrorContainer,
                        ),
                      ),
                    ),
                    const SizedBox(height: 16),
                  ],
                  if (_info != null) ...[
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: Theme.of(context)
                            .colorScheme
                            .secondaryContainer,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(
                        _info!,
                        style: TextStyle(
                          color: Theme.of(context)
                              .colorScheme
                              .onSecondaryContainer,
                        ),
                      ),
                    ),
                    const SizedBox(height: 16),
                  ],
                  Form(
                    key: _requestFormKey,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          '1. Request a reset email',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: 8),
                        TextFormField(
                          controller: _emailController,
                          keyboardType: TextInputType.emailAddress,
                          decoration: const InputDecoration(
                            labelText: 'Email',
                            prefixIcon: Icon(Icons.email_outlined),
                            border: OutlineInputBorder(),
                          ),
                          validator: (v) {
                            if (v == null || v.trim().isEmpty) {
                              return 'Email is required';
                            }
                            if (!v.contains('@')) return 'Enter a valid email';
                            return null;
                          },
                        ),
                        const SizedBox(height: 12),
                        PrimaryButton(
                          label: 'Send reset email',
                          loading: _submitting && !_requestSent,
                          onPressed: _requestReset,
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 28),
                  const Divider(),
                  const SizedBox(height: 12),
                  Form(
                    key: _resetFormKey,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          '2. Set a new password',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: 8),
                        TextFormField(
                          controller: _tokenController,
                          decoration: const InputDecoration(
                            labelText: 'Reset token (from email)',
                            prefixIcon: Icon(Icons.vpn_key_outlined),
                            border: OutlineInputBorder(),
                          ),
                          validator: (v) => (v == null || v.trim().isEmpty)
                              ? 'Reset token is required'
                              : null,
                        ),
                        const SizedBox(height: 12),
                        TextFormField(
                          controller: _newPasswordController,
                          obscureText: true,
                          decoration: const InputDecoration(
                            labelText: 'New password',
                            prefixIcon: Icon(Icons.lock_outline),
                            border: OutlineInputBorder(),
                            helperText: 'At least 12 characters',
                          ),
                          validator: (v) {
                            if (v == null || v.length < 12) {
                              return 'Password must be at least 12 characters';
                            }
                            return null;
                          },
                        ),
                        const SizedBox(height: 12),
                        PrimaryButton(
                          label: 'Update password',
                          loading: _submitting && _requestSent,
                          onPressed: _resetPassword,
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
