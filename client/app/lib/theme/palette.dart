import 'package:flutter/material.dart';

/// Veilon brand palette (shared with the panel) plus the client's own
/// depth tones. The client keeps the brand colours but uses its own visual
/// language: aurora backgrounds, glass and a portal-shaped connect hero.
abstract final class Palette {
  // Brand (from the panel's design tokens).
  static const bg = Color(0xFF07080C);
  static const bg2 = Color(0xFF0D0F16);
  static const bg3 = Color(0xFF141722);
  static const text = Color(0xFFEEF0F6);
  static const text2 = Color(0xFFB7BCCB);
  static const muted = Color(0xFF8F95A6);
  static const lime = Color(0xFFC6FF3D);
  static const limeInk = Color(0xFF0B1000);
  static const violet = Color(0xFF8B7CFF);
  static const violetFg = Color(0xFFA99DFF);
  static const danger = Color(0xFFFF5C7A);
  static const warn = Color(0xFFFFB547);
  static const info = Color(0xFF5CC8FF);

  // Client depth tones.
  static const indigoDeep = Color(0xFF140F33);
  static const limeDeep = Color(0xFF2E4A00);
  static const glass = Color(0x0FFFFFFF); // 6%
  static const glassHi = Color(0x1AFFFFFF); // 10%
  static const line = Color(0x17FFFFFF); // 9%
  static const line2 = Color(0x29FFFFFF); // 16%

  static Color pingColor(int? ms) {
    if (ms == null) return muted;
    if (ms < 0) return danger;
    if (ms < 300) return lime;
    if (ms < 800) return warn;
    return danger;
  }
}

abstract final class Fonts {
  static const fa = 'Vazirmatn';
  static const display = 'Sora';

  /// Sora has no Persian digits/letters: fall back to Vazirmatn.
  static const fallback = [fa];

  /// Equal-width digits for live numbers (no width jitter as values change).
  static const tabular = [FontFeature.tabularFigures()];

  /// Style for measurements (speeds, sizes, delays): Latin tabular digits.
  static TextStyle metric({double size = 14, FontWeight weight = FontWeight.w700, Color? color, double? height}) => TextStyle(
      fontFamily: display,
      fontFamilyFallback: fallback,
      fontFeatures: tabular,
      fontSize: size,
      fontWeight: weight,
      color: color,
      height: height);
}

/// Spacing scale (4pt grid).
abstract final class Gap {
  static const xs = 4.0;
  static const s = 8.0;
  static const m = 12.0;
  static const l = 16.0;
  static const xl = 24.0;
  static const xxl = 32.0;
  static const page = 20.0;
}

abstract final class Radii {
  static const s = 12.0;
  static const m = 18.0;
  static const l = 26.0;
  static const xl = 34.0;
}

/// The app theme, built once (1.0.7): a new ThemeData per rebuild made
/// AnimatedTheme animate and rebuild every Theme dependant.
final ThemeData appTheme = buildTheme();

ThemeData buildTheme() {
  const scheme = ColorScheme.dark(
    surface: Palette.bg,
    primary: Palette.lime,
    onPrimary: Palette.limeInk,
    secondary: Palette.violet,
    onSecondary: Colors.white,
    error: Palette.danger,
    onSurface: Palette.text,
    outline: Palette.line2,
  );
  final base = ThemeData(
    useMaterial3: true,
    brightness: Brightness.dark,
    colorScheme: scheme,
    scaffoldBackgroundColor: Palette.bg,
    fontFamily: Fonts.fa,
    splashFactory: InkSparkle.splashFactory,
    pageTransitionsTheme: const PageTransitionsTheme(builders: {
      TargetPlatform.android: _FadeThroughBuilder(),
    }),
  );
  return base.copyWith(
    textTheme: base.textTheme.apply(bodyColor: Palette.text, displayColor: Palette.text, fontFamily: Fonts.fa),
    dividerColor: Palette.line,
    snackBarTheme: const SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: Palette.bg3,
      contentTextStyle: TextStyle(fontFamily: Fonts.fa, color: Palette.text),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(Radii.m))),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? Palette.limeInk : Palette.text2),
      trackColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? Palette.lime : Palette.glassHi),
      trackOutlineColor: WidgetStateProperty.all(Colors.transparent),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Palette.glass,
      hintStyle: const TextStyle(color: Palette.muted),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: const BorderSide(color: Palette.line)),
      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: const BorderSide(color: Palette.line)),
      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: const BorderSide(color: Palette.lime, width: 1.4)),
      contentPadding: const EdgeInsets.symmetric(horizontal: Gap.l, vertical: Gap.m),
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: Palette.lime),
    bottomSheetTheme: const BottomSheetThemeData(backgroundColor: Colors.transparent, elevation: 0),
    dialogTheme: DialogThemeData(
      backgroundColor: Palette.bg2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.l), side: const BorderSide(color: Palette.line)),
    ),
  );
}

/// App-wide scrolling (1.0.3): clamping physics and no overscroll effect.
/// Content that fits never moves (no bounce, no Android stretch on a drag),
/// longer content scrolls normally and simply stops at its ends.
class VeilonScrollBehavior extends MaterialScrollBehavior {
  const VeilonScrollBehavior();
  @override
  ScrollPhysics getScrollPhysics(BuildContext context) => const ClampingScrollPhysics();
  @override
  Widget buildOverscrollIndicator(BuildContext context, Widget child, ScrollableDetails details) => child;
}

/// Page scroll physics: never scrolls when the content fits.
const pagePhysics = ClampingScrollPhysics();

/// Page transition (1.0.3): the new page fades in over the old one, which
/// stays fully opaque underneath until it is covered. 1.0.2 also faded the
/// old page out, so mid-transition both were half transparent over the
/// black window and the whole screen dimmed, then snapped to full
/// brightness (the "dark haze" on Split tunneling). Pure fade, no scale:
/// nothing moves.
class _FadeThroughBuilder extends PageTransitionsBuilder {
  const _FadeThroughBuilder();
  @override
  Widget buildTransitions<T>(PageRoute<T> route, BuildContext context, Animation<double> animation,
      Animation<double> secondaryAnimation, Widget child) {
    final curved = CurvedAnimation(parent: animation, curve: Curves.easeOutCubic, reverseCurve: Curves.easeInCubic);
    return FadeTransition(opacity: curved, child: child);
  }
}
