import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/widgets/connect_button.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: Center(child: child)));

void main() {
  testWidgets('says what it will do, not what state it is in', (tester) async {
    await tester.pumpWidget(
      _wrap(ConnectButton(connected: false, busy: false, onPressed: () {})),
    );
    expect(find.text('Connect'), findsOneWidget);

    await tester.pumpWidget(
      _wrap(ConnectButton(connected: true, busy: false, onPressed: () {})),
    );
    await tester.pumpAndSettle();
    expect(find.text('Disconnect'), findsOneWidget);
  });

  testWidgets('shows progress instead of a label while it works',
      (tester) async {
    await tester.pumpWidget(
      _wrap(const ConnectButton(connected: false, busy: true, onPressed: null)),
    );

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.text('Connect'), findsNothing);
  });

  testWidgets('a disabled button does not fire', (tester) async {
    var taps = 0;
    await tester.pumpWidget(
      _wrap(const ConnectButton(connected: false, busy: false, onPressed: null)),
    );
    await tester.tap(find.byType(ConnectButton));
    await tester.pump();
    expect(taps, 0);

    await tester.pumpWidget(
      _wrap(ConnectButton(
        connected: false,
        busy: false,
        onPressed: () => taps++,
      )),
    );
    await tester.tap(find.byType(ConnectButton));
    await tester.pump();
    expect(taps, 1);
  });
}
