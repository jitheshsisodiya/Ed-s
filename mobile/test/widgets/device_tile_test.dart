import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/models/models.dart';
import 'package:nexusvpn/widgets/device_tile.dart';

Widget _wrap(Widget child) =>
    MaterialApp(home: Scaffold(body: ListView(children: [child])));

Device _device({
  String status = 'online',
  int? latencyMs,
  String? virtualIp = '100.84.0.12',
  DateTime? lastSeenAt,
}) {
  return Device(
    id: 'd-1',
    name: 'Studio Desktop',
    os: 'windows',
    publicKey: 'k',
    virtualIp: virtualIp,
    status: status,
    latencyMs: latencyMs,
    lastSeenAt: lastSeenAt,
  );
}

void main() {
  testWidgets('a device is addressed by name, never by address', (tester) async {
    await tester.pumpWidget(_wrap(DeviceTile(device: _device(latencyMs: 12))));

    expect(find.text('Studio Desktop'), findsOneWidget);
    expect(find.text('Direct connection'), findsOneWidget);
    // Case-insensitive: the pill uppercases for the eye, and the test cares
    // that the verdict is present, not how it is cased.
    expect(
      find.textContaining(RegExp('excellent', caseSensitive: false)),
      findsOneWidget,
    );
    // The address and the raw number are the things Advanced mode exists for.
    expect(find.textContaining('100.84.0.12'), findsNothing);
    expect(find.textContaining('12 ms'), findsNothing);
  });

  testWidgets('Advanced mode reveals the address and the measurement',
      (tester) async {
    await tester.pumpWidget(
      _wrap(DeviceTile(device: _device(latencyMs: 12), advanced: true)),
    );

    expect(find.textContaining('100.84.0.12'), findsOneWidget);
    expect(find.textContaining('12 ms'), findsOneWidget);
  });

  testWidgets('an offline device says when it was last seen', (tester) async {
    await tester.pumpWidget(
      _wrap(
        DeviceTile(
          device: _device(
            status: 'offline',
            lastSeenAt: DateTime.now().subtract(const Duration(hours: 2)),
          ),
        ),
      ),
    );

    expect(
      find.textContaining(RegExp('offline', caseSensitive: false)),
      findsOneWidget,
    );
    expect(find.text('Last seen 2 hr ago'), findsOneWidget);
  });

  testWidgets('a device that has never connected says so plainly',
      (tester) async {
    await tester.pumpWidget(
      _wrap(DeviceTile(device: _device(status: 'offline', virtualIp: null))),
    );

    expect(find.text('Not connected yet'), findsOneWidget);
  });
}
