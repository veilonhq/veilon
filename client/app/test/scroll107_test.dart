// 1.0.7: frame-by-frame scroll cost of every scrollable screen (off-device
// proxy for a profile-mode trace, which this build machine cannot run).
//
// For each screen a scripted scroll is played (drag up 25 frames, fling,
// 40 ballistic frames, drag down 25 frames, fling, 30 ballistic frames;
// 16 ms per frame; while connected a stats tick arrives every 60 frames like
// the native side's 1 s tick). Per frame it records:
//   * build: host time of build + layout + paint of the frame (Stopwatch
//     around pump, debug mode, flutter_tester, CPU);
//   * raster: host software time to rasterise that frame's layer tree
//     (root layer → image at 1x; no GPU, so absolute numbers are far from a
//     phone's, only the before/after change means anything);
//   * rebuilt widgets (debugOnRebuildDirtyWidget) and painted render
//     objects (debugOnProfilePaint).
// Over budget = build or raster above 16.7 ms on this host.
// Results go to logs/scroll107-<PERF_TAG>.json (before = 1.0.6 tree,
// after = 1.0.7 tree). The test fails only on errors, not on numbers.
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/pages/about_page.dart';
import 'package:veilon/ui/pages/home_page.dart';
import 'package:veilon/ui/pages/logs_page.dart';
import 'package:veilon/ui/pages/per_app_page.dart';
import 'package:veilon/ui/pages/profile_editor_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/pages/settings_page.dart';
import 'package:veilon/ui/pages/stats_page.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const tag = String.fromEnvironment('PERF_TAG', defaultValue: 'run');
const budgetUs = 16667;
final results = <String, Map<String, Object>>{};

Map<String, dynamic> _sub() => {
      'id': 's1',
      'url': 'https://panel.example.com:2096/sub/abc',
      'title': 'Veilon Panel',
      'userInfo': {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 1798761600, 'present': true},
      'updatedAt': DateTime(2026, 11, 11, 18, 40).millisecondsSinceEpoch,
    };

Future<AppState> _seeded({int servers = 120}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      vlessReality(),
      wireguard(),
      trojan(),
      for (var i = 0; i < servers; i++)
        (i.isEven ? vlessReality() : trojan())
          ..['id'] = 'p-sub-$i'
          ..['name'] = 'Node $i · ${i.isEven ? 'Reality' : 'Trojan'}'
          ..['subId'] = 's1',
    ]),
    'flutter.subs': jsonEncode([_sub()]),
    'flutter.settings': jsonEncode({'language': 'fa'}),
    'flutter.selected': 'p-vless',
  });
  final s = AppState();
  await s.init();
  s.pings.addAll({'p-vless': 142, for (var i = 0; i < servers; i += 3) 'p-sub-$i': 80 + i});
  return s;
}

int _rebuilds = 0, _paints = 0;
void _hook() {
  debugOnRebuildDirtyWidget = (e, _) => _rebuilds++;
  debugOnProfilePaint = (r) => _paints++;
}

void _unhook() {
  debugOnRebuildDirtyWidget = null;
  debugOnProfilePaint = null;
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

Future<AppState> _boot(WidgetTester t, {double h = 640, bool connected = true}) async {
  t.view.physicalSize = Size(360, h);
  t.view.devicePixelRatio = 1;
  final state = await _seeded();
  await t.pumpWidget(VeilonApp(state: state));
  await t.pump(const Duration(milliseconds: 500));
  if (connected) {
    state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
    await t.pump(const Duration(milliseconds: 800));
    for (var i = 0; i < 3; i++) {
      state.handleEvent(_stats());
      await t.pump(const Duration(milliseconds: 400));
    }
  }
  return state;
}

Future<int> _raster(WidgetTester t) async {
  final layer = t.binding.renderViews.first.debugLayer! as OffsetLayer;
  final size = t.view.physicalSize;
  final sw = Stopwatch()..start();
  final img = await t.runAsync(() => layer.toImage(Offset.zero & size));
  final us = sw.elapsedMicroseconds;
  img?.dispose();
  return us;
}

/// Plays the scripted scroll on [scrollable] and stores the frame stats as [name].
Future<void> measure(WidgetTester t, String name, Finder scrollable, {AppState? ticking}) async {
  expect(scrollable, findsWidgets, reason: name);
  final target = scrollable.first;
  // Re-read every time: a rebuild can replace the ScrollPosition.
  ScrollPosition pos() => t.state<ScrollableState>(target).position;
  final build = <int>[], raster = <int>[], rb = <int>[], pt = <int>[];
  var frame = 0;
  final trace = <int>[];
  _hook();
  Future<void> one() async {
    if (ticking != null && frame % 60 == 30) ticking.handleEvent(_stats());
    final r0 = _rebuilds, p0 = _paints;
    final sw = Stopwatch()..start();
    await t.pump(const Duration(milliseconds: 16));
    build.add(sw.elapsedMicroseconds);
    rb.add(_rebuilds - r0);
    pt.add(_paints - p0);
    raster.add(await _raster(t));
    frame++;
  }

  Future<void> drag(double dy, int frames, int ballistic) async {
    final g = await t.startGesture(t.getCenter(target));
    var ts = Duration.zero;
    for (var f = 0; f < frames; f++) {
      ts += const Duration(milliseconds: 16);
      await g.moveBy(Offset(0, dy), timeStamp: ts);
      await one();
      if (f == 3) trace.add(pos().pixels.round());
    }
    await g.up(timeStamp: ts + const Duration(milliseconds: 8));
    for (var f = 0; f < ballistic; f++) {
      await one();
    }
    trace.add(pos().pixels.round());
  }

  final extent = pos().maxScrollExtent;
  await drag(-40, 25, 40);
  final reached = pos().pixels;
  await drag(40, 25, 30);
  _unhook();
  int mx(List<int> l) => l.reduce(max);
  double avg(List<int> l) => l.reduce((a, b) => a + b) / l.length;
  final worst = [for (var i = 0; i < build.length; i++) build[i] + raster[i]];
  final r = <String, Object>{
    'frames': build.length,
    'scrollExtent': extent.round(),
    'reached': reached.round(),
    'trace': trace.join('/'),
    'buildMaxMs': mx(build) / 1000,
    'buildAvgMs': double.parse((avg(build) / 1000).toStringAsFixed(2)),
    'buildOver': build.where((x) => x > budgetUs).length,
    'rasterMaxMs': mx(raster) / 1000,
    'rasterAvgMs': double.parse((avg(raster) / 1000).toStringAsFixed(2)),
    'rasterOver': raster.where((x) => x > budgetUs).length,
    'worstFrameMs': mx(worst) / 1000,
    'rebuildsMax': mx(rb),
    'rebuildsTotal': rb.reduce((a, b) => a + b),
    'paintsMax': mx(pt),
    'paintsTotal': pt.reduce((a, b) => a + b),
  };
  results[name] = r;
  debugPrint('SCROLL $name ${jsonEncode(r)}');
}

/// The page's main scroll view (not a text field's own Scrollable).
Finder _scrollIn(Type page) => find.descendant(
    of: find.descendant(of: find.byType(page), matching: find.byWidgetPredicate((w) => w is ScrollView)),
    matching: find.byType(Scrollable));

Future<void> _push(WidgetTester t, Widget page) async {
  Navigator.of(t.element(find.byType(TabRail))).push(MaterialPageRoute(builder: (_) => page));
  await t.pump();
  await t.pump(const Duration(milliseconds: 600));
  await t.pump(const Duration(milliseconds: 600));
}

Future<void> _done(WidgetTester t, AppState s) async {
  await t.pump(const Duration(seconds: 3));
  s.dispose();
}

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    installNativeMocks();
    _tick = 0;
  });
  tearDown(_unhook);
  tearDownAll(() {
    final f = File('/workspace/work/logs/scroll107-$tag.json');
    if (results.isNotEmpty) f.writeAsStringSync(const JsonEncoder.withIndent(' ').convert(results));
  });

  testWidgets('home (connected)', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t, h: 600);
    await measure(t, 'home', _scrollIn(HomePage), ticking: s);
    await _done(t, s);
  });

  testWidgets('servers 120 cards (connected)', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t);
    Shell.goTo(t.element(find.byType(TabRail)), 1);
    await t.pump(const Duration(milliseconds: 800));
    await measure(t, 'servers-cards', _scrollIn(ServersPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('servers 120 compact (connected)', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t);
    s.setServersCompact(true);
    Shell.goTo(t.element(find.byType(TabRail)), 1);
    await t.pump(const Duration(milliseconds: 800));
    await measure(t, 'servers-compact', _scrollIn(ServersPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('stats (connected)', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t, h: 520);
    Shell.goTo(t.element(find.byType(TabRail)), 2);
    await t.pump(const Duration(milliseconds: 800));
    await measure(t, 'stats', _scrollIn(StatsPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('settings (connected)', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t);
    Shell.goTo(t.element(find.byType(TabRail)), 3);
    await t.pump(const Duration(milliseconds: 800));
    await measure(t, 'settings', _scrollIn(SettingsPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('logs 1500 lines (connected)', (t) async {
    addTearDown(t.view.reset);
    final now = DateTime(2026, 11, 12, 18).millisecondsSinceEpoch;
    fakeLogs = [
      for (var i = 1; i <= 1500; i++)
        {
          'seq': i,
          'time': now + i * 1000,
          'level': const ['info', 'debug', 'warn', 'error'][i % 4],
          'msg': 'inbound/tun[$i]: connection to 10.0.${i % 255}.${i % 200}:443 via proxy (${'x' * (i % 40)})',
        },
    ];
    final s = await _boot(t);
    await _push(t, const LogsPage());
    await t.pump(const Duration(seconds: 2));
    await measure(t, 'logs', _scrollIn(LogsPage), ticking: s);
    fakeLogs = [];
    await _done(t, s);
  });

  testWidgets('per-app 200 apps with icons', (t) async {
    addTearDown(t.view.reset);
    fakeApps = [
      for (var i = 0; i < 200; i++) {'package': 'com.example.app$i', 'label': 'App $i', 'system': false},
    ];
    fakeAppIcon = await t.runAsync(() async {
      final rec = ui.PictureRecorder();
      final c = Canvas(rec);
      c.drawRect(const Rect.fromLTWH(0, 0, 192, 192), Paint()..color = const Color(0xFF3366CC));
      c.drawCircle(const Offset(96, 96), 60, Paint()..color = const Color(0xFFFFCC00));
      final img = await rec.endRecording().toImage(192, 192);
      final bd = await img.toByteData(format: ui.ImageByteFormat.png);
      return Uint8List.view(bd!.buffer);
    });
    final s = await _boot(t);
    await _push(t, const PerAppPage());
    for (var i = 0; i < 10; i++) {
      await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
      await t.pump(const Duration(milliseconds: 50));
    }
    await measure(t, 'per-app', _scrollIn(PerAppPage), ticking: s);
    fakeApps = null;
    fakeAppIcon = null;
    await _done(t, s);
  });

  testWidgets('profile editor', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t);
    ProfileEditorPage.open(t.element(find.byType(TabRail)), Profile(vlessReality()));
    await t.pump();
    await t.pump(const Duration(milliseconds: 600));
    await t.pump(const Duration(milliseconds: 600));
    await measure(t, 'profile-editor', _scrollIn(ProfileEditorPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('about', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t, h: 420);
    await _push(t, const AboutPage());
    await measure(t, 'about', _scrollIn(AboutPage), ticking: s);
    await _done(t, s);
  });

  testWidgets('error details sheet', (t) async {
    addTearDown(t.view.reset);
    final s = await _boot(t, h: 520);
    s.handleEvent({'type': 'state', 'state': 'error', 'error': List.filled(80, 'dial tcp: i/o timeout while connecting to the server').join('\n')});
    await t.pump(const Duration(milliseconds: 600));
    final link = find.text(S.of(t.element(find.byType(HomePage))).t('err.details'));
    if (link.evaluate().isEmpty) {
      debugPrint('SCROLL error-details skipped (no details link)');
      await _done(t, s);
      return;
    }
    await t.tap(link.first);
    await t.pump();
    await t.pump(const Duration(milliseconds: 600));
    await measure(t, 'error-details', find.descendant(of: find.byType(SingleChildScrollView), matching: find.byType(Scrollable)), ticking: null);
    await _done(t, s);
  });
}
