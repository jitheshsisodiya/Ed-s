import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

import 'core/app_settings.dart';
import 'core/deck_theme.dart';
import 'core/auth_provider.dart';
import 'core/router.dart';
import 'core/vpn_controller.dart';

class NexusVpnApp extends StatefulWidget {
  const NexusVpnApp({super.key});

  @override
  State<NexusVpnApp> createState() => _NexusVpnAppState();
}

class _NexusVpnAppState extends State<NexusVpnApp> {
  late final AuthProvider _authProvider;
  late final VpnController _vpnController;
  late final Preferences _preferences;
  late final GoRouter _router;

  @override
  void initState() {
    super.initState();
    _authProvider = AuthProvider();
    _vpnController = VpnController(api: _authProvider.api);
    _preferences = Preferences();
    _router = buildRouter(_authProvider);
    _authProvider.bootstrap();
    _preferences.load();
  }

  @override
  void dispose() {
    _vpnController.dispose();
    _authProvider.dispose();
    _preferences.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        ChangeNotifierProvider<AuthProvider>.value(value: _authProvider),
        ChangeNotifierProvider<VpnController>.value(value: _vpnController),
        ChangeNotifierProvider<Preferences>.value(value: _preferences),
      ],
      // The deck commits to one visual world: the accent colour carries
      // state, and the glows that make it legible have nothing to glow
      // against on a light ground. A theme switch here would not be a
      // preference, it would be a second design.
      child: MaterialApp.router(
        title: 'NexusVPN',
        debugShowCheckedModeBanner: false,
        theme: Deck.theme(),
        darkTheme: Deck.theme(),
        themeMode: ThemeMode.dark,
        routerConfig: _router,
      ),
    );
  }
}
