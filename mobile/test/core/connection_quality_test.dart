import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/connection_quality.dart';

/// These cases are the same table as `TestDescribeQuality` in
/// client/agent/quality_test.go. The two implementations cannot share code,
/// so they share a test table instead: if either side drifts, one of the two
/// suites fails and says so.
void main() {
  group('describeQuality', () {
    const cases = <(String, String, int?, ConnectionQuality)>[
      ('direct and instant', 'direct', 12, ConnectionQuality.excellent),
      ('direct at the excellent boundary', 'direct', 49, ConnectionQuality.excellent),
      ('direct just past it', 'direct', 50, ConnectionQuality.good),
      ('direct and responsive', 'direct', 120, ConnectionQuality.good),
      ('direct but sluggish', 'direct', 150, ConnectionQuality.limited),
      ('direct and very slow', 'direct', 800, ConnectionQuality.limited),

      // A direct path exists but has not been probed yet. Reporting
      // "Offline" here would be a lie the user can see through, since
      // traffic is already flowing.
      ('direct, not yet probed', 'direct', null, ConnectionQuality.good),
      ('direct, sentinel latency', 'direct', -1, ConnectionQuality.good),

      // A relay always costs an extra hop, however fast it measures.
      ('relayed but fast', 'relay', 8, ConnectionQuality.limited),
      ('relayed and slow', 'relay', 300, ConnectionQuality.limited),

      ('offline', 'offline', null, ConnectionQuality.offline),
      ('still connecting', 'connecting', null, ConnectionQuality.offline),
      ('unknown mode', '', null, ConnectionQuality.offline),
    ];

    for (final (name, mode, latency, want) in cases) {
      test(name, () {
        expect(describeQuality(mode: mode, latencyMs: latency), want);
      });
    }
  });

  test('a device the server has not seen is Offline whatever its latency', () {
    expect(
      describeDeviceQuality(online: false, latencyMs: 5),
      ConnectionQuality.offline,
    );
    expect(
      describeDeviceQuality(online: true, latencyMs: 5),
      ConnectionQuality.excellent,
    );
    expect(
      describeDeviceQuality(online: true, latencyMs: null),
      ConnectionQuality.good,
    );
  });

  test('every quality label is one of the four sanctioned words', () {
    const allowed = {'Excellent', 'Good', 'Limited', 'Offline'};
    for (final mode in ['direct', 'relay', 'offline', 'connecting', '', 'nonsense']) {
      for (final latency in [null, -1, 0, 49, 50, 149, 150, 10000]) {
        final label = describeQuality(mode: mode, latencyMs: latency).label;
        expect(allowed, contains(label), reason: 'mode=$mode latency=$latency');
      }
    }
  });

  test('the path description never leaks the mode string', () {
    expect(describePath('direct'), 'Direct connection');
    expect(describePath('relay'), 'Connected through a relay');
    expect(describePath('connecting'), 'Finding the best route…');
    expect(describePath('something new'), 'Online');
  });
}
