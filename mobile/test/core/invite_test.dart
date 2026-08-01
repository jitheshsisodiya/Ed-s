import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/invite.dart';

/// The same table as `TestParseInviteCode` in client/agent/invite_test.go.
/// A QR generated on the desktop is scanned here, so the two parsers have to
/// agree on every form an invite can arrive in.
void main() {
  const cases = <(String, String, String)>[
    ('bare code', 'K7M2QP', 'K7M2QP'),
    ('lower case', 'k7m2qp', 'K7M2QP'),
    ('padded by a copy-paste', '  K7M2QP\n', 'K7M2QP'),
    ('app link', 'nexusvpn://join/K7M2QP', 'K7M2QP'),
    ('app link, opaque form', 'nexusvpn:join/K7M2QP', 'K7M2QP'),
    ('app link with a trailing slash', 'nexusvpn://join/K7M2QP/', 'K7M2QP'),
    ('app link, mixed case scheme', 'NexusVPN://join/k7m2qp', 'K7M2QP'),
    ('web link', 'https://nexus.example.com/join/K7M2QP', 'K7M2QP'),
    (
      'web link with a query string',
      'https://nexus.example.com/join/K7M2QP?from=email',
      'K7M2QP',
    ),
    ('plain http', 'http://nexus.example.com/join/K7M2QP', 'K7M2QP'),
    ('empty', '', ''),
    ('whitespace only', '   ', ''),
    ("someone else's scheme", 'mailto:alice@example.com', ''),
    ('unrelated scheme', 'ftp://example.com/join/K7M2QP', ''),
  ];

  group('parseInviteCode', () {
    for (final (name, input, want) in cases) {
      test(name, () => expect(parseInviteCode(input), want));
    }
  });

  test('a link we generate is a link we accept', () {
    for (final code in ['K7M2QP', 'b3xq7t', '0123456789ABCDEF']) {
      expect(parseInviteCode(inviteLink(code)), parseInviteCode(code));
    }
  });
}
