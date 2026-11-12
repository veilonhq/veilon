import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';

import '../../core/models.dart';
import '../../theme/palette.dart';

/// The connect hero: a liquid "portal" of interfering wave-rings around a
/// core. Idle: slow violet breathing. Connecting: rings churn and a comet
/// sweeps the rim. Protected: the core floods lime, rings calm into a
/// steady orbit with drifting sparks. Error: a short red shiver.
class Portal extends StatefulWidget {
  const Portal({super.key, required this.state, required this.onTap, this.size = 270, this.enabled = true});
  final ConnState state;
  final VoidCallback onTap;
  final double size;
  final bool enabled;

  @override
  State<Portal> createState() => _PortalState();
}

class _PortalState extends State<Portal> with TickerProviderStateMixin {
  late final Ticker _ticker;
  final _clock = ValueNotifier<double>(0);
  late final AnimationController _fill; // 0 idle → 1 connected
  late final AnimationController _press;
  late final AnimationController _shock; // ripple on state change
  // Continuous blends instead of per-state switches (1.0.3): the comet
  // fades in/out with connecting, the red error cast fades too, and the
  // shock ring never restarts from its centre while one is running.
  late final AnimationController _comet; // 1 while connecting / disconnecting
  late final AnimationController _error; // 1 in the error state
  double _turbulence = 0.25;
  DateTime _lastTap = DateTime.fromMillisecondsSinceEpoch(0);
  double _t = 0;
  Duration _last = Duration.zero;

  @override
  void initState() {
    super.initState();
    // 1.0.6: slightly longer cross-fades (were 350 / 280 / 300 ms) so
    // connecting → connected / disconnected reads as one smooth blend.
    _fill = AnimationController(vsync: this, duration: const Duration(milliseconds: 450), value: _target(widget.state));
    _press = AnimationController(vsync: this, duration: const Duration(milliseconds: 160));
    _shock = AnimationController(vsync: this, duration: const Duration(milliseconds: 350));
    _comet = AnimationController(vsync: this, duration: const Duration(milliseconds: 420), value: _cometTarget(widget.state));
    _error = AnimationController(vsync: this, duration: const Duration(milliseconds: 360), value: _errorTarget(widget.state));
    _ticker = createTicker((e) {
      // Clamp: after a pause (hidden page, TickerMode) elapsed jumps.
      final dt = min(0.05, (e - _last).inMicroseconds / 1e6);
      _last = e;
      final targetTurb = switch (widget.state) {
        ConnState.connecting || ConnState.disconnecting => 1.0,
        ConnState.connected => 0.14,
        ConnState.error => 0.7,
        _ => 0.3,
      };
      _turbulence += (targetTurb - _turbulence) * min(1, dt * 2.2);
      final speed = 0.35 + _turbulence * 1.6;
      _t += dt * speed;
      _clock.value = _t;
    })
      ..start();
  }

  double _target(ConnState s) => s == ConnState.connected ? 1 : 0;
  double _cometTarget(ConnState s) => s == ConnState.connecting || s == ConnState.disconnecting ? 1 : 0;
  double _errorTarget(ConnState s) => s == ConnState.error ? 1 : 0;

  /// Starts a shock ring unless one is still expanding (a restart would
  /// make the visible ring jump back to the centre).
  void _shockOnce() {
    if (!_shock.isAnimating) _shock.forward(from: 0);
  }

  @override
  void didUpdateWidget(Portal old) {
    super.didUpdateWidget(old);
    if (old.state != widget.state) {
      // animateTo continues from the current value (no restart), with a
      // symmetric curve so a reversal mid-way has no velocity spike.
      _fill.animateTo(_target(widget.state), curve: Curves.easeInOutCubic);
      _comet.animateTo(_cometTarget(widget.state), curve: Curves.easeInOut);
      _error.animateTo(_errorTarget(widget.state), curve: Curves.easeInOut);
      if (widget.state == ConnState.connected) {
        HapticFeedback.heavyImpact();
        _shockOnce();
      } else if (widget.state == ConnState.error) {
        HapticFeedback.vibrate();
        _shockOnce();
      }
    }
  }

  @override
  void dispose() {
    _ticker.dispose();
    _clock.dispose();
    _fill.dispose();
    _press.dispose();
    _shock.dispose();
    _comet.dispose();
    _error.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      label: 'connect',
      child: GestureDetector(
        onTapDown: widget.enabled ? (_) => _press.forward() : null,
        onTapCancel: () => _press.reverse(),
        onTapUp: widget.enabled ? (_) => _press.reverse() : null,
        onTap: widget.enabled
            ? () {
                // Debounce: a double tap must not fire connect + disconnect.
                final now = DateTime.now();
                if (now.difference(_lastTap) < const Duration(milliseconds: 400)) return;
                _lastTap = now;
                HapticFeedback.mediumImpact();
                widget.onTap();
              }
            : null,
        child: AnimatedBuilder(
          animation: Listenable.merge([_press, _fill, _shock, _comet, _error]),
          builder: (context, _) => Transform.scale(
            scale: 1 - _press.value * 0.05,
            child: RepaintBoundary(
              child: CustomPaint(
                size: Size.square(widget.size),
                painter: PortalPainter(
                  clock: _clock,
                  fill: _fill.value,
                  turbulence: () => _turbulence,
                  comet: _comet.value,
                  error: _error.value,
                  shock: _shock.value,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Paints one portal frame. Every visual input is a continuous value
/// (fill, comet, error, shock in 0..1), so no state change can make a
/// frame jump; public for the frame-continuity tests.
class PortalPainter extends CustomPainter {
  PortalPainter(
      {required this.clock, required this.fill, required this.turbulence, required this.comet, required this.error, required this.shock})
      : super(repaint: clock);

  final ValueNotifier<double> clock;
  final double fill;
  final double Function() turbulence;
  final double comet;
  final double error;
  final double shock;

  static const _rings = 4;

  Color get _accent => Color.lerp(Color.lerp(Palette.violet, Palette.lime, fill)!, Palette.danger, error)!;

  /// A wave ring around the origin, drawn in a frame rotated by [rot]: the
  /// point at local angle a has the radius of world angle a + rot, so the
  /// ring's shape stays fixed on screen while only the colour sweep turns.
  Path _wave(double r, double amp, int k1, int k2, double ph1, double ph2, double rot) {
    final p = Path();
    const steps = 160;
    for (var i = 0; i <= steps; i++) {
      final a = i / steps * 2 * pi;
      final w = a + rot;
      final rr = r + amp * (sin(k1 * w + ph1) * 0.6 + sin(k2 * w - ph2) * 0.4);
      final pt = Offset(rr * cos(a), rr * sin(a));
      i == 0 ? p.moveTo(pt.dx, pt.dy) : p.lineTo(pt.dx, pt.dy);
    }
    return p..close();
  }

  @override
  void paint(Canvas canvas, Size size) {
    final t = clock.value;
    final turb = turbulence();
    final c = size.center(Offset.zero);
    final R = size.width / 2;
    final accent = _accent;

    // Outer halo.
    final haloR = R * (1.0 + 0.04 * sin(t * 1.3));
    canvas.drawCircle(
      c,
      haloR,
      Paint()
        ..shader = ui.Gradient.radial(c, haloR, [
          accent.withValues(alpha: 0.10 + 0.22 * fill),
          accent.withValues(alpha: 0.03),
          Colors.transparent,
        ], [0.35, 0.7, 1]),
    );

    // Shockwave on connect / error.
    if (shock > 0 && shock < 1) {
      final sr = R * (0.45 + 0.6 * Curves.easeOutCubic.transform(shock));
      canvas.drawCircle(
          c,
          sr,
          Paint()
            ..style = PaintingStyle.stroke
            ..strokeWidth = 2.5 * (1 - shock)
            ..color = accent.withValues(alpha: 0.7 * (1 - shock)));
    }

    // Wave rings.
    for (var i = 0; i < _rings; i++) {
      final f = i / (_rings - 1);
      final r = R * (0.56 + 0.10 * f);
      final amp = R * (0.012 + 0.05 * turb) * (1 - f * 0.4);
      // 1.0.6 smoothness fix: the colour sweep used to turn by moving
      // the gradient's start angle (t·speed mod 2π, TileMode.clamp).
      // Angles below the start were clamped to the transparent first stop,
      // so each ring slowly faded out over a turn and then popped back in
      // when the start wrapped to 0 - a visible jerk every revolution
      // (most often while connecting, when the clock runs fastest). Now
      // the canvas is rotated instead and the sweep always spans 0..2π.
      final rot = (t * (0.6 + f)) % (2 * pi);
      final path = _wave(r, amp, 3 + i, 5 + i * 2, t * (1.2 + i * 0.35), t * (0.8 + i * 0.22), rot);
      final sweep = ui.Gradient.sweep(
        Offset.zero,
        [
          accent.withValues(alpha: 0.0),
          accent.withValues(alpha: 0.85 - f * 0.35),
          Palette.violet.withValues(alpha: 0.65 - f * 0.25),
          accent.withValues(alpha: 0.0),
        ],
        [0.0, 0.35, 0.7, 1.0],
        TileMode.clamp,
        0,
        2 * pi,
      );
      canvas.save();
      canvas.translate(c.dx, c.dy);
      canvas.rotate(rot);
      canvas.drawPath(
        path,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.2 - f * 0.9
          ..shader = sweep
          ..maskFilter = MaskFilter.blur(BlurStyle.solid, 1.5 + 3 * fill),
      );
      canvas.restore();
    }

    // Connecting comet (fades in / out with [comet]).
    if (comet > 0.001) {
      final ra = R * 0.78;
      // 1.0.6: the tail gradient was given absolute angles (head grows
      // without bound) while the sweep shader compares them with the pixel
      // angle in 0..2π, so after the first turn the tail was clamped to
      // transparent and only the head dot showed, flickering. Draw the
      // tail in a frame rotated to the head instead: its gradient always
      // spans 0..1.4 rad.
      final head = (t * 2.4) % (2 * pi);
      canvas.save();
      canvas.translate(c.dx, c.dy);
      canvas.rotate(head - 1.4);
      canvas.drawArc(
        Rect.fromCircle(center: Offset.zero, radius: ra),
        // starts just after 0: the round cap must not reach below angle 0,
        // where the shader would wrap to 2π and clamp to the opaque end.
        0.04,
        1.36,
        false,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeCap = StrokeCap.round
          ..strokeWidth = 3
          ..shader = ui.Gradient.sweep(
              Offset.zero, [Colors.transparent, Palette.lime.withValues(alpha: comet)], [0, 1], TileMode.clamp, 0, 1.4),
      );
      canvas.restore();
      canvas.drawCircle(Offset(c.dx + ra * cos(head), c.dy + ra * sin(head)), 4,
          Paint()
            ..color = Palette.lime.withValues(alpha: comet)
            ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 4));
    }

    // Orbiting sparks when protected.
    if (fill > 0.001) {
      for (var i = 0; i < 9; i++) {
        final a = t * (0.25 + i * 0.03) + i * 2 * pi / 9;
        final rr = R * (0.72 + 0.05 * sin(t + i));
        final p = Offset(c.dx + rr * cos(a), c.dy + rr * sin(a));
        canvas.drawCircle(p, 1.4 + (i % 3) * 0.5,
            Paint()..color = Palette.lime.withValues(alpha: fill * (0.35 + 0.4 * (0.5 + 0.5 * sin(t * 2 + i)))));
      }
    }

    // Core = the button itself (1.0.6 restyle; the rings, comet, sparks,
    // shock and halo above are unchanged). Everything stays inside 0.48 R
    // so the ring band of the animation is untouched. All inputs are the
    // same continuous values (fill, error), so it blends like before.
    final coreR = R * (0.44 + 0.012 * sin(t * 2));
    final coreRect = Rect.fromCircle(center: c, radius: coreR);
    // Soft contact shadow under the button (lifts it off the background).
    canvas.drawCircle(
      c.translate(0, coreR * 0.06),
      coreR * 1.0,
      Paint()
        ..color = Colors.black.withValues(alpha: 0.55 * (1 - fill * 0.6))
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, coreR * 0.05),
    );
    // Bezel: a thin satin ring around the face, lit from the top-left and
    // tinted with the state colour.
    final bezelW = coreR * 0.07;
    final bezelR = coreR + bezelW * 0.15;
    canvas.drawCircle(
      c,
      bezelR,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = bezelW
        ..shader = ui.Gradient.linear(
          c.translate(-bezelR, -bezelR),
          c.translate(bezelR, bezelR),
          [
            Color.lerp(Color.lerp(const Color(0xFF4A4F66), accent, 0.35)!, const Color(0xFFF2FFC4), fill * 0.7)!,
            Color.lerp(const Color(0xFF151824), const Color(0xFF5E8F00), fill)!,
            Color.lerp(Color.lerp(const Color(0xFF1C1F2C), accent, 0.45)!, const Color(0xFF3F6200), fill * 0.8)!,
          ],
          [0, 0.55, 1],
        ),
    );
    // Face.
    final faceR = coreR * 0.97;
    final idleCore = ui.Gradient.radial(c.translate(-faceR * 0.25, -faceR * 0.45), faceR * 1.55,
        [const Color(0xFF2B2F44), const Color(0xFF161925), const Color(0xFF0B0D14)], [0, 0.55, 1]);
    final onCore = ui.Gradient.radial(c.translate(-faceR * 0.25, -faceR * 0.45), faceR * 1.45,
        [const Color(0xFFEFFFB8), Palette.lime, const Color(0xFF6FA300)], [0, 0.5, 1]);
    canvas.drawCircle(c, faceR, Paint()..shader = idleCore);
    if (fill > 0) {
      // 1.0.7: drawn with the paint's alpha instead of an offscreen
      // saveLayer (same pixels for this single circle, no extra layer on
      // every frame while connected).
      canvas.drawCircle(
          c,
          faceR,
          Paint()
            ..shader = onCore
            ..color = Colors.white.withValues(alpha: fill.clamp(0.0, 1.0)));
    }
    // Accent track: a fine inner ring in the state colour, brightest at
    // the top (like a lit dial), dim at the bottom.
    final trackR = faceR * 0.82;
    final trackRect = Rect.fromCircle(center: c, radius: trackR);
    final trackInk = Color.lerp(accent, Palette.limeInk, fill)!;
    canvas.drawCircle(
      c,
      trackR,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = faceR * 0.018
        ..shader = ui.Gradient.linear(trackRect.topCenter, trackRect.bottomCenter, [
          trackInk.withValues(alpha: 0.55 - 0.25 * fill),
          trackInk.withValues(alpha: 0.05),
        ]),
    );
    // Gloss: a soft specular cap on the upper half (glass button look).
    final gloss = Rect.fromCenter(center: c.translate(0, -faceR * 0.42), width: faceR * 1.45, height: faceR * 0.95);
    canvas.drawOval(
      gloss,
      Paint()
        ..shader = ui.Gradient.linear(gloss.topCenter, gloss.bottomCenter, [
          Colors.white.withValues(alpha: 0.13 + 0.12 * fill),
          Colors.white.withValues(alpha: 0.0),
        ]),
    );
    // Inner rim light.
    canvas.drawCircle(
      c,
      faceR,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.2
        ..shader = ui.Gradient.linear(coreRect.topLeft, coreRect.bottomRight,
            [Colors.white.withValues(alpha: 0.4), Colors.white.withValues(alpha: 0.02)]),
    );

    // Glyph: power (idle) morphing into a shield check (protected), with a
    // faint glow in the state colour behind it.
    final ink = Color.lerp(Color.lerp(Palette.text, Palette.danger, error)!, Palette.limeInk, fill)!;
    final s = coreR * 0.40;
    final g = Paint()
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..strokeWidth = coreR * 0.075
      ..color = ink;
    final glow = Paint()
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..strokeWidth = coreR * 0.16
      ..maskFilter = MaskFilter.blur(BlurStyle.normal, coreR * 0.06);
    if (fill < 0.999) {
      final a = 1 - fill;
      final arc = Rect.fromCircle(center: c, radius: s);
      glow.color = Color.lerp(Palette.violet, Palette.danger, error)!.withValues(alpha: 0.35 * a);
      canvas.drawArc(arc, -pi / 2 + 0.7, 2 * pi - 1.4, false, glow);
      canvas.drawLine(c.translate(0, -s * 1.15), c.translate(0, -s * 0.2), glow);
      g.color = ink.withValues(alpha: a);
      canvas.drawArc(arc, -pi / 2 + 0.7, 2 * pi - 1.4, false, g);
      canvas.drawLine(c.translate(0, -s * 1.15), c.translate(0, -s * 0.2), g);
    }
    if (fill > 0.001) {
      g.color = ink.withValues(alpha: fill);
      final sh = Path()
        ..moveTo(c.dx, c.dy - s * 1.1)
        ..lineTo(c.dx + s * 0.9, c.dy - s * 0.7)
        ..quadraticBezierTo(c.dx + s * 0.9, c.dy + s * 0.6, c.dx, c.dy + s * 1.15)
        ..quadraticBezierTo(c.dx - s * 0.9, c.dy + s * 0.6, c.dx - s * 0.9, c.dy - s * 0.7)
        ..close();
      canvas.drawPath(sh, g);
      final ck = Path()
        ..moveTo(c.dx - s * 0.38, c.dy + s * 0.02)
        ..lineTo(c.dx - s * 0.06, c.dy + s * 0.34)
        ..lineTo(c.dx + s * 0.44, c.dy - s * 0.28);
      for (final metric in ck.computeMetrics()) {
        canvas.drawPath(metric.extractPath(0, metric.length * fill), g);
      }
    }
  }

  @override
  bool shouldRepaint(PortalPainter old) =>
      old.fill != fill || old.comet != comet || old.error != error || old.shock != shock;
}
