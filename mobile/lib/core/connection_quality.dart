/// Connection health, in the words shown to users.
///
/// This is a deliberate copy of `describeQuality` in `client/agent/agent.go`.
/// The two live in different languages and cannot share code, so the rule is
/// stated in one place per platform and pinned by tests on both sides — a
/// connection that reads "Good" on the desktop must not read "Excellent" on
/// the phone.
enum ConnectionQuality {
  excellent('Excellent'),
  good('Good'),
  limited('Limited'),
  offline('Offline');

  const ConnectionQuality(this.label);

  final String label;

  /// Whether traffic is flowing at all.
  bool get isOnline => this != ConnectionQuality.offline;
}

/// Derives the single word a user sees from how traffic reaches a peer and
/// how long the round trip takes.
///
/// The thresholds are about perceptibility, not networking: under 50ms feels
/// instant, under 150ms feels responsive, and anything relayed is
/// working-but-slower by definition because it takes an extra hop. A null
/// [latencyMs] means "not measured yet" — with a direct path already up that
/// is reported as Good rather than pretending to be offline, because traffic
/// is already flowing and the user can see it.
ConnectionQuality describeQuality({required String mode, int? latencyMs}) {
  switch (mode) {
    case 'offline':
    case 'connecting':
    case '':
      return ConnectionQuality.offline;
    case 'relay':
      return ConnectionQuality.limited;
  }

  if (latencyMs == null || latencyMs < 0) return ConnectionQuality.good;
  if (latencyMs < 50) return ConnectionQuality.excellent;
  if (latencyMs < 150) return ConnectionQuality.good;
  return ConnectionQuality.limited;
}

/// The same judgement for a device as the REST API describes it, where the
/// path is not reported — only whether the control plane has seen it lately.
ConnectionQuality describeDeviceQuality({
  required bool online,
  int? latencyMs,
}) {
  if (!online) return ConnectionQuality.offline;
  return describeQuality(mode: 'direct', latencyMs: latencyMs);
}

/// How traffic reaches a peer, said without jargon. The quality word beside
/// it already carries the verdict, so this answers "why" rather than
/// repeating "Online".
String describePath(String mode) {
  switch (mode) {
    case 'direct':
      return 'Direct connection';
    case 'relay':
      return 'Connected through a relay';
    case 'connecting':
      return 'Finding the best route…';
    default:
      return 'Online';
  }
}
