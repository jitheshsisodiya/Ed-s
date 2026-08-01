import 'package:flutter/material.dart';

/// The one control on the home screen.
///
/// It is deliberately large and round: it is the only thing most people ever
/// tap, it has to be reachable one-handed, and its colour says the state
/// before the label is read — blue for off, green for on, amber while it
/// works.
class ConnectButton extends StatelessWidget {
  const ConnectButton({
    super.key,
    required this.connected,
    required this.busy,
    required this.onPressed,
    this.size = 168,
  });

  final bool connected;
  final bool busy;
  final VoidCallback? onPressed;
  final double size;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final enabled = onPressed != null;

    final Color base;
    if (!enabled && !busy) {
      base = scheme.surfaceContainerHighest;
    } else if (busy) {
      base = const Color(0xFFD97706);
    } else if (connected) {
      base = const Color(0xFF059669);
    } else {
      base = scheme.primary;
    }

    final foreground = (!enabled && !busy) ? scheme.outline : Colors.white;

    return Semantics(
      button: true,
      label: connected ? 'Disconnect' : 'Connect',
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: RadialGradient(
            center: const Alignment(0, -0.35),
            colors: [
              Color.alphaBlend(Colors.white.withValues(alpha: 0.22), base),
              base,
            ],
          ),
          boxShadow: [
            // The halo is the state's own colour, so the button reads from
            // across the room without anyone parsing the word on it.
            BoxShadow(
              color: base.withValues(alpha: 0.28),
              blurRadius: 0,
              spreadRadius: 10,
            ),
            BoxShadow(
              color: base.withValues(alpha: 0.35),
              blurRadius: 30,
              offset: const Offset(0, 14),
            ),
          ],
        ),
        child: Material(
          color: Colors.transparent,
          shape: const CircleBorder(),
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            onTap: onPressed,
            child: Center(
              child: busy
                  ? SizedBox(
                      width: 30,
                      height: 30,
                      child: CircularProgressIndicator(
                        strokeWidth: 2.6,
                        valueColor: AlwaysStoppedAnimation(foreground),
                      ),
                    )
                  : Text(
                      connected ? 'Disconnect' : 'Connect',
                      style: TextStyle(
                        color: foreground,
                        fontSize: 17,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
            ),
          ),
        ),
      ),
    );
  }
}
