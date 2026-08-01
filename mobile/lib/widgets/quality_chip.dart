import 'package:flutter/material.dart';

import '../core/connection_quality.dart';
import 'status_badge.dart';

/// The one-word verdict on a connection, coloured by severity.
///
/// The colours are fixed rather than drawn from the theme's seed, because
/// they carry meaning: green means good on any accent, and a "Limited" that
/// picks up the brand colour stops warning anybody.
class QualityChip extends StatelessWidget {
  const QualityChip({super.key, required this.quality});

  final ConnectionQuality quality;

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final color = switch (quality) {
      ConnectionQuality.excellent =>
        dark ? const Color(0xFF34D399) : const Color(0xFF047857),
      ConnectionQuality.good =>
        dark ? const Color(0xFF60A5FA) : const Color(0xFF1D4ED8),
      ConnectionQuality.limited =>
        dark ? const Color(0xFFFBBF24) : const Color(0xFFB45309),
      ConnectionQuality.offline => Theme.of(context).colorScheme.outline,
    };
    return StatusBadge(label: quality.label, color: color);
  }
}
