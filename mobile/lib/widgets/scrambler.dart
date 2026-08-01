import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';

import '../core/deck_theme.dart';

/// Text that churns and then locks, translated from the desktop.
///
/// The effect marks a moment that genuinely happens: before the tunnel is up
/// this device has no address on the network, and the instant it does, it
/// has one. The churn is the interval where the answer is unknown; the lock
/// is the answer arriving.
///
/// It settles left to right, the way an address is read, and never
/// scrambles the dots — so the shape stays legible throughout and the width
/// never shifts.
class Scrambler extends StatefulWidget {
  const Scrambler({
    super.key,
    required this.value,
    required this.active,
    this.style,
  });

  /// The final text. Empty means there is nothing to lock onto yet.
  final String value;

  /// Whether to run the churn at all.
  final bool active;

  final TextStyle? style;

  @override
  State<Scrambler> createState() => _ScramblerState();
}

class _ScramblerState extends State<Scrambler> {
  static const _digits = '0123456789';
  static const _frame = Duration(milliseconds: 45);

  final _random = Random();
  Timer? _timer;
  String _shown = '';
  int _tick = 0;
  bool _reduceMotion = false;

  @override
  void initState() {
    super.initState();
    _shown = widget.value;
  }

  // MediaQuery cannot be read during initState — an inherited widget looked
  // up before the element is attached throws, and the whole subtree fails to
  // build. didChangeDependencies is where that read belongs, and it also
  // runs again if the user changes the setting while this is on screen.
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _reduceMotion = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    _restart();
  }

  @override
  void didUpdateWidget(covariant Scrambler old) {
    super.didUpdateWidget(old);
    if (old.value != widget.value || old.active != widget.active) _restart();
  }

  void _restart() {
    _timer?.cancel();
    _tick = 0;

    if (!widget.active || widget.value.isEmpty || _reduceMotion) {
      _shown = widget.value;
      if (mounted) setState(() {});
      return;
    }

    // Two frames per character: fast enough to read as churn, slow enough
    // that digits are individually visible rather than a grey blur, which
    // is what makes it look like a readout and not a rendering fault.
    final total = widget.value.length * 2;
    _timer = Timer.periodic(_frame, (t) {
      _tick++;
      if (_tick >= total) {
        t.cancel();
        setState(() => _shown = widget.value);
        return;
      }
      final settled = _tick ~/ 2;
      setState(() {
        _shown = String.fromCharCodes(
          widget.value.runes.toList().asMap().entries.map((e) {
            final ch = String.fromCharCode(e.value);
            if (e.key < settled || ch == '.' || ch == ':' || ch == '/') {
              return e.value;
            }
            return _digits.codeUnitAt(_random.nextInt(_digits.length));
          }),
        );
      });
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: widget.value.isEmpty ? null : widget.value,
      child: Text(
        _shown.isEmpty ? '—' : _shown,
        style: widget.style ?? Deck.mono(size: 22, color: Deck.live),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}
