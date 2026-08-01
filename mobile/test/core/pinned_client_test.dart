import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/pinned_client.dart';

/// The pin is what makes a self-signed certificate safe, so the thing that
/// must never happen is an unusable pin quietly becoming "trust anything" —
/// the failure that looks exactly like success.
void main() {
  const valid =
      'aee195632066bcf54bd127965c554a010fdfb5e36576b925d1a89ecd610571e5';

  group('forms of the same fingerprint', () {
    test('lower case hex', () {
      expect(PinnedHttpClient.normalizeFingerprint(valid), valid);
    });
    test('upper case', () {
      expect(PinnedHttpClient.normalizeFingerprint(valid.toUpperCase()), valid);
    });
    test('separated by colons, as tools print them', () {
      final colons = <String>[
        for (var i = 0; i < valid.length; i += 2) valid.substring(i, i + 2),
      ].join(':');
      expect(PinnedHttpClient.normalizeFingerprint(colons), valid);
    });
    test('separated by spaces, as a person might read one aloud', () {
      final spaced = <String>[
        for (var i = 0; i < valid.length; i += 4) valid.substring(i, i + 4),
      ].join(' ');
      expect(PinnedHttpClient.normalizeFingerprint(spaced), valid);
    });
  });

  group('what is not a fingerprint is not treated as one', () {
    // Each of these must come back empty, and empty must mean "no pin was
    // supplied" — never "a pin that matches everything".
    for (final bad in <String>[
      '',
      '   ',
      'not-a-fingerprint',
      'abcd',
      'zz',
    ]) {
      test('${bad.isEmpty ? '(empty)' : bad}', () {
        expect(PinnedHttpClient.normalizeFingerprint(bad), '');
        expect(PinnedHttpClient.isUsableFingerprint(bad), isFalse);
      });
    }

    test('64 characters that are not hex', () {
      final notHex = 'z' * 64;
      expect(PinnedHttpClient.normalizeFingerprint(notHex), '');
    });

    // A SHA-1 fingerprint is 40 characters. Accepting one would pin against a
    // hash this code never computes, so nothing would ever match — a server
    // that could not be reached, with no explanation.
    test('a SHA-1 length fingerprint', () {
      expect(PinnedHttpClient.normalizeFingerprint('ab' * 20), '');
    });

    test('a truncated SHA-256', () {
      expect(PinnedHttpClient.normalizeFingerprint(valid.substring(0, 62)), '');
    });
  });

  test('a usable fingerprint is recognised as one', () {
    expect(PinnedHttpClient.isUsableFingerprint(valid), isTrue);
  });

  test('creating a client never throws, whatever it is given', () {
    for (final input in <String>['', valid, 'rubbish', 'ab' * 20]) {
      expect(() => PinnedHttpClient.create(input).close(), returnsNormally);
    }
  });
}
