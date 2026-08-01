import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/models/models.dart';
import 'package:nexusvpn/core/remembered_peers.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A phone away from home can reach its peers but not always the machine that
/// hands out the list of them. What was true last time is the best
/// information available, and without it the tunnel simply does not come up.
void main() {
  setUp(() {
    TestWidgetsFlutterBinding.ensureInitialized();
    SharedPreferences.setMockInitialValues({});
  });

  Device peer(String id, String ip, String key) => Device(
        id: id,
        name: 'machine-$id',
        os: 'linux',
        publicKey: key,
        virtualIp: ip,
        // The endpoint is what a phone away from home actually dials, so a
        // remembered peer without one is remembered for nothing.
        lastPublicIp: '203.0.113.7',
        status: 'online',
      );

  test('peers survive being written and read back', () async {
    await RememberedPeers.save('device-1', [
      peer('p1', '10.77.0.2', 'key-one'),
      peer('p2', '10.77.0.4', 'key-two'),
    ]);

    final got = await RememberedPeers.load('device-1');
    expect(got, hasLength(2));
    // The key and the address are the whole point: without them the tunnel
    // comes up with nothing to talk to, which looks like working.
    expect(got.first.publicKey, 'key-one');
    expect(got.first.virtualIp, '10.77.0.2');
    expect(got.first.lastPublicIp, '203.0.113.7',
        reason: 'without the endpoint there is nothing to dial from away');
    expect(got.last.publicKey, 'key-two');
  });

  test('each device remembers its own peers', () async {
    await RememberedPeers.save('device-1', [peer('p1', '10.77.0.2', 'key-one')]);
    await RememberedPeers.save('device-2', [
      peer('p9', '10.88.0.9', 'key-nine'),
    ]);

    expect((await RememberedPeers.load('device-1')).single.publicKey, 'key-one');
    expect((await RememberedPeers.load('device-2')).single.publicKey, 'key-nine');
  });

  test('a device with nothing remembered gets an empty list', () async {
    expect(await RememberedPeers.load('never-connected'), isEmpty);
  });

  test('an empty device id is not a key', () async {
    // Saving under an empty id would put one device's peers where any other
    // device with no id would find them.
    await RememberedPeers.save('', [peer('p1', '10.77.0.2', 'key-one')]);
    expect(await RememberedPeers.load(''), isEmpty);
  });

  test('unreadable contents are treated as absent, not as a crash', () async {
    SharedPreferences.setMockInitialValues({
      'nexusvpn.peers.device-1': 'not json at all',
    });
    expect(await RememberedPeers.load('device-1'), isEmpty);
  });

  test('a later save replaces the earlier one', () async {
    await RememberedPeers.save('device-1', [peer('p1', '10.77.0.2', 'old')]);
    await RememberedPeers.save('device-1', [peer('p1', '10.77.0.2', 'new')]);

    final got = await RememberedPeers.load('device-1');
    expect(got, hasLength(1));
    expect(got.single.publicKey, 'new');
  });

  test('the age is recorded so somebody can be told how stale this is',
      () async {
    await RememberedPeers.save('device-1', [peer('p1', '10.77.0.2', 'k')]);
    final age = await RememberedPeers.age('device-1');
    expect(age, isNotNull);
    expect(age!.inMinutes, lessThan(1));
  });

  test('describe puts an age into words', () {
    expect(RememberedPeers.describe(const Duration(minutes: 7)), '7 minutes old');
    expect(RememberedPeers.describe(const Duration(hours: 5)), '5 hours old');
    expect(RememberedPeers.describe(const Duration(days: 3)), '3 days old');
    expect(RememberedPeers.describe(null), 'unknown age');
  });
}
