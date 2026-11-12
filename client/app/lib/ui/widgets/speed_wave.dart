import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../theme/palette.dart';

/// Mirrored live-traffic waveform: download rises above the horizon in lime,
/// upload falls below it in violet. New samples slide in smoothly.
class SpeedWave extends StatefulWidget {
  const SpeedWave({super.key, required this.samples, this.height = 120, this.active = true});
  final List<SpeedSample> samples;
  final double height;
  final bool active;

  @override
  State<SpeedWave> createState() => _SpeedWaveState();
}

class _SpeedWaveState extends State<SpeedWave> with SingleTickerProviderStateMixin {
  late final AnimationController _slide =
      AnimationController(vsync: this, duration: const Duration(milliseconds: 950));
  int _lastLen = 0;
  SpeedSample? _lastSample;
  double _scale = 1;

  @override
  void didUpdateWidget(SpeedWave old) {
    super.didUpdateWidget(old);
    final last = widget.samples.isEmpty ? null : widget.samples.last;
    if (!identical(last, _lastSample) || widget.samples.length != _lastLen) {
      _lastSample = last;
      _lastLen = widget.samples.length;
      _slide.forward(from: 0);
    }
  }

  @override
  void dispose() {
    _slide.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final peak = widget.samples.fold<double>(1, (m, s) => max(m, max(s.down, s.up)));
    // Ease the vertical scale so the wave does not jump.
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: _scale, end: peak),
      duration: const Duration(milliseconds: 700),
      curve: Curves.easeOutCubic,
      onEnd: () => _scale = peak,
      builder: (context, scale, _) => AnimatedBuilder(
        animation: _slide,
        builder: (context, _) => CustomPaint(
          size: Size(double.infinity, widget.height),
          painter: _WavePainter(widget.samples, scale, Curves.easeOut.transform(_slide.value), widget.active,
              Directionality.of(context) == TextDirection.rtl),
        ),
      ),
    );
  }
}

class _WavePainter extends CustomPainter {
  _WavePainter(this.samples, this.scale, this.slide, this.active, this.rtl);
  final List<SpeedSample> samples;
  final double scale;
  final double slide;
  final bool active;
  final bool rtl;

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    final mid = h * 0.56;
    final upH = mid - 6, downH = h - mid - 6;
    // Horizon.
    final dash = Paint()
      ..color = Palette.line2
      ..strokeWidth = 1;
    for (double x = 0; x < w; x += 8) {
      canvas.drawLine(Offset(x, mid), Offset(x + 3, mid), dash);
    }
    if (samples.length < 2) return;
    final n = samples.length;
    final step = w / (n - 2);
    double xAt(int i) {
      final x = (i - slide) * step;
      return rtl ? w - x : x;
    }

    double norm(double v) => scale <= 0 ? 0 : sqrt((v / scale).clamp(0.0, 1.0));

    Path curve(double Function(SpeedSample) pick, bool up) {
      final pts = <Offset>[];
      for (var i = 0; i < n; i++) {
        final v = norm(pick(samples[i]));
        pts.add(Offset(xAt(i), up ? mid - v * upH : mid + v * downH * 0.8));
      }
      final p = Path()..moveTo(pts.first.dx, pts.first.dy);
      for (var i = 0; i < pts.length - 1; i++) {
        final p0 = i > 0 ? pts[i - 1] : pts[i];
        final p1 = pts[i], p2 = pts[i + 1];
        final p3 = i + 2 < pts.length ? pts[i + 2] : p2;
        final c1 = p1 + (p2 - p0) / 6;
        final c2 = p2 - (p3 - p1) / 6;
        p.cubicTo(c1.dx, c1.dy, c2.dx, c2.dy, p2.dx, p2.dy);
      }
      return p;
    }

    void draw(Path line, Color color, bool up) {
      final area = Path.from(line)
        ..lineTo(xAt(n - 1), mid)
        ..lineTo(xAt(0), mid)
        ..close();
      canvas.drawPath(
        area,
        Paint()
          ..shader = ui.Gradient.linear(Offset(0, up ? 0 : h), Offset(0, mid),
              [color.withValues(alpha: active ? 0.38 : 0.12), color.withValues(alpha: 0.0)]),
      );
      canvas.drawPath(
        line,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 5
          ..color = color.withValues(alpha: 0.25)
          ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 6),
      );
      canvas.drawPath(
        line,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2
          ..strokeCap = StrokeCap.round
          ..color = color.withValues(alpha: active ? 1 : 0.4),
      );
    }

    canvas.save();
    canvas.clipRect(Offset.zero & size);
    draw(curve((s) => s.down, true), Palette.lime, true);
    draw(curve((s) => s.up, false), Palette.violet, false);
    // Leading dots.
    final lastX = xAt(n - 1);
    final dDown = norm(samples.last.down), dUp = norm(samples.last.up);
    for (final (y, c) in [(mid - dDown * upH, Palette.lime), (mid + dUp * downH * 0.8, Palette.violet)]) {
      canvas.drawCircle(Offset(lastX, y), 6, Paint()..color = c.withValues(alpha: 0.25));
      canvas.drawCircle(Offset(lastX, y), 3, Paint()..color = c);
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(_WavePainter old) => true;
}
