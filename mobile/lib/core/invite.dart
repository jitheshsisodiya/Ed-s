/// The URI a NexusVPN invite is shared as. A bare code works everywhere too;
/// the scheme exists so an invite can be sent as a link, put in a QR code, or
/// opened by the app without the recipient having to know which of those they
/// were given.
///
/// This mirrors `client/agent/invite.go` — the two implementations are pinned
/// to the same test table so a link one produces is a link the other accepts.
const String kInviteScheme = 'nexusvpn';

/// Extracts the invite code from whatever a person actually pasted or
/// scanned.
///
/// People do not paste codes; they paste the message they were sent. That is
/// a bare code, a `nexusvpn://join/CODE` link, an `https://host/join/CODE`
/// link from a browser, or any of those with a trailing slash, a query
/// string, or surrounding whitespace. Every one of those means the same
/// thing, so every one is accepted rather than answered with "invalid invite
/// code".
///
/// Returns an empty string for anything that is not an invite — including a
/// URL in some other scheme, because guessing at a stranger's last path
/// segment would send it to the server.
String parseInviteCode(String input) {
  var s = input.trim();
  if (s.isEmpty) return '';

  final uri = Uri.tryParse(s);
  if (uri != null && uri.hasScheme) {
    final scheme = uri.scheme.toLowerCase();
    if (scheme == kInviteScheme) {
      // nexusvpn://join/CODE parses with host "join" and path "/CODE";
      // nexusvpn:join/CODE parses with an empty host and the rest in path.
      s = _lastSegment('${uri.host}/${uri.path}');
    } else if (scheme == 'http' || scheme == 'https') {
      s = _lastSegment(uri.path);
    } else {
      return '';
    }
  }

  return s.replaceAll('/', '').trim().toUpperCase();
}

/// Renders a code as the shareable link form.
String inviteLink(String code) => '$kInviteScheme://join/${code.trim().toUpperCase()}';

/// The final path segment that is not the literal "join", so both
/// "join/K7M2QP" and "/join/K7M2QP/" yield "K7M2QP".
String _lastSegment(String path) {
  final parts = path.split('/');
  for (var i = parts.length - 1; i >= 0; i--) {
    final p = parts[i].trim();
    if (p.isNotEmpty && p.toLowerCase() != 'join') return p;
  }
  return '';
}
