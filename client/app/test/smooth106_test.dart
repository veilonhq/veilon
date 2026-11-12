// 1.0.6: the connect button (Portal) animation must be continuous.
//
// Until 1.0.5 the wave rings turned their colour sweep by moving the sweep
// gradient's start angle (t·speed mod 2π, TileMode.clamp): pixels at angles
// below the start were clamped to the transparent first stop, so every ring
// faded out over one turn and popped back when the start wrapped to 0. The
// comet's tail gradient used unbounded absolute angles and was clamped to
// transparent after its first turn. These tests paint PortalPainter frames
// directly and measure the visible ring / comet energy over time.
import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/ui/widgets/portal.dart';

const _size = 200.0;

Future<List<int>> _paint(WidgetTester t, double clock, {double fill = 0, double comet = 0, double turb = 1}) async {
  final notifier = ValueNotifier<double>(clock);
  final p = PortalPainter(clock: notifier, fill: fill, turbulence: () => turb, comet: comet, error: 0, shock: 0);
  final rec = ui.PictureRecorder();
  final c = Canvas(rec);
  c.drawRect(const Rect.fromLTWH(0, 0, _size, _size), Paint()..color = Colors.black);
  p.paint(c, const Size.square(_size));
  final img = await t.runAsync(() => rec.endRecording().toImage(_size.toInt(), _size.toInt()));
  final bd = await t.runAsync(() => img!.toByteData(format: ui.ImageByteFormat.rawRgba));
  notifier.dispose();
  return bd!.buffer.asUint8List();
}

/// Mean brightness in the annulus [r0, r1] (fractions of the radius).
double _band(List<int> px, double r0, double r1, {bool limeOnly = false}) {
  const n = _size ~/ 1;
  const c = _size / 2;
  var sum = 0.0;
  var cnt = 0;
  for (var y = 0; y < n; y++) {
    for (var x = 0; x < n; x++) {
      final d = sqrt(pow(x + 0.5 - c, 2) + pow(y + 0.5 - c, 2)) / c;
      if (d < r0 || d > r1) continue;
      final i = (y * n + x) * 4;
      final r = px[i], g = px[i + 1], b = px[i + 2];
      if (limeOnly) {
        // lime (#c6ff3d-ish): strong green, weak blue
        if (g > 120 && g > b + 60) sum += g;
      } else {
        sum += 0.2126 * r + 0.7152 * g + 0.0722 * b;
      }
      cnt++;
    }
  }
  return sum / cnt;
}

void main() {
  testWidgets('wave rings keep a steady brightness while turning (no fade-out / pop per revolution)', (t) async {
    // The slowest ring turns at 0.6 rad per clock unit: 2π/0.6 ≈ 10.5
    // units is one revolution. Sample 1.3 revolutions.
    final v = <double>[];
    for (var i = 0; i <= 90; i++) {
      v.add(_band(await _paint(t, 40 + i * 0.15), 0.50, 0.70));
    }
    final mean = v.reduce((a, b) => a + b) / v.length;
    final spread = (v.reduce(max) - v.reduce(min)) / mean;
    var maxStep = 0.0;
    for (var i = 1; i < v.length; i++) {
      maxStep = max(maxStep, (v[i] - v[i - 1]).abs() / mean);
    }
    debugPrint('rings: mean ${mean.toStringAsFixed(2)} spread ${(spread * 100).toStringAsFixed(1)}% maxStep ${(maxStep * 100).toStringAsFixed(1)}%');
    expect(spread, lessThan(0.25), reason: 'ring brightness swings over a revolution: $v');
    expect(maxStep, lessThan(0.08), reason: 'ring brightness jumps between samples: $v');
  });

  testWidgets('comet tail stays visible on every turn', (t) async {
    final v = <double>[];
    for (var i = 0; i <= 60; i++) {
      // late clock values (the app has been open a while)
      v.add(_band(await _paint(t, 500 + i * 0.37, comet: 1, turb: 0.3), 0.72, 0.84, limeOnly: true));
    }
    final mean = v.reduce((a, b) => a + b) / v.length;
    final lo = v.reduce(min);
    debugPrint('comet: mean ${mean.toStringAsFixed(2)} min ${lo.toStringAsFixed(2)} max ${v.reduce(max).toStringAsFixed(2)}');
    expect(lo, greaterThan(mean * 0.6), reason: 'comet tail drops out on some turns: $v');
    // reference: the tail is much brighter than the head dot alone
    final dotOnly = _band(await _paint(t, 500, comet: 0, turb: 0.3), 0.72, 0.84, limeOnly: true);
    expect(mean, greaterThan(dotOnly + 0.5), reason: 'no tail drawn (mean $mean vs no comet $dotOnly)');
  });

  testWidgets('50 rapid state flips: no exception, every blend stays continuous', (t) async {
    t.view.physicalSize = const Size(300, 300);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.reset);
    final state = ValueNotifier(ConnState.disconnected);
    await t.pumpWidget(MaterialApp(
      home: Scaffold(
        body: Center(
          child: ValueListenableBuilder<ConnState>(
            valueListenable: state,
            builder: (_, s, _) => Portal(state: s, onTap: () {}, size: 220),
          ),
        ),
      ),
    ));
    PortalPainter painter() => t
        .widget<CustomPaint>(find.descendant(of: find.byType(Portal), matching: find.byType(CustomPaint)).first)
        .painter! as PortalPainter;
    const cycle = [ConnState.connecting, ConnState.connected, ConnState.disconnecting, ConnState.disconnected, ConnState.error];
    final fills = <double>[], comets = <double>[], errors = <double>[];
    for (var i = 0; i < 50; i++) {
      state.value = cycle[i % cycle.length];
      for (var f = 0; f < 3; f++) {
        await t.pump(const Duration(milliseconds: 16));
        expect(t.takeException(), isNull);
        final p = painter();
        fills.add(p.fill);
        comets.add(p.comet);
        errors.add(p.error);
      }
    }
    double maxStep(List<double> v) {
      var m = 0.0;
      for (var i = 1; i < v.length; i++) {
        m = max(m, (v[i] - v[i - 1]).abs());
      }
      return m;
    }

    expect(maxStep(fills), lessThanOrEqualTo(0.16));
    expect(maxStep(comets), lessThanOrEqualTo(0.16));
    expect(maxStep(errors), lessThanOrEqualTo(0.16));
    await t.pumpWidget(const SizedBox());
    await t.pump(const Duration(seconds: 1));
  });
}
