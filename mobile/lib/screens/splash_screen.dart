import 'package:flutter/material.dart';

import '../widgets/loading_indicator.dart';

/// Shown for the brief moment while [AuthProvider.bootstrap] checks
/// persisted tokens; the router redirects away as soon as auth status is
/// known.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.hub_outlined, size: 64, color: theme.colorScheme.primary),
            const SizedBox(height: 16),
            Text('NexusVPN', style: theme.textTheme.headlineSmall),
            const SizedBox(height: 24),
            const LoadingIndicator(),
          ],
        ),
      ),
    );
  }
}
