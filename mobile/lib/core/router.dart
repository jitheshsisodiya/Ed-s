import 'package:go_router/go_router.dart';

import 'auth_provider.dart';
import '../screens/devices_screen.dart';
import '../screens/forgot_password_screen.dart';
import '../screens/login_screen.dart';
import '../screens/mfa_screen.dart';
import '../screens/network_detail_screen.dart';
import '../screens/networks_screen.dart';
import '../screens/register_screen.dart';
import '../screens/settings_screen.dart';
import '../screens/splash_screen.dart';

GoRouter buildRouter(AuthProvider auth) {
  return GoRouter(
    initialLocation: '/',
    refreshListenable: auth,
    redirect: (context, state) {
      final loc = state.matchedLocation;
      final onAuthPages = loc == '/login' ||
          loc == '/register' ||
          loc == '/forgot-password';

      switch (auth.status) {
        case AuthStatus.unknown:
          return loc == '/' ? null : '/';
        case AuthStatus.mfaRequired:
          return loc == '/mfa' ? null : '/mfa';
        case AuthStatus.unauthenticated:
          if (loc == '/' || (!onAuthPages && loc != '/mfa')) {
            return '/login';
          }
          return null;
        case AuthStatus.authenticated:
          if (loc == '/' || onAuthPages || loc == '/mfa') {
            return '/networks';
          }
          return null;
      }
    },
    routes: [
      GoRoute(path: '/', builder: (context, state) => const SplashScreen()),
      GoRoute(
        path: '/login',
        builder: (context, state) => const LoginScreen(),
      ),
      GoRoute(
        path: '/register',
        builder: (context, state) => const RegisterScreen(),
      ),
      GoRoute(
        path: '/mfa',
        builder: (context, state) => const MfaScreen(),
      ),
      GoRoute(
        path: '/forgot-password',
        builder: (context, state) => const ForgotPasswordScreen(),
      ),
      GoRoute(
        path: '/networks',
        builder: (context, state) => const NetworksScreen(),
      ),
      GoRoute(
        path: '/networks/:id',
        builder: (context, state) => NetworkDetailScreen(
          networkId: state.pathParameters['id']!,
        ),
      ),
      GoRoute(
        path: '/networks/:id/devices',
        builder: (context, state) => DevicesScreen(
          networkId: state.pathParameters['id']!,
        ),
      ),
      GoRoute(
        path: '/settings',
        builder: (context, state) => const SettingsScreen(),
      ),
    ],
  );
}
