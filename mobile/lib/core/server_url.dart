/// Where the app is allowed to send an unencrypted request.
///
/// A self-hosted control plane usually runs on somebody's own machine, on
/// their own network, reached as `http://192.168.1.20:8080`. Getting a
/// certificate for that is not something a person can reasonably do, so
/// refusing plain HTTP outright would mean the app only worked for people who
/// had already solved a problem the app exists to avoid.
///
/// The answer is not to allow cleartext everywhere. It is to allow it exactly
/// where it is defensible — an address that cannot leave the local network —
/// and to require TLS for everything else. A server on the public internet
/// carries sign-in credentials and session tokens across networks nobody in
/// this conversation controls, and that has to be encrypted.
///
/// Android's own network security config cannot express this: it matches
/// hostnames, not address ranges, so the platform flag is either "all
/// cleartext" or "none". The manifest therefore permits cleartext and this is
/// the check that actually decides, applied at the single point where a
/// server address enters the app.
library;

/// Thrown when an address would send credentials in the clear across a
/// network the user does not control.
class InsecureServerUrl implements Exception {
  const InsecureServerUrl(this.message);
  final String message;

  @override
  String toString() => message;
}

/// Checks a control-plane address, returning it with any trailing slash
/// removed. Throws [InsecureServerUrl] or [FormatException] if it is not one
/// the app will use.
String validateServerUrl(String input) {
  final trimmed = input.trim();
  if (trimmed.isEmpty) {
    throw const FormatException('Enter your server address.');
  }

  final uri = Uri.tryParse(trimmed);
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
    throw const FormatException(
      'That does not look like an address. It should start with http:// or '
      'https:// — for example http://192.168.1.20:8080',
    );
  }

  final scheme = uri.scheme.toLowerCase();
  if (scheme != 'http' && scheme != 'https') {
    throw FormatException('$scheme addresses are not supported.');
  }

  if (scheme == 'http' && !isPrivateHost(uri.host)) {
    throw InsecureServerUrl(
      '${uri.host} is not on your local network, so http would send your '
      'password across the internet unencrypted. Use https:// instead.',
    );
  }

  var out = trimmed;
  while (out.endsWith('/')) {
    out = out.substring(0, out.length - 1);
  }
  return out;
}

/// Whether a host is one that cannot be routed off the local network.
///
/// Names are handled by convention rather than resolution: `localhost` and
/// mDNS `.local` names are local by definition, and resolving anything else
/// here would only tell us where it points right now, not where it will point
/// when the request is actually made.
bool isPrivateHost(String host) {
  final h = host.toLowerCase();
  if (h == 'localhost' || h.endsWith('.local') || h.endsWith('.localhost')) {
    return true;
  }

  // A bracketed IPv6 literal arrives from Uri.host without its brackets, but
  // a zone identifier can still be attached.
  final bare = h.split('%').first;
  final address = _parseIPv4(bare);
  if (address != null) {
    final [a, b, _, _] = address;
    return a == 10 // 10.0.0.0/8
        || a == 127 // 127.0.0.0/8, loopback
        || (a == 172 && b >= 16 && b <= 31) // 172.16.0.0/12
        || (a == 192 && b == 168) // 192.168.0.0/16
        || (a == 169 && b == 254) // 169.254.0.0/16, link-local
        || (a == 100 && b >= 64 && b <= 127); // 100.64.0.0/10, carrier NAT
  }

  if (bare.contains(':')) {
    if (bare == '::1') return true; // loopback
    if (bare.startsWith('fe8') ||
        bare.startsWith('fe9') ||
        bare.startsWith('fea') ||
        bare.startsWith('feb')) {
      return true; // fe80::/10, link-local
    }
    // fc00::/7, unique local. The second hex digit distinguishes it from
    // fe80::/10 above, which shares no prefix with these two.
    if (bare.startsWith('fc') || bare.startsWith('fd')) return true;
    return false;
  }

  // A name that is not one of the local conventions above. It may well
  // resolve to a private address, but the app cannot know that at the moment
  // somebody types it, and guessing wrong means sending a password in the
  // clear.
  return false;
}

/// Returns the four octets of a dotted-quad address, or null if the string is
/// not one. Deliberately strict: `10.1` and `010.0.0.1` are not addresses this
/// app should treat as private on a guess.
List<int>? _parseIPv4(String s) {
  final parts = s.split('.');
  if (parts.length != 4) return null;
  final out = <int>[];
  for (final part in parts) {
    if (part.isEmpty || part.length > 3) return null;
    if (part.length > 1 && part.startsWith('0')) return null;
    final n = int.tryParse(part);
    if (n == null || n < 0 || n > 255) return null;
    out.add(n);
  }
  return out;
}
