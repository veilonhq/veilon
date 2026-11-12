// 1.0.7: measurable evidence for the tab-switch (Servers → Home) and home
// scroll jank fixes.
//
// Counts, in debug mode, how many widgets are rebuilt
// (debugOnRebuildDirtyWidget) and how many render objects are painted
// (debugOnProfilePaint) per frame for:
//   * one stats tick (the native side sends one per second while connected),
//   * a Servers → Home tab switch while connected and ticking,
//   * a drag-scroll of the home page while connected, with ticks arriving,
// plus the number of saveLayer calls in one connected Portal frame.
// Host-side build/layout/paint times are printed too (flutter_tester, no GPU,
// so only the relative change means anything).
//
// Run with --dart-define=PERF_BASELINE=true to print the numbers without the
// assertions (used to record the 1.0.6 baseline in logs/perf107-before.log).
import 'dart:convert';
import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/pages/home_page.dart';
import 'package:veilon/ui/shell.dart';
import 'package:veilon/ui/widgets/portal.dart';

import 'helpers.dart';

const baseline = bool.fromEnvironment('PERF_BASELINE');

Map<String, dynamic> _sub() => {
      'id': 's1',
      'url': 'https://panel.example.com:2096/sub/abc',
      'title': 'Veilon Panel',
      'userInfo': {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 1798761600, 'present': true},
      'updatedAt': DateTime(2026, 11, 11, 18, 40).millisecondsSinceEpoch,
    };

Future<AppState> _seeded() async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      vlessReality(),
      wireguard(),
      trojan()..['subId'] = 's1',
      for (var i = 0; i < 12; i++)
        (vlessReality()
          ..['id'] = 'p-sub-$i'
          ..['name'] = 'Node $i · Reality'
          ..['subId'] = 's1'),
    ]),
    'flutter.subs': jsonEncode([_sub()]),
    'flutter.settings': jsonEncode({'language': 'fa'}),
    'flutter.selected': 'p-vless',
  });
  final s = AppState();
  await s.init();
  s.pings.addAll({'p-vless': 142});
  return s;
}

int _rebuilds = 0, _paints = 0;
final _paintTypes = <String, int>{};

void _hook() {
  debugOnRebuildDirtyWidget = (e, _) => _rebuilds++;
  debugOnProfilePaint = (r) {
    _paints++;
    final k = r.runtimeType.toString();
    _paintTypes[k] = (_paintTypes[k] ?? 0) + 1;
  };
}

void _unhook() {
  debugOnRebuildDirtyWidget = null;
  debugOnProfilePaint = null;
}

void _reset() {
  _rebuilds = 0;
  _paints = 0;
  _paintTypes.clear();
}

int _tick = 0;
Map<String, Object?> _stats() {
  _tick++;
  return {
    'type': 'stats',
    'down': 180000.0 + 1000 * _tick,
    'up': 24000.0 + 100 * _tick,
    'sessionDown': 52428800 + 100000 * _tick,
    'sessionUp': 3145728 + 10000 * _tick,
  };
}

Future<AppState> _connectedApp(WidgetTester t, {double w = 360, double h = 780}) async {
  t.view.physicalSize = Size(w * 3, h * 3);
  t.view.devicePixelRatio = 3;
  final state = await _seeded();
  await t.pumpWidget(VeilonApp(state: state));
  await t.pump(const Duration(milliseconds: 500));
  state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
  await t.pump(const Duration(milliseconds: 800));
  for (var i = 0; i < 3; i++) {
    state.handleEvent(_stats());
    await t.pump(const Duration(milliseconds: 400));
  }
  return state;
}

/// Pumps one 16 ms frame and returns its host build+layout+paint time (µs).
Future<int> _frame(WidgetTester t) async {
  final sw = Stopwatch()..start();
  await t.pump(const Duration(milliseconds: 16));
  return sw.elapsedMicroseconds;
}

String _top(Map<String, int> m, [int n = 6]) {
  final e = m.entries.toList()..sort((a, b) => b.value.compareTo(a.value));
  return e.take(n).map((x) => '${x.key}:${x.value}').join(' ');
}

/// A Canvas that counts saveLayer calls and forwards the calls PortalPainter uses.
class _CountingCanvas implements Canvas {
  _CountingCanvas(this.inner);
  final Canvas inner;
  int saveLayers = 0, blurs = 0;
  void _p(Paint p) {
    if (p.maskFilter != null) blurs++;
  }

  @override
  void saveLayer(Rect? bounds, Paint paint) {
    saveLayers++;
    inner.saveLayer(bounds, paint);
  }

  @override
  void save() => inner.save();
  @override
  void restore() => inner.restore();
  @override
  void translate(double dx, double dy) => inner.translate(dx, dy);
  @override
  void rotate(double radians) => inner.rotate(radians);
  @override
  void drawCircle(Offset c, double radius, Paint paint) {
    _p(paint);
    inner.drawCircle(c, radius, paint);
  }

  @override
  void drawPath(Path path, Paint paint) {
    _p(paint);
    inner.drawPath(path, paint);
  }

  @override
  void drawArc(Rect rect, double startAngle, double sweepAngle, bool useCenter, Paint paint) {
    _p(paint);
    inner.drawArc(rect, startAngle, sweepAngle, useCenter, paint);
  }

  @override
  void drawOval(Rect rect, Paint paint) {
    _p(paint);
    inner.drawOval(rect, paint);
  }

  @override
  void drawLine(Offset p1, Offset p2, Paint paint) {
    _p(paint);
    inner.drawLine(p1, p2, paint);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => throw UnimplementedError('${invocation.memberName}');
}

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    installNativeMocks();
    _reset();
    _tick = 0;
  });
  tearDown(_unhook);

  testWidgets('one stats tick rebuilds only the live numbers', (t) async {
    addTearDown(t.view.reset);
    final state = await _connectedApp(t);
    _hook();
    // Home in front.
    _reset();
    state.handleEvent(_stats());
    final us = await _frame(t);
    final homeRebuilds = _rebuilds, homePaints = _paints;
    debugPrint('PERF tick@home rebuilds=$homeRebuilds paints=$homePaints hostUs=$us top=${_top(_paintTypes)}');
    // Servers in front.
    Shell.goTo(t.element(find.byType(TabRail)), 1);
    for (var i = 0; i < 40; i++) {
      await t.pump(const Duration(milliseconds: 16)); // let the switch and row fade-ins finish
    }
    _reset();
    state.handleEvent(_stats());
    final us2 = await _frame(t);
    debugPrint('PERF tick@servers rebuilds=$_rebuilds paints=$_paints hostUs=$us2 top=${_top(_paintTypes)}');
    final serversRebuilds = _rebuilds;
    _unhook();
    if (!baseline) {
      expect(homeRebuilds, lessThan(150), reason: 'a stats tick must not rebuild the whole app');
      expect(serversRebuilds, lessThan(40), reason: 'a stats tick must not rebuild hidden / unrelated pages');
    }
    await t.pump(const Duration(seconds: 2));
    state.dispose();
  });

  testWidgets('servers → home tab switch while connected', (t) async {
    addTearDown(t.view.reset);
    final state = await _connectedApp(t);
    Shell.goTo(t.element(find.byType(TabRail)), 1);
    await t.pump(const Duration(milliseconds: 600));
    for (var round = 0; round < 3; round++) {
      _hook();
      _reset();
      await t.tap(find.byKey(const ValueKey('tab-0')), warnIfMissed: false);
      var maxR = 0, maxP = 0, maxUs = 0, totalUs = 0;
      for (var f = 0; f < 20; f++) {
        if (f == 8) state.handleEvent(_stats()); // a tick lands mid-switch
        final r0 = _rebuilds, p0 = _paints;
        final us = await _frame(t);
        maxR = max(maxR, _rebuilds - r0);
        maxP = max(maxP, _paints - p0);
        maxUs = max(maxUs, us);
        totalUs += us;
      }
      _unhook();
      debugPrint('PERF switch#$round rebuilds=$_rebuilds maxFrameRebuilds=$maxR paints=$_paints maxFramePaints=$maxP '
          'hostAvgUs=${totalUs ~/ 20} hostMaxUs=$maxUs top=${_top(_paintTypes)}');
      if (!baseline && round > 0) {
        expect(maxR, lessThan(150), reason: 'no frame of the switch may rebuild the whole tree');
        expect(_rebuilds, lessThan(600));
      }
      Shell.goTo(t.element(find.byType(TabRail)), 1);
      await t.pump(const Duration(milliseconds: 600));
    }
    expect(Shell.indexOf(t.element(find.byType(TabRail))), 1);
    await t.pump(const Duration(seconds: 2));
    state.dispose();
  });

  testWidgets('home scroll while connected: content is not repainted per frame', (t) async {
    addTearDown(t.view.reset);
    final state = await _connectedApp(t, h: 600);
    final list = find.descendant(of: find.byType(HomePage), matching: find.byType(Scrollable)).first;
    final pos = t.state<ScrollableState>(list).position;
    expect(pos.maxScrollExtent, greaterThan(50), reason: 'the test needs a scrollable home page');
    _hook();
    _reset();
    final g = await t.startGesture(t.getCenter(list));
    var maxR = 0, maxP = 0, maxUs = 0, totalUs = 0;
    const frames = 40;
    for (var f = 0; f < frames; f++) {
      if (f % 15 == 7) state.handleEvent(_stats());
      await g.moveBy(Offset(0, f < frames / 2 ? -12 : 12));
      final r0 = _rebuilds, p0 = _paints;
      final us = await _frame(t);
      maxR = max(maxR, _rebuilds - r0);
      maxP = max(maxP, _paints - p0);
      maxUs = max(maxUs, us);
      totalUs += us;
    }
    await g.up();
    _unhook();
    debugPrint('PERF scroll rebuilds=$_rebuilds maxFrameRebuilds=$maxR paints=$_paints maxFramePaints=$maxP '
        'hostAvgUs=${totalUs ~/ frames} hostMaxUs=$maxUs top=${_top(_paintTypes, 8)}');
    if (!baseline) {
      expect(maxR, lessThan(150), reason: 'a tick during scrolling must not rebuild the whole page');
      expect(_paints / frames, lessThan(40), reason: 'scrolling must not repaint the page content every frame');
    }
    await t.pump(const Duration(seconds: 2));
    state.dispose();
  });

  testWidgets('idle frames and stats ticks keep every ScrollPosition (no theme re-animation)', (t) async {
    addTearDown(t.view.reset);
    final state = await _connectedApp(t);
    final scrollables = find.byType(Scrollable, skipOffstage: false);
    List<ScrollPosition> positions() => [for (final e in scrollables.evaluate()) (e as StatefulElement).state is ScrollableState ? ((e).state as ScrollableState).position : throw StateError('x')];
    var replaced = 0, themeBuilds = 0;
    debugOnRebuildDirtyWidget = (e, _) {
      if (e.widget is AnimatedTheme) themeBuilds++;
    };
    for (var f = 0; f < 120; f++) {
      if (f % 60 == 10) state.handleEvent(_stats());
      final before = positions();
      await t.pump(const Duration(milliseconds: 16));
      final after = positions();
      for (var i = 0; i < before.length && i < after.length; i++) {
        if (!identical(before[i], after[i])) replaced++;
      }
    }
    _unhook();
    debugPrint('PERF positions replaced over 120 frames (2 ticks) = $replaced, AnimatedTheme rebuilds = $themeBuilds');
    if (!baseline) {
      expect(replaced, 0);
      expect(themeBuilds, 0);
    }
    await t.pump(const Duration(seconds: 2));
    state.dispose();
  });

  testWidgets('connected Portal frame uses no saveLayer', (t) async {
    final clock = ValueNotifier<double>(12.3);
    for (final fill in [0.0, 0.5, 1.0]) {
      final rec = ui.PictureRecorder();
      final c = _CountingCanvas(Canvas(rec));
      PortalPainter(clock: clock, fill: fill, turbulence: () => 0.14, comet: 0, error: 0, shock: 0).paint(c, const Size.square(300));
      rec.endRecording().dispose();
      debugPrint('PERF portal fill=$fill saveLayers=${c.saveLayers} blurredDraws=${c.blurs}');
      if (!baseline) expect(c.saveLayers, 0);
    }
    clock.dispose();
  });

  testWidgets('portal and its rings sit in their own repaint layer', (t) async {
    addTearDown(t.view.reset);
    final state = await _connectedApp(t);
    final portalBoundaries = find.descendant(of: find.byType(Portal), matching: find.byType(RepaintBoundary));
    expect(portalBoundaries, findsWidgets);
    final ro = t.renderObject<RenderRepaintBoundary>(portalBoundaries.first);
    expect(ro.isRepaintBoundary, isTrue);
    await t.pump(const Duration(seconds: 2));
    state.dispose();
  });
}
