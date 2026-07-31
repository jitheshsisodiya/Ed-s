import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

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
  late final GoRouter _router;

  @override
  void initState() {
    super.initState();
    _authProvider = AuthProvider();
    _vpnController = VpnController(api: _authProvider.api);
    _router = buildRouter(_authProvider);
    _authProvider.bootstrap();
  }

  @override
  void dispose() {
    _vpnController.dispose();
    _authProvider.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        ChangeNotifierProvider<AuthProvider>.value(value: _authProvider),
        ChangeNotifierProvider<VpnController>.value(value: _vpnController),
      ],
      child: MaterialApp.router(
        title: 'NexusVPN',
        debugShowCheckedModeBanner: false,
        theme: _buildTheme(Brightness.light),
        darkTheme: _buildTheme(Brightness.dark),
        themeMode: ThemeMode.system,
        routerConfig: _router,
      ),
    );
  }
}

ThemeData _buildTheme(Brightness brightness) {
  final seed = const Color(0xFF3D5CFF);
  final colorScheme = ColorScheme.fromSeed(
    seedColor: seed,
    brightness: brightness,
  );
  return ThemeData(
    colorScheme: colorScheme,
    useMaterial3: true,
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      filled: false,
    ),
    cardTheme: const CardThemeData(
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(14)),
      ),
    ),
    appBarTheme: const AppBarTheme(centerTitle: false, elevation: 0),
  );
}
