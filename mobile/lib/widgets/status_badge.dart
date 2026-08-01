import 'package:flutter/material.dart';

import '../core/deck_theme.dart';

/// A small outlined pill carrying one word of state.
///
/// Outlined rather than filled: on this ground a filled chip reads as a
/// button and invites a tap that does nothing. The border carries the
/// colour, which is all the colour needs to do.
class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.label, required this.color});

  factory StatusBadge.forDeviceStatus(String status) {
    return switch (status) {
      'online' => const StatusBadge(label: 'Online', color: Deck.live),
      'offline' => const StatusBadge(label: 'Offline', color: Deck.inkFaint),
      _ => const StatusBadge(label: 'Unknown', color: Deck.work),
    };
  }

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    // Uppercased for the eye, announced in its natural casing for the ear.
    // Screen readers spell out all-caps words letter by letter, which turns
    // "OFFLINE" into seven letters read aloud instead of one word.
    return Semantics(
      label: label,
      excludeSemantics: true,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
        decoration:
            BoxDecoration(border: Border.all(color: color.withValues(alpha: 0.45))),
        child: Text(
          label.toUpperCase(),
          style: Deck.mono(size: 9.5, color: color, spacing: 1.2),
        ),
      ),
    );
  }
}
