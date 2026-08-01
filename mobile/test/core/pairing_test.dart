import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/pairing.dart';

/// Pinned to the same cases as `client/agent/pairing_test.go`, because the
/// link is the entire interface between two devices that have never met: the
/// desktop writes it, this reads it, and neither can see the other's tests.
void main() {
  group('links the desktop produces', () {
    test('the standard form', () {
      final p = parsePairingLink(
        'nexusvpn://pair?s=http%3A%2F%2F192.168.1.20%3A8080&t=abc.def.ghi',
      );
      expect(p, isNotNull);
      expect(p!.serverUrl, 'http://192.168.1.20:8080');
      expect(p.token, 'abc.def.ghi');
    });

    test('the opaque form, which some QR readers produce', () {
      final p = parsePairingLink('nexusvpn:pair?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok');
      expect(p?.serverUrl, 'http://10.0.0.5:8080');
      expect(p?.token, 'tok');
    });

    test('mixed case scheme, because scanners rewrite them', () {
      final p = parsePairingLink('NexusVPN://PAIR?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok');
      expect(p?.serverUrl, 'http://10.0.0.5:8080');
    });

    test('surrounding whitespace from a paste', () {
      final p = parsePairingLink('  nexusvpn://pair?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok  ');
      expect(p?.token, 'tok');
    });

    // Not hand-written: produced by running the same url.Values.Encode()
    // the desktop uses, with a real JWT shape in it. A parser tested only
    // against links its own author typed can pass while rejecting every link
    // the other half of the system actually emits.
    test('a link captured from the Go generator, verbatim', () {
      const generated =
          'nexusvpn://pair?s=http%3A%2F%2F192.168.1.20%3A8080'
          '&t=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiI3ZjNhIn0.sig';
      final p = parsePairingLink(generated);
      expect(p, isNotNull, reason: 'the phone cannot read what the desktop writes');
      expect(p!.serverUrl, 'http://192.168.1.20:8080');
      expect(
        p.token,
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiI3ZjNhIn0.sig',
        reason: 'a JWT contains dots, which a sloppy split would eat',
      );
    });
  });

  group('what is not a pairing link', () {
    // An invite joins a network you are already signed in for; a pairing code
    // signs you in. Reading one as the other would claim against a token that
    // is not one.
    test('an invite link', () {
      expect(parsePairingLink('nexusvpn://join/ABC123'), isNull);
    });
    test('a code with no server cannot be redeemed anywhere', () {
      expect(parsePairingLink('nexusvpn://pair?t=tok'), isNull);
    });
    test('a server with no code is not an offer of anything', () {
      expect(parsePairingLink('nexusvpn://pair?s=http%3A%2F%2F10.0.0.5'), isNull);
    });
    test("somebody else's scheme", () {
      expect(parsePairingLink('otherapp://pair?s=http%3A%2F%2Fx&t=tok'), isNull);
    });
    test('an ordinary web address', () {
      expect(parsePairingLink('https://example.com/pair?s=x&t=y'), isNull);
    });
    test('empty', () {
      expect(parsePairingLink('   '), isNull);
    });
    test('a bare word', () {
      expect(parsePairingLink('ABC123'), isNull);
    });
  });
}
