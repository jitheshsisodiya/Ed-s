import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/reachability.dart';

/// The failure this exists for: a phone on mobile data, a correct 192.168
/// address, and twenty seconds of nothing. The general message sends somebody
/// to re-check an address that was right all along, so they check it, find it
/// right, and try again.
void main() {
  const lanHost = '192.168.31.91';

  test('mobile data against a home address names the cause', () {
    final why = Reachability.explainWith(
      host: lanHost,
      connection: [ConnectivityResult.mobile],
    );
    expect(why, isNotNull);
    expect(why, contains('mobile data'));
    expect(why, contains(lanHost), reason: 'name the address being explained');
    expect(why, contains('Wi-Fi'), reason: 'say what to do about it');
  });

  test('no connection at all is its own answer', () {
    final why = Reachability.explainWith(
      host: lanHost,
      connection: [ConnectivityResult.none],
    );
    expect(why, contains('no network connection'));
  });

  test('an empty answer from the platform is treated as no connection', () {
    expect(
      Reachability.explainWith(host: lanHost, connection: []),
      contains('no network connection'),
    );
  });

  group('nothing specific to add', () {
    // On the right network and still unreachable is a real problem, and not
    // one this can diagnose. Guessing would bury the general message under a
    // confident wrong answer.
    for (final on in <ConnectivityResult>[
      ConnectivityResult.wifi,
      ConnectivityResult.ethernet,
      ConnectivityResult.vpn,
    ]) {
      test('on ${on.name}, say nothing', () {
        expect(Reachability.explainWith(host: lanHost, connection: [on]), isNull);
      });
    }

    // A public address is reachable from anywhere or nowhere; which network
    // this phone is on has no bearing on it.
    test('a public address is not about the phone', () {
      expect(
        Reachability.explainWith(
          host: 'vpn.example.com',
          connection: [ConnectivityResult.mobile],
        ),
        isNull,
      );
    });
  });

  test('a phone on both mobile and Wi-Fi is on Wi-Fi', () {
    // Android reports several at once during a handover. Treating that as
    // "mobile only" would show the message to somebody who is on the right
    // network.
    expect(
      Reachability.explainWith(
        host: lanHost,
        connection: [ConnectivityResult.mobile, ConnectivityResult.wifi],
      ),
      isNull,
    );
  });

  group('an address left over from before the server spoke TLS', () {
    // Dart reports a server hanging up mid-handshake as headers never
    // arriving, which describes the symptom and hides the cause. Re-typing
    // the same address does not fix it.
    for (final error in [
      'Connection closed before full header was received',
      'Connection reset by peer',
      'HandshakeException: ...',
    ]) {
      test('suggests https for: $error', () {
        final why = Reachability.explainScheme(
          serverUrl: 'http://192.168.31.91:8080',
          error: error,
        );
        expect(why, isNotNull);
        expect(why, contains('https://192.168.31.91:8080'));
      });
    }

    test('says nothing when the address is already https', () {
      expect(
        Reachability.explainScheme(
          serverUrl: 'https://192.168.31.91:8080',
          error: 'Connection closed before full header was received',
        ),
        isNull,
      );
    });

    test('says nothing for an unrelated failure', () {
      expect(
        Reachability.explainScheme(
          serverUrl: 'http://192.168.31.91:8080',
          error: 'Connection timed out',
        ),
        isNull,
      );
    });
  });

  test('every private range is covered, not just 192.168', () {
    for (final host in ['10.0.0.5', '172.16.4.4', '192.168.1.20']) {
      expect(
        Reachability.explainWith(host: host, connection: [ConnectivityResult.mobile]),
        isNotNull,
        reason: '$host is a home address too',
      );
    }
  });
}
