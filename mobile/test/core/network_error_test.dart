import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/app_exception.dart';

/// A network failure is where somebody is most likely to be stuck, so the
/// message is the whole of the help they get. These check it names the
/// mistake rather than describing the symptom.
void main() {
  test('loopback is explained as the phone itself', () {
    for (final host in ['127.0.0.1', 'localhost', '::1']) {
      final e = ApiException.network('refused', host: host);
      expect(
        e.message,
        contains('means the phone itself'),
        reason: '$host is the commonest wrong answer and the least obvious',
      );
      expect(e.message, contains('192.168'), reason: 'show what right looks like');
    }
  });

  test('any other host names itself and what to check', () {
    final e = ApiException.network('refused', host: '192.168.1.20');
    expect(e.message, contains('192.168.1.20'));
    expect(e.message, contains('same network'));
  });

  // Settings cannot be opened while signed out — the router sends every route
  // back to sign-in — so sending somebody there is a loop, and this is where
  // the old message sent them.
  test('the message never sends somebody to Settings', () {
    for (final host in ['127.0.0.1', '192.168.1.20', 'example.com']) {
      expect(
        ApiException.network('refused', host: host).message,
        isNot(contains('Settings')),
      );
    }
  });

  test('the underlying error is kept, because it is what gets reported back',
      () {
    final e = ApiException.network('SocketException: refused', host: 'x');
    expect(e.message, contains('SocketException: refused'));
    expect(e.isNetworkFailure, isTrue);
  });
}
