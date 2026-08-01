import 'invite.dart' show kInviteScheme;

/// Where a pairing link points, and what it is worth.
class PairingLink {
  const PairingLink({
    required this.serverUrl,
    required this.token,
    this.fingerprint = '',
  });

  /// The machine running NexusVPN. Carried in the link because it is the
  /// part nobody can be expected to know — it is whatever private address
  /// the router handed that machine — and having it travel with the code is
  /// most of the reason pairing exists.
  final String serverUrl;

  /// A single-use, short-lived credential that buys a session on that server.
  final String token;

  /// The certificate that server must present. Empty for a deployment with a
  /// real certificate, where the platform's own verification is enough.
  final String fingerprint;
}

/// The host part of `nexusvpn://pair?…`, kept distinct from `join` so a
/// reader can tell a code that carries a session from one that does not.
const String kPairingHost = 'pair';

/// Reads a pairing link, or returns null if this is not one.
///
/// Mirrors `client/agent/pairing.go`; the two are pinned to the same cases so
/// a code one produces is a code the other accepts.
///
/// Both halves are required. A token with no server cannot be redeemed
/// anywhere, and a server with no token is not an offer of anything.
PairingLink? parsePairingLink(String input) {
  final s = input.trim();
  if (s.isEmpty) return null;

  final uri = Uri.tryParse(s);
  if (uri == null || uri.scheme.toLowerCase() != kInviteScheme) return null;

  // nexusvpn://pair?… parses with host "pair" and an empty path; the opaque
  // form nexusvpn:pair?… puts it in the path instead, and some QR readers
  // produce that.
  final where = (uri.host.isNotEmpty ? uri.host : uri.path)
      .replaceAll('/', '')
      .toLowerCase();
  if (where != kPairingHost) return null;

  final server = (uri.queryParameters['s'] ?? '').trim();
  final token = (uri.queryParameters['t'] ?? '').trim();
  final fingerprint = (uri.queryParameters['f'] ?? '').trim();
  if (server.isEmpty || token.isEmpty) return null;

  return PairingLink(
    serverUrl: server,
    token: token,
    fingerprint: fingerprint,
  );
}
