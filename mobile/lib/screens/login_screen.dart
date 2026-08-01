import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

import '../core/auth_provider.dart';
import '../core/server_url.dart';
import '../widgets/pair_scanner.dart';
import '../widgets/primary_button.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  final _serverController = TextEditingController();
  bool _obscure = true;

  @override
  void initState() {
    super.initState();
    final auth = context.read<AuthProvider>();
    if (auth.lastKnownEmail != null) {
      _emailController.text = auth.lastKnownEmail!;
    }
    // The address has to be answerable from here. Settings is behind the
    // sign-in this screen is trying to perform, so a wrong address left this
    // screen the only one reachable and nothing on it could fix the problem.
    auth.api.baseUrl.then((url) {
      if (mounted) _serverController.text = url;
    });
  }

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    _serverController.dispose();
    super.dispose();
  }

  /// Opens the camera, and signs in with whatever it reads.
  ///
  /// Nothing is typed on this path — not the address, not the account — which
  /// is why it is offered first. On a computer the code is behind the phone
  /// icon on any network row.
  Future<void> _scanToPair() async {
    final link = await Navigator.of(context).push<String>(
      MaterialPageRoute(builder: (_) => const PairScannerScreen()),
    );
    if (link == null || link.isEmpty || !mounted) return;

    final auth = context.read<AuthProvider>();
    final networkId = await auth.pairWithLink(link);
    if (!mounted) return;
    if (networkId != null) {
      context.go(networkId.isEmpty ? '/networks' : '/networks/$networkId');
    }
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;
    final auth = context.read<AuthProvider>();

    // Saved before the attempt, so signing in and pointing at the right
    // server are one action rather than two.
    await auth.api.setBaseUrl(_serverController.text);

    if (!mounted) return;
    final ok = await auth.login(
      _emailController.text.trim(),
      _passwordController.text,
    );
    if (!mounted) return;
    if (ok) {
      context.go('/networks');
    } else if (auth.status == AuthStatus.mfaRequired) {
      context.go('/mfa');
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = context.watch<AuthProvider>();
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Form(
                key: _formKey,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Icon(
                      Icons.hub_outlined,
                      size: 56,
                      color: Theme.of(context).colorScheme.primary,
                    ),
                    const SizedBox(height: 12),
                    Text(
                      'Welcome back',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      'Sign in to your NexusVPN account',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                    const SizedBox(height: 24),

                    // Offered before the form, because it is the easier path
                    // and the one that cannot be got wrong: nothing to type,
                    // and the address comes with the code.
                    FilledButton.icon(
                      onPressed: auth.busy ? null : _scanToPair,
                      icon: const Icon(Icons.qr_code_scanner),
                      label: const Text('Scan code from your computer'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(48),
                      ),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        const Expanded(child: Divider()),
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 12),
                          child: Text(
                            'or sign in',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ),
                        const Expanded(child: Divider()),
                      ],
                    ),
                    const SizedBox(height: 20),

                    if (auth.errorMessage != null) ...[
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: Theme.of(context)
                              .colorScheme
                              .errorContainer,
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Text(
                          auth.errorMessage!,
                          style: TextStyle(
                            color: Theme.of(context)
                                .colorScheme
                                .onErrorContainer,
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                    ],
                    TextFormField(
                      controller: _serverController,
                      keyboardType: TextInputType.url,
                      autocorrect: false,
                      decoration: const InputDecoration(
                        labelText: 'Server',
                        hintText: 'http://192.168.1.20:8080',
                        helperText: 'The machine running NexusVPN',
                        prefixIcon: Icon(Icons.dns_outlined),
                        border: OutlineInputBorder(),
                      ),
                      validator: (v) {
                        try {
                          validateServerUrl(v ?? '');
                          return null;
                        } on InsecureServerUrl catch (e) {
                          return e.message;
                        } on FormatException catch (e) {
                          return e.message;
                        }
                      },
                    ),
                    const SizedBox(height: 16),
                    TextFormField(
                      controller: _emailController,
                      keyboardType: TextInputType.emailAddress,
                      autofillHints: const [AutofillHints.email],
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
                    const SizedBox(height: 16),
                    TextFormField(
                      controller: _passwordController,
                      obscureText: _obscure,
                      autofillHints: const [AutofillHints.password],
                      decoration: InputDecoration(
                        labelText: 'Password',
                        prefixIcon: const Icon(Icons.lock_outline),
                        border: const OutlineInputBorder(),
                        suffixIcon: IconButton(
                          icon: Icon(
                            _obscure
                                ? Icons.visibility_outlined
                                : Icons.visibility_off_outlined,
                          ),
                          onPressed: () => setState(() => _obscure = !_obscure),
                        ),
                      ),
                      validator: (v) => (v == null || v.isEmpty)
                          ? 'Password is required'
                          : null,
                      onFieldSubmitted: (_) => _submit(),
                    ),
                    Align(
                      alignment: Alignment.centerRight,
                      child: TextButton(
                        onPressed: () => context.push('/forgot-password'),
                        child: const Text('Forgot password?'),
                      ),
                    ),
                    const SizedBox(height: 8),
                    PrimaryButton(
                      label: 'Sign In',
                      loading: auth.busy,
                      onPressed: _submit,
                    ),
                    const SizedBox(height: 16),
                    // Wrap rather than Row: a Row overflows here on a narrow
                    // screen or at a large font size, and the overflow is the
                    // only way to reach registration.
                    Wrap(
                      alignment: WrapAlignment.center,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        const Text("Don't have an account?"),
                        TextButton(
                          onPressed: () => context.push('/register'),
                          child: const Text('Sign up'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
