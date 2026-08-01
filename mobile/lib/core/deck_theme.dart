import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// The command deck, in Flutter.
///
/// These are the same values as `desktop/frontend/src/theme.css`, restated
/// because the two platforms cannot share a stylesheet. Keeping them in one
/// file per platform is what makes "is this the same product" answerable by
/// reading two short files rather than grepping for hex codes.
///
/// The palette rests on one idea: the accent colour *is* the state. Cyan
/// means a tunnel is carrying traffic, amber means it is working on it,
/// crimson means it stopped without being asked. Nothing decorative is
/// allowed those hues, so a glance anywhere on screen answers "is it up".
class Deck {
  const Deck._();

  // Ground. Not black — a near-black with a blue bias, so cyan sits on a
  // surface that shares its temperature instead of vibrating against #000.
  static const deck900 = Color(0xFF05080F);
  static const deck800 = Color(0xFF0A0F1A);
  static const deck700 = Color(0xFF0F1626);
  static const deck600 = Color(0xFF16203A);
  static const line = Color(0xFF1D2A45);
  static const lineBright = Color(0xFF2B3D63);

  // Text, in three steps. A fourth always ends up unused.
  static const ink = Color(0xFFE8EDF7);
  static const inkDim = Color(0xFF8FA0BD);
  static const inkFaint = Color(0xFF55637D);

  // State. Load-bearing, and used nowhere decorative.
  static const live = Color(0xFF22E5C8);
  static const liveDeep = Color(0xFF0B8F7D);
  static const work = Color(0xFFFFA733);
  static const workDeep = Color(0xFFB3660D);
  static const fail = Color(0xFFFF3D5E);
  static const failDeep = Color(0xFFA3092A);
  static const rest = Color(0xFF4A6EA8);

  /// Every number and address is set in a monospace face, because digits
  /// that shift width as they tick are the fastest way to make live data
  /// look broken. No font is bundled: the platform's own monospace is
  /// tabular on both Android and iOS, and a missing webfont would drop the
  /// numbers into a proportional fallback, which this layout cannot
  /// survive.
  static const monoFamily = 'monospace';

  static TextStyle mono({
    double size = 12,
    Color color = ink,
    FontWeight weight = FontWeight.w400,
    double spacing = 0,
  }) {
    return TextStyle(
      fontFamily: monoFamily,
      fontFamilyFallback: const ['RobotoMono', 'Menlo', 'Courier'],
      fontSize: size,
      color: color,
      fontWeight: weight,
      letterSpacing: spacing,
      fontFeatures: const [FontFeature.tabularFigures()],
    );
  }

  /// A section label: uppercase, letterspaced, small. These are labels on
  /// instrumentation, not headings in a document.
  static TextStyle eyebrow({Color color = inkFaint}) => mono(
        size: 10,
        color: color,
        weight: FontWeight.w500,
        spacing: 1.6,
      );

  /// The glow that makes a surface read as emitting light rather than
  /// casting a shadow: a tight ring plus a wide bloom.
  static List<BoxShadow> glow(Color color, {double strength = 1}) => [
        BoxShadow(
          color: color.withValues(alpha: 0.45 * strength),
          blurRadius: 22,
          spreadRadius: -4,
        ),
        BoxShadow(
          color: color.withValues(alpha: 0.28 * strength),
          blurRadius: 56,
          spreadRadius: -10,
        ),
      ];

  /// The app's theme. Dark only, deliberately: this design commits to a
  /// single visual world where the accent carries meaning, and a light
  /// ground would leave the glows — which are most of how state reads —
  /// with nothing to glow against.
  static ThemeData theme() {
    const scheme = ColorScheme.dark(
      primary: live,
      onPrimary: deck900,
      secondary: work,
      surface: deck800,
      onSurface: ink,
      error: fail,
      onError: deck900,
      outline: line,
      outlineVariant: lineBright,
      surfaceContainerHighest: deck700,
      onSurfaceVariant: inkDim,
    );

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: deck900,
      canvasColor: deck900,
      dividerColor: line,
      splashFactory: InkSparkle.splashFactory,
      textTheme: const TextTheme().apply(bodyColor: ink, displayColor: ink),
      appBarTheme: const AppBarTheme(
        backgroundColor: deck800,
        foregroundColor: ink,
        elevation: 0,
        centerTitle: false,
        surfaceTintColor: Colors.transparent,
      ),
      dialogTheme: const DialogThemeData(
        backgroundColor: deck800,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(4)),
          side: BorderSide(color: line),
        ),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: deck800,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(4)),
          side: BorderSide(color: line),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: deck900,
        hintStyle: mono(color: inkFaint),
        contentPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 12),
        border: _fieldBorder(line),
        enabledBorder: _fieldBorder(line),
        focusedBorder: _fieldBorder(live),
        errorBorder: _fieldBorder(fail),
        focusedErrorBorder: _fieldBorder(fail),
        labelStyle: eyebrow(),
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected) ? live : inkFaint,
        ),
        trackColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected)
              ? live.withValues(alpha: 0.22)
              : deck700,
        ),
        trackOutlineColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected) ? live : lineBright,
        ),
      ),
      listTileTheme: const ListTileThemeData(
        textColor: ink,
        iconColor: inkDim,
        dense: true,
      ),
    );
  }

  static OutlineInputBorder _fieldBorder(Color color) => OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(2)),
        borderSide: BorderSide(color: color),
      );

  /// The system bars painted to match, so the app does not sit in a bright
  /// frame of someone else's chrome.
  static const systemOverlay = SystemUiOverlayStyle(
    statusBarColor: Colors.transparent,
    statusBarIconBrightness: Brightness.light,
    statusBarBrightness: Brightness.dark,
    systemNavigationBarColor: deck800,
    systemNavigationBarIconBrightness: Brightness.light,
  );
}

/// The connection phase, in the four values every surface switches on.
/// Mirrors `tunnel.State` in the Go engine and `Phase` on the desktop.
enum DeckPhase {
  idle(Deck.rest, 'offline'),
  linking(Deck.work, 'linking'),
  active(Deck.live, 'online'),
  dropped(Deck.fail, 'tunnel lost');

  const DeckPhase(this.color, this.word);

  final Color color;

  /// The word shown to a user. Deliberately about the tunnel and not about
  /// their safety: on a mesh with no exit node, not being connected means
  /// you cannot reach your own machines and nothing more.
  final String word;

  Color get deep => switch (this) {
        DeckPhase.idle => Deck.deck600,
        DeckPhase.linking => Deck.workDeep,
        DeckPhase.active => Deck.liveDeep,
        DeckPhase.dropped => Deck.failDeep,
      };
}
