import 'package:flutter/material.dart';

import '../core/deck_theme.dart';

/// Four bars, the way Radmin shows reachability.
///
/// This answers "can I use this machine right now" faster than any number:
/// the shape is readable at a glance and from the corner of your eye, and it
/// degrades gracefully for anyone who cannot separate the colours, because
/// the height carries the same information as the hue.
///
/// The number is still shown, in its own column. The bars are the summary,
/// not a replacement.
class SignalBars extends StatelessWidget {
  const SignalBars({
    super.key,
    required this.latencyMs,
    required this.online,
    this.relayed = false,
    this.height = 14,
  });

  final int? latencyMs;
  final bool online;

  /// A relayed path is capped at three bars however fast it measures: it
  /// costs an extra hop, and full strength would promise otherwise.
  final bool relayed;

  final double height;

  @override
  Widget build(BuildContext context) {
    final strength = online ? _bars(latencyMs, relayed) : 0;
    final color = !online
        ? Deck.inkFaint
        : relayed
            ? Deck.work
            : Deck.live;

    return SizedBox(
      width: height + 6,
      height: height,
      child: Stack(
        alignment: Alignment.bottomRight,
        children: [
          if (!online)
            Positioned(
              left: 0,
              bottom: 0,
              child: Icon(Icons.close, size: height * 0.62, color: Deck.inkFaint),
            ),
          Row(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.end,
            children: List.generate(4, (i) {
              return Container(
                width: 2.5,
                height: height * (0.32 + i * 0.225),
                margin: const EdgeInsets.only(left: 1.5),
                decoration: BoxDecoration(
                  color: i < strength ? color : Deck.lineBright,
                  borderRadius: BorderRadius.circular(0.5),
                ),
              );
            }),
          ),
        ],
      ),
    );
  }

  /// How many bars a measurement earns.
  ///
  /// The thresholds are the ones `client/agent` uses to pick the word
  /// "Excellent" or "Good", so four bars and that word can never disagree.
  /// An established but unmeasured path gets three: showing one would say
  /// "barely reachable" about a link that is working fine.
  static int _bars(int? latencyMs, bool relayed) {
    if (relayed) return (latencyMs != null && latencyMs < 150) ? 3 : 2;
    if (latencyMs == null || latencyMs < 0) return 3;
    if (latencyMs < 50) return 4;
    if (latencyMs < 150) return 3;
    if (latencyMs < 300) return 2;
    return 1;
  }
}
