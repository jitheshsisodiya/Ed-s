import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

/// An HTTP client that will talk to exactly one server: the one holding the
/// certificate with this fingerprint.
///
/// The control plane is a machine on somebody's desk. No certificate
/// authority will vouch for 192.168.1.20 or a home IP address, and there is
/// no domain to prove ownership of, so the usual arrangement is unavailable —
/// not skipped.
///
/// What replaces it is stronger for this shape of problem. The fingerprint
/// arrives in the pairing QR code, over a channel no network attacker is on:
/// a screen in the same room. From then on this trusts exactly one key,
/// rather than any of the hundreds of authorities the platform trusts.
///
/// Mirrors `client/internal/apiclient/pinned.go`.
class PinnedHttpClient {
  /// Returns a client pinned to [fingerprint], or an ordinary one if it is
  /// empty — for a deployment that has a real certificate.
  ///
  /// A fingerprint that is not a SHA-256 is treated as no fingerprint at all
  /// rather than as one that matches nothing, because a malformed pin must
  /// never quietly become "trust anything". Callers should reject an
  /// unusable value before getting here; [isUsableFingerprint] says whether
  /// it is one.
  static http.Client create(String fingerprint) {
    final want = normalizeFingerprint(fingerprint);
    if (want.isEmpty) return http.Client();

    final io = HttpClient()
      ..badCertificateCallback = (X509Certificate cert, String host, int port) {
        // Reached only when the platform's own verification has already
        // failed, which for a self-signed certificate is always. The question
        // being answered is not "is this certificate valid" but "is this the
        // machine I paired with".
        final got = sha256.convert(cert.der).toString().toLowerCase();
        return got == want;
      };
    return IOClient(io);
  }

  /// Whether a string is a SHA-256 fingerprint in any of the forms one gets
  /// written in: upper or lower case, with or without separating colons or
  /// spaces.
  static bool isUsableFingerprint(String fingerprint) =>
      normalizeFingerprint(fingerprint).isNotEmpty;

  /// Strips separators and case. Returns empty for anything that is not 64
  /// hex characters, because a SHA-256 is exactly that and treating a shorter
  /// value as a pin would silently trust whatever it happened to be.
  static String normalizeFingerprint(String fingerprint) {
    final buffer = StringBuffer();
    for (final rune in fingerprint.toLowerCase().runes) {
      final c = String.fromCharCode(rune);
      if ((c.compareTo('0') >= 0 && c.compareTo('9') <= 0) ||
          (c.compareTo('a') >= 0 && c.compareTo('f') <= 0)) {
        buffer.write(c);
      }
    }
    final out = buffer.toString();
    return out.length == 64 ? out : '';
  }
}
