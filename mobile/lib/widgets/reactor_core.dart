import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../core/deck_theme.dart';

/// The one control, translated from the desktop's reactor core.
///
/// It is larger here than on the desktop — a phone is held one-handed and
/// this is the only thing most sessions touch, so it sits in the thumb's
/// reach and is sized to be hit without looking.
///
/// The four states differ by motion as well as hue, because a hue-only
/// difference fails for the eight percent of men who cannot reliably
/// separate red from green: idle is still, linking sweeps, active breathes,
/// dropped pulses hard and off-rhythm.
class ReactorCore extends StatefulWidget {
  const ReactorCore({
    super.key,
    required this.phase,
    required this.label,
    required this.onPressed,
    this.size = 168,
  });

  final DeckPhase phase;
  final String label;
  final VoidCallback? onPressed;
  final double size;

  @override
  State<ReactorCore> createState() => _ReactorCoreState();
}

class _ReactorCoreState extends State<ReactorCore>
    with TickerProviderStateMixin {
  /// Drives the sweep while linking. One continuous rotation.
  late final AnimationController _sweep = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1600),
  );

  /// Drives the bloom: a slow breath while live, a hard flash while lost.
  late final AnimationController _pulse = AnimationController(vsync: this);

  /// Drives the snap when the state lands. A spring, not a fade — a
  /// mechanism engaging rather than a value changing.
  late final AnimationController _snap = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 420),
  );

  bool _pressed = false;

  @override
  void initState() {
    super.initState();
    _applyPhase();
  }

  @override
  void didUpdateWidget(covariant ReactorCore old) {
    super.didUpdateWidget(old);
    if (old.phase != widget.phase) {
      _applyPhase();
      _snap.forward(from: 0);
    }
  }

  void _applyPhase() {
    switch (widget.phase) {
      case DeckPhase.linking:
        _sweep.repeat();
        _pulse.stop();
      case DeckPhase.active:
        _sweep.stop();
        _pulse
          ..duration = const Duration(milliseconds: 3400)
          ..repeat(reverse: true);
      case DeckPhase.dropped:
        _sweep.stop();
        _pulse
          ..duration = const Duration(milliseconds: 900)
          ..repeat(reverse: true);
      case DeckPhase.idle:
        _sweep.stop();
        _pulse.stop();
    }
  }

  @override
  void dispose() {
    _sweep.dispose();
    _pulse.dispose();
    _snap.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    // Anyone who has asked their system not to animate gets a still
    // control that still carries every state in colour and label.
    final still = MediaQuery.disableAnimationsOf(context);
    final phase = widget.phase;
    final core = widget.size * 0.66;
    final enabled = widget.onPressed != null;

    return Semantics(
      container: true,
      button: true,
      enabled: enabled,
      label: '${widget.label}. ${phase.word}',
      onTap: widget.onPressed,
      child: ExcludeSemantics(
        child: SizedBox(
          width: widget.size,
          height: widget.size,
          child: Stack(
            alignment: Alignment.center,
            children: [
              // Static tracks. They never move, so the ring that does has
              // something to be measured against.
              _ring(widget.size, phase.color.withValues(alpha: 0.22)),
              _ring(widget.size * 0.83, phase.color.withValues(alpha: 0.14)),

              if (phase == DeckPhase.linking && !still)
                AnimatedBuilder(
                  animation: _sweep,
                  builder: (context, _) => Transform.rotate(
                    angle: _sweep.value * 2 * math.pi,
                    child: CustomPaint(
                      size: Size.square(widget.size),
                      painter: _SweepPainter(phase.color),
                    ),
                  ),
                ),

              // The bloom.
              AnimatedBuilder(
                animation: _pulse,
                builder: (context, _) {
                  final base = _bloomFor(phase);
                  final t = still ? 0.0 : _pulse.value;
                  final opacity = switch (phase) {
                    DeckPhase.active => base * (0.6 + 0.4 * t),
                    DeckPhase.dropped => base * (1 - 0.75 * t),
                    _ => base,
                  };
                  return Container(
                    width: core * 1.5,
                    height: core * 1.5,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      boxShadow: [
                        BoxShadow(
                          color: phase.color.withValues(alpha: opacity),
                          blurRadius: widget.size / 3,
                          spreadRadius: widget.size / 22,
                        ),
                      ],
                    ),
                  );
                },
              ),

              AnimatedBuilder(
                animation: _snap,
                builder: (_, child) {
                  // A short overshoot that settles: 1 → 1.06 → 1.
                  final t = still
                      ? 1.0
                      : Curves.elasticOut.transform(_snap.value);
                  final scale = _snap.isAnimating
                      ? 0.94 + 0.06 * t + 0.02 * (1 - t)
                      : 1.0;
                  return Transform.scale(
                    scale: _pressed ? 0.97 : scale,
                    child: child,
                  );
                },
                child: GestureDetector(
                  onTapDown: enabled
                      ? (_) => setState(() => _pressed = true)
                      : null,
                  onTapUp: enabled
                      ? (_) => setState(() => _pressed = false)
                      : null,
                  onTapCancel: enabled
                      ? () => setState(() => _pressed = false)
                      : null,
                  onTap: widget.onPressed,
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 260),
                    width: core,
                    height: core,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      gradient: RadialGradient(
                        center: const Alignment(0, -0.4),
                        colors: [phase.deep, Deck.deck800],
                        stops: const [0, 0.72],
                      ),
                      border: Border.all(
                        color: enabled
                            ? phase.color
                            : phase.color.withValues(alpha: 0.4),
                      ),
                    ),
                    alignment: Alignment.center,
                    child: Text(
                      widget.label.toUpperCase(),
                      style: Deck.mono(
                        size: widget.size / 8,
                        color: enabled ? phase.color : Deck.inkFaint,
                        weight: FontWeight.w600,
                        spacing: 2,
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  static double _bloomFor(DeckPhase p) => switch (p) {
    DeckPhase.idle => 0.08,
    DeckPhase.linking => 0.3,
    DeckPhase.active => 0.4,
    DeckPhase.dropped => 0.44,
  };

  Widget _ring(double size, Color color) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      shape: BoxShape.circle,
      border: Border.all(color: color),
    ),
  );
}

/// The scanning arc: a gradient sweep clipped to a hairline ring.
class _SweepPainter extends CustomPainter {
  const _SweepPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5
      ..strokeCap = StrokeCap.round
      ..shader = SweepGradient(
        colors: [color.withValues(alpha: 0), color],
        stops: const [0.55, 1],
      ).createShader(rect);

    // A little over a quarter turn: long enough to read as a sweep, short
    // enough that the gap makes the rotation legible.
    canvas.drawArc(
      rect.deflate(0.75),
      -math.pi / 2,
      math.pi * 0.62,
      false,
      paint,
    );
  }

  @override
  bool shouldRepaint(_SweepPainter old) => old.color != color;
}
