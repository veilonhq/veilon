import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import '../../core/models.dart';
import '../../theme/palette.dart';

/// Living aurora / mesh-gradient background that reacts to the connection:
/// dim violet when idle, swirling while connecting, a lime-violet bloom when
/// protected and a warm red cast after an error.
class AuroraBackground extends StatefulWidget {
  const AuroraBackground({super.key, required this.state, this.child});
  final ConnState state;
  final Widget? child;

  @override
  State<AuroraBackground> createState() => _AuroraBackgroundState();
}

class _AuroraBackgroundState extends State<AuroraBackground> with SingleTickerProviderStateMixin {
  late final Ticker _ticker;
  final _time = ValueNotifier<double>(0);
  double _phase = 0;
  double _speed = 0.08;
  Duration _last = Duration.zero;
  Duration _lastPaint = Duration.zero;

  double get _targetSpeed => switch (widget.state) {
        ConnState.connecting || ConnState.disconnecting => 0.55,
        ConnState.connected => 0.18,
        _ => 0.08,
      };

  @override
  void initState() {
    super.initState();
    _ticker = createTicker((elapsed) {
      final dt = min(0.05, (elapsed - _last).inMicroseconds / 1e6);
      _last = elapsed;
      // Ease the drift speed so state changes never jerk the blobs.
      _speed += (_targetSpeed - _speed) * min(1.0, dt * 3);
      _phase += dt * _speed;
      // The blobs move slowly: ~30 fps is visually identical and halves the
      // GPU work (battery).
      if (elapsed - _lastPaint >= const Duration(milliseconds: 32)) {
        _lastPaint = elapsed;
        _time.value = _phase;
      }
    })
      ..start();
  }

  @override
  void dispose() {
    _ticker.dispose();
    _time.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final mood = switch (widget.state) {
      ConnState.connected => _Mood(Palette.lime, Palette.violet, Palette.info, 1.0),
      ConnState.connecting || ConnState.disconnecting => _Mood(Palette.violet, Palette.lime, Palette.info, 0.8),
      ConnState.error => _Mood(Palette.danger, Palette.violet, Palette.warn, 0.55),
      _ => _Mood(Palette.violet, Palette.indigoDeep, Palette.info, 0.45),
    };
    // The painter sits in its own layer; content above it (if any) is a
    // separate repaint boundary so aurora frames never repaint it.
    final bg = TweenAnimationBuilder<_Mood>(
      tween: _MoodTween(end: mood),
      duration: const Duration(milliseconds: 350),
      curve: Curves.easeOutCubic,
      builder: (context, m, _) => RepaintBoundary(
        child: CustomPaint(painter: _AuroraPainter(_time, m), size: Size.infinite),
      ),
    );
    if (widget.child == null) return bg;
    return Stack(fit: StackFit.expand, children: [
      Positioned.fill(child: bg),
      RepaintBoundary(child: widget.child),
    ]);
  }
}

class _Mood {
  const _Mood(this.a, this.b, this.c, this.energy);
  final Color a, b, c;
  final double energy;

  @override
  bool operator ==(Object o) => o is _Mood && o.a == a && o.b == b && o.c == c && o.energy == energy;
  @override
  int get hashCode => Object.hash(a, b, c, energy);
}

class _MoodTween extends Tween<_Mood> {
  _MoodTween({super.end});
  @override
  _Mood lerp(double t) {
    final b0 = begin ?? end!;
    return _Mood(Color.lerp(b0.a, end!.a, t)!, Color.lerp(b0.b, end!.b, t)!, Color.lerp(b0.c, end!.c, t)!,
        ui.lerpDouble(b0.energy, end!.energy, t)!);
  }
}

class _AuroraPainter extends CustomPainter {
  _AuroraPainter(this.time, this.mood) : super(repaint: time);
  final ValueNotifier<double> time;
  final _Mood mood;

  @override
  void paint(Canvas canvas, Size size) {
    final t = time.value;
    // 1.0.8: light theme. Additive blending (plus) only works on a dark
    // base; on a light base the blobs are tinted washes (normal blending,
    // softer) and the vignette fades into the light background.
    final light = Palette.isLight;
    final bg = Palette.bg;
    canvas.drawRect(Offset.zero & size, Paint()..color = bg);
    final w = size.width, h = size.height;
    final e = mood.energy;
    final k = light ? 0.55 : 1.0;
    void blob(Color c, double cx, double cy, double r, double alpha) {
      final center = Offset(cx * w, cy * h);
      final paint = Paint()
        ..shader = ui.Gradient.radial(center, r, [c.withValues(alpha: alpha * k), c.withValues(alpha: 0)], [0, 1])
        ..blendMode = light ? BlendMode.srcOver : BlendMode.plus;
      canvas.drawCircle(center, r, paint);
    }

    // Three drifting blobs (Lissajous paths).
    blob(mood.a, 0.5 + 0.28 * sin(t * 0.9), 0.26 + 0.08 * cos(t * 0.7), w * (0.75 + 0.1 * sin(t * 0.5)), 0.26 * e);
    blob(mood.b, 0.2 + 0.2 * cos(t * 0.6 + 1.3), 0.6 + 0.12 * sin(t * 0.8), w * 0.7, 0.20 * e);
    blob(mood.c, 0.85 + 0.12 * sin(t * 0.5 + 2.1), 0.82 + 0.06 * cos(t), w * 0.55, 0.12 * e);
    // Soft vignette for depth.
    canvas.drawRect(
      Offset.zero & size,
      Paint()
        ..shader = ui.Gradient.radial(Offset(w / 2, h * 0.35), max(w, h) * 0.9,
            [light ? bg.withValues(alpha: 0) : Colors.transparent, bg.withValues(alpha: light ? 0.7 : 0.85)], [0.45, 1]),
    );
  }

  @override
  bool shouldRepaint(_AuroraPainter old) => old.mood != mood || old.time != time;
}
