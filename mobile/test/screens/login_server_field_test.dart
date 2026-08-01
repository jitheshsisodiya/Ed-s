import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:nexusvpn/core/api_client.dart';
import 'package:nexusvpn/core/auth_provider.dart';
import 'package:nexusvpn/screens/login_screen.dart';
import 'package:provider/provider.dart';

/// The sign-in screen must be able to answer "which server?" itself.
///
/// It is the only screen reachable while signed out — the router sends every
/// other route to /login — so if the address can only be changed in Settings,
/// a wrong one is a dead end: sign-in fails against a server that is not
/// there, and the screen that could fix it is behind the sign-in that cannot
/// succeed. These tests exist because that is exactly what shipped.
void main() {
  Future<void> pumpLogin(WidgetTester tester, AuthProvider auth) async {
    await tester.pumpWidget(
      ChangeNotifierProvider<AuthProvider>.value(
        value: auth,
        child: MaterialApp.router(
          routerConfig: GoRouter(
            routes: [
              GoRoute(path: '/', builder: (_, _) => const LoginScreen()),
              GoRoute(path: '/register', builder: (_, _) => const SizedBox()),
              GoRoute(
                path: '/forgot-password',
                builder: (_, _) => const SizedBox(),
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('the server address can be set from sign-in', (tester) async {
    final auth = AuthProvider(apiClient: ApiClient());
    await pumpLogin(tester, auth);

    expect(
      find.widgetWithText(TextFormField, 'Server'),
      findsOneWidget,
      reason: 'without this field a wrong address cannot be corrected',
    );
  });

  testWidgets('an address that would send the password in the clear is '
      'refused before anything is sent', (tester) async {
    final auth = AuthProvider(apiClient: ApiClient());
    await pumpLogin(tester, auth);

    await tester.enterText(
      find.widgetWithText(TextFormField, 'Server'),
      'http://api.example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'Email'),
      'someone@example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'Password'),
      'hunter2hunter2',
    );
    await tester.tap(find.text('Sign In'));
    await tester.pump();

    expect(find.textContaining('Use https'), findsOneWidget);
  });

  testWidgets('a server on the local network is accepted over plain http',
      (tester) async {
    final auth = AuthProvider(apiClient: ApiClient());
    await pumpLogin(tester, auth);

    await tester.enterText(
      find.widgetWithText(TextFormField, 'Server'),
      'http://192.168.1.20:8080',
    );
    await tester.pump();

    final form = tester.state<FormState>(find.byType(Form));
    // Only the server field is under test, so the others are filled in to
    // keep their own validators quiet.
    await tester.enterText(
      find.widgetWithText(TextFormField, 'Email'),
      'someone@example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'Password'),
      'hunter2hunter2',
    );
    expect(form.validate(), isTrue);
  });
}
