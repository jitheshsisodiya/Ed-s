import 'package:flutter/material.dart';

import '../core/connection_quality.dart';
import '../core/deck_theme.dart';
import 'status_badge.dart';

/// The one-word verdict on a connection, coloured by severity.
///
/// The colours are the deck's state colours rather than anything drawn from
/// the theme's seed, because they carry meaning: a "Limited" that picks up
/// the brand accent stops warning anybody.
class QualityChip extends StatelessWidget {
  const QualityChip({super.key, required this.quality});

  final ConnectionQuality quality;

  @override
  Widget build(BuildContext context) {
    final color = switch (quality) {
      ConnectionQuality.excellent => Deck.live,
      ConnectionQuality.good => Deck.live,
      ConnectionQuality.limited => Deck.work,
      ConnectionQuality.offline => Deck.inkFaint,
    };
    return StatusBadge(label: quality.label, color: color);
  }
}
