import 'package:flutter/material.dart';

/// One set of colours for a theme (1.0.8: dark and light). Every colour of
/// the UI comes from the active set through [Palette]; nothing is hardcoded
/// per theme in the widgets.
@immutable
class PaletteData {
  const PaletteData({
    required this.brightness,
    required this.bg,
    required this.bg2,
    required this.bg3,
    required this.text,
    required this.text2,
    required this.muted,
    required this.lime,
    required this.limeFill,
    required this.limeFill2,
    required this.limeInk,
    required this.violet,
    required this.violetFg,
    required this.danger,
    required this.warn,
    required this.info,
    required this.indigoDeep,
    required this.limeDeep,
    required this.glass,
    required this.glassHi,
    required this.line,
    required this.line2,
    required this.card,
    required this.card2,
    required this.shadow,
    required this.barrier,
  });

  final Brightness brightness;
  final Color bg, bg2, bg3, text, text2, muted;

  /// Lime as text / icon / stroke colour. Dark: the brand lime. Light: a
  /// deep olive lime (lime #C6FF3D on white is unreadable, 1.2:1).
  final Color lime;

  /// Lime as a filled surface with [limeInk] on top (primary buttons, the
  /// switch track): the brand lime in both themes.
  final Color limeFill, limeFill2;
  final Color limeInk;
  final Color violet, violetFg, danger, warn, info;
  final Color indigoDeep, limeDeep;
  final Color glass, glassHi, line, line2;

  /// Card gradient (top-left → bottom-right) of [Glass].
  final Color card, card2;

  /// Drop shadow under raised surfaces (sheets, menus).
  final Color shadow;

  /// Modal barrier.
  final Color barrier;

  bool get isLight => brightness == Brightness.light;
}

/// The brand palette (dark, shared with the panel) plus the client's own
/// depth tones. Values identical to 1.0.7.
const darkPalette = PaletteData(
  brightness: Brightness.dark,
  bg: Color(0xFF07080C),
  bg2: Color(0xFF0D0F16),
  bg3: Color(0xFF141722),
  text: Color(0xFFEEF0F6),
  text2: Color(0xFFB7BCCB),
  muted: Color(0xFF8F95A6),
  lime: Color(0xFFC6FF3D),
  limeFill: Color(0xFFC6FF3D),
  limeFill2: Color(0xFFD8FF6E),
  limeInk: Color(0xFF0B1000),
  violet: Color(0xFF8B7CFF),
  violetFg: Color(0xFFA99DFF),
  danger: Color(0xFFFF5C7A),
  warn: Color(0xFFFFB547),
  info: Color(0xFF5CC8FF),
  indigoDeep: Color(0xFF140F33),
  limeDeep: Color(0xFF2E4A00),
  glass: Color(0x0FFFFFFF), // 6%
  glassHi: Color(0x1AFFFFFF), // 10%
  line: Color(0x17FFFFFF), // 9%
  line2: Color(0x29FFFFFF), // 16%
  // = alphaBlend(white 8.5% / 3%, 0x2E0B0D14), the 1.0.7 glass card.
  card: Color(0x4A3E4046),
  card2: Color(0x38202128),
  shadow: Color(0x99000000),
  barrier: Color(0x8C000000),
);

/// Light theme (1.0.8): same brand, adapted for contrast on light
/// backgrounds. Text colours ≥ 4.5:1 on [bg] (lime 5.2, violetFg 5.6,
/// danger 5.1, warn 4.6, info 4.7, muted 4.5).
const lightPalette = PaletteData(
  brightness: Brightness.light,
  bg: Color(0xFFF4F5FA),
  bg2: Color(0xFFFFFFFF),
  bg3: Color(0xFFE9EBF2),
  text: Color(0xFF10121A),
  text2: Color(0xFF3A3F4E),
  muted: Color(0xFF6A7083),
  lime: Color(0xFF4A7300),
  limeFill: Color(0xFFC6FF3D),
  limeFill2: Color(0xFFD8FF6E),
  limeInk: Color(0xFF0B1000),
  violet: Color(0xFF6A5AE0),
  violetFg: Color(0xFF5B4BD6),
  danger: Color(0xFFC8273F),
  warn: Color(0xFFA35F00),
  info: Color(0xFF0A75AA),
  indigoDeep: Color(0xFFD9D4FF),
  limeDeep: Color(0xFFE3F5B8),
  glass: Color(0x0A10121A), // 4% ink
  glassHi: Color(0x1410121A), // 8%
  line: Color(0x1A10121A), // 10%
  line2: Color(0x2E10121A), // 18%
  card: Color(0xF2FFFFFF),
  card2: Color(0xD9FFFFFF),
  shadow: Color(0x2410121A),
  barrier: Color(0x5910121A),
);

/// The active colours. Read at build / paint time; [ThemeHost] switches the
/// set and rebuilds + repaints the whole tree in the same frame, so no frame
/// ever mixes the two themes.
abstract final class Palette {
  static PaletteData _p = darkPalette;

  static PaletteData get current => _p;
  static bool get isLight => _p.isLight;

  /// Only [ThemeHost] (and tests) switch the palette.
  static void use(PaletteData p) => _p = p;

  static Color get bg => _p.bg;
  static Color get bg2 => _p.bg2;
  static Color get bg3 => _p.bg3;
  static Color get text => _p.text;
  static Color get text2 => _p.text2;
  static Color get muted => _p.muted;
  static Color get lime => _p.lime;
  static Color get limeFill => _p.limeFill;
  static Color get limeFill2 => _p.limeFill2;
  static Color get limeInk => _p.limeInk;
  static Color get violet => _p.violet;
  static Color get violetFg => _p.violetFg;
  static Color get danger => _p.danger;
  static Color get warn => _p.warn;
  static Color get info => _p.info;
  static Color get indigoDeep => _p.indigoDeep;
  static Color get limeDeep => _p.limeDeep;
  static Color get glass => _p.glass;
  static Color get glassHi => _p.glassHi;
  static Color get line => _p.line;
  static Color get line2 => _p.line2;
  static Color get card => _p.card;
  static Color get card2 => _p.card2;
  static Color get shadow => _p.shadow;
  static Color get barrier => _p.barrier;

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

/// The app themes, built once per palette (1.0.7: a new ThemeData per
/// rebuild made AnimatedTheme animate and rebuild every Theme dependant).
final ThemeData appThemeDark = buildTheme(darkPalette);
final ThemeData appThemeLight = buildTheme(lightPalette);

/// Kept for callers of 1.0.7 (the dark theme).
ThemeData get appTheme => appThemeDark;

ThemeData themeFor(PaletteData p) => p.isLight ? appThemeLight : appThemeDark;

ThemeData buildTheme([PaletteData? palette]) {
  final p = palette ?? Palette.current;
  final scheme = (p.isLight ? ColorScheme.light : ColorScheme.dark)(
    surface: p.bg,
    primary: p.lime,
    onPrimary: p.isLight ? Colors.white : p.limeInk,
    secondary: p.violet,
    onSecondary: Colors.white,
    error: p.danger,
    onSurface: p.text,
    outline: p.line2,
  );
  final base = ThemeData(
    useMaterial3: true,
    brightness: p.brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: p.bg,
    canvasColor: p.bg,
    fontFamily: Fonts.fa,
    splashFactory: InkSparkle.splashFactory,
    pageTransitionsTheme: const PageTransitionsTheme(builders: {
      TargetPlatform.android: _FadeThroughBuilder(),
    }),
  );
  return base.copyWith(
    textTheme: base.textTheme.apply(bodyColor: p.text, displayColor: p.text, fontFamily: Fonts.fa),
    iconTheme: IconThemeData(color: p.text2),
    dividerColor: p.line,
    listTileTheme: ListTileThemeData(iconColor: p.text2, textColor: p.text),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: p.isLight ? p.text : p.bg3,
      contentTextStyle: TextStyle(fontFamily: Fonts.fa, color: p.isLight ? p.bg : p.text),
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(Radii.m))),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? p.limeInk : (p.isLight ? Colors.white : p.text2)),
      trackColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? (p.isLight ? p.lime : p.limeFill) : (p.isLight ? p.line2 : p.glassHi)),
      trackOutlineColor: WidgetStateProperty.all(Colors.transparent),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: p.glass,
      hintStyle: TextStyle(color: p.muted),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: BorderSide(color: p.line)),
      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: BorderSide(color: p.line)),
      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.m), borderSide: BorderSide(color: p.lime, width: 1.4)),
      contentPadding: const EdgeInsets.symmetric(horizontal: Gap.l, vertical: Gap.m),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: p.lime),
    bottomSheetTheme: const BottomSheetThemeData(backgroundColor: Colors.transparent, elevation: 0),
    dialogTheme: DialogThemeData(
      backgroundColor: p.bg2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.l), side: BorderSide(color: p.line)),
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
