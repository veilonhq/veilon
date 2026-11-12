import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';

import 'palette.dart';

/// The palette for a theme setting ('dark' default, 'light', 'system').
PaletteData paletteFor(String mode, Brightness platform) => switch (mode) {
      'light' => lightPalette,
      'system' => platform == Brightness.light ? lightPalette : darkPalette,
      _ => darkPalette,
    };

/// Status / navigation bar icons that read on the palette's background.
SystemUiOverlayStyle systemBarsFor(PaletteData p) => SystemUiOverlayStyle(
      statusBarColor: Colors.transparent,
      systemNavigationBarColor: Colors.transparent,
      systemNavigationBarDividerColor: Colors.transparent,
      statusBarIconBrightness: p.isLight ? Brightness.dark : Brightness.light,
      statusBarBrightness: p.isLight ? Brightness.light : Brightness.dark,
      systemNavigationBarIconBrightness: p.isLight ? Brightness.dark : Brightness.light,
      systemNavigationBarContrastEnforced: false,
    );

/// Applies the theme setting to the whole app (1.0.8).
///
/// Switching (setting changed, or the device theme changed while on
/// "System") is one cross-fade with no intermediate states:
///  1. the frame on screen right now is captured into an image (one GPU
///     copy of the current layer tree, nothing is re-laid out);
///  2. the palette is switched and EVERY element is rebuilt and every render
///     object repainted in the same frame, so the next frame is entirely in
///     the new theme (no half-themed frame, no text that keeps the old
///     colour because its widget was const or cached);
///  3. the captured image lies on top at full opacity and fades out over
///     [fadeDuration]. The fade is a single textured quad drawn with an
///     alpha (no saveLayer of the app), so it costs the same on any phone.
/// Nothing under the image is animated by the switch itself: the aurora
/// keeps its phase, pages keep their scroll offsets and state.
///
/// When the app is not visible (or animations are disabled), the switch is
/// instant. A switch during a running fade captures the current blended
/// frame, so it continues from exactly what is on screen.
class ThemeHost extends StatefulWidget {
  const ThemeHost({super.key, required this.mode, required this.builder, this.onPalette});

  /// 'dark', 'light' or 'system'.
  final String mode;

  /// Builds the app for the active palette.
  final Widget Function(BuildContext context, PaletteData palette) builder;

  /// Called after the palette changed (native window colour, splash theme).
  final ValueChanged<PaletteData>? onPalette;

  static const fadeDuration = Duration(milliseconds: 320);

  @override
  State<ThemeHost> createState() => ThemeHostState();
}

class ThemeHostState extends State<ThemeHost> with WidgetsBindingObserver, SingleTickerProviderStateMixin {
  final _shotKey = GlobalKey(debugLabel: 'theme-host-screen');
  late final AnimationController _fade = AnimationController(vsync: this, duration: ThemeHost.fadeDuration)
    ..addStatusListener(_onFadeStatus);
  late final Animation<double> _shotOpacity =
      ReverseAnimation(CurvedAnimation(parent: _fade, curve: Curves.easeInOutCubic));
  ui.Image? _shot;

  /// Number of theme switches applied (tests).
  int switches = 0;

  /// Whether the last switch cross-faded (tests).
  bool lastSwitchAnimated = false;

  PaletteData get _target =>
      paletteFor(widget.mode, WidgetsBinding.instance.platformDispatcher.platformBrightness);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    // main() already chose the palette before the first frame; make sure
    // it matches the setting (tests build the host directly).
    final p = _target;
    if (!identical(p, Palette.current)) Palette.use(p);
    SystemChrome.setSystemUIOverlayStyle(systemBarsFor(p));
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _fade.dispose();
    _shot?.dispose();
    _shot = null;
    super.dispose();
  }

  @override
  void didUpdateWidget(ThemeHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.mode != widget.mode) _switchTo(_target, inBuild: true);
  }

  @override
  void didChangePlatformBrightness() {
    if (widget.mode == 'system') _switchTo(_target);
  }

  void _onFadeStatus(AnimationStatus s) {
    if (s == AnimationStatus.completed && _shot != null && mounted) {
      setState(() {
        _shot?.dispose();
        _shot = null;
      });
    }
  }

  bool get _canAnimate {
    final lifecycle = SchedulerBinding.instance.lifecycleState;
    if (lifecycle != null && lifecycle != AppLifecycleState.resumed) return false;
    final view = View.maybeOf(context);
    if (view != null && MediaQueryData.fromView(view).disableAnimations) return false;
    return true;
  }

  /// The frame on screen (including a running fade), or null.
  ui.Image? _capture() {
    try {
      final ro = _shotKey.currentContext?.findRenderObject();
      if (ro is! RenderRepaintBoundary || !ro.attached || !ro.hasSize || ro.size.isEmpty) return null;
      final dpr = View.maybeOf(context)?.devicePixelRatio ?? 1.0;
      return ro.toImageSync(pixelRatio: dpr);
    } catch (_) {
      return null;
    }
  }

  void _switchTo(PaletteData p, {bool inBuild = false}) {
    if (identical(p, Palette.current)) return;
    final shot = _canAnimate ? _capture() : null;
    Palette.use(p);
    switches++;
    lastSwitchAnimated = shot != null;
    SystemChrome.setSystemUIOverlayStyle(systemBarsFor(p));
    widget.onPalette?.call(p);
    _rebuildEverything();
    final old = _shot;
    _shot = shot;
    if (shot != null) {
      _fade.forward(from: 0);
    } else {
      _fade.value = 1;
    }
    // The previous capture (if a fade was running) is replaced only after
    // this frame, which no longer shows it.
    if (old != null) SchedulerBinding.instance.addPostFrameCallback((_) => old.dispose());
    if (mounted && !inBuild) setState(() {});
  }

  /// Rebuilds every element and repaints every render object below the host:
  /// widgets read [Palette] at build / paint time, and a const or cached
  /// widget would otherwise keep the old colours until something else
  /// rebuilt it.
  void _rebuildEverything() {
    void visit(Element e) {
      e.markNeedsBuild();
      if (e is RenderObjectElement) e.renderObject.markNeedsPaint();
      e.visitChildren(visit);
    }

    (context as Element).visitChildren(visit);
  }

  @override
  Widget build(BuildContext context) {
    final shot = _shot;
    // MaterialApp sets a default bar style (black navigation bar icons
    // stay light) on every build; this region is applied after each frame
    // and wins, so both bars always match the active palette.
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: systemBarsFor(Palette.current),
      child: RepaintBoundary(
      key: _shotKey,
      child: Directionality(
        textDirection: TextDirection.ltr,
        child: Stack(fit: StackFit.expand, alignment: Alignment.topLeft, children: [
          KeyedSubtree(key: const ValueKey('theme-host-app'), child: widget.builder(context, Palette.current)),
          if (shot != null)
            Positioned.fill(
              key: const ValueKey('theme-host-shot'),
              child: IgnorePointer(
                child: RepaintBoundary(
                  child: RawImage(image: shot, fit: BoxFit.fill, opacity: _shotOpacity, filterQuality: FilterQuality.medium),
                ),
              ),
            ),
        ]),
      ),
    ),
    );
  }
}
