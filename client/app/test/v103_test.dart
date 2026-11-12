// v1.0.3: (TUN stack picker removed in 1.0.4), servers view mode, manual-only subscription
// refresh with a 10 s cap, split tunneling layout, and frame-by-frame
// animation continuity (power button, tabs, route transition).
// Frame strips are written to build/anim/.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/per_app_page.dart';
import 'package:veilon/ui/pages/settings_page.dart';
import 'package:veilon/ui/shell.dart';
import 'package:veilon/ui/widgets/portal.dart';

import 'helpers.dart';

const animDir = 'build/anim';
const frame = Duration(milliseconds: 16);

final sub1 = {'id': 's1', 'url': 'https://sub.example.com/x', 'name': 'My sub'};

Future<AppState> seeded(String lang, {List<Map<String, Object>>? subs, Map<String, Object> settings = const {}}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([vlessReality(), trojan(), wireguard()]),
    'flutter.settings': jsonEncode({'language': lang, ...settings}),
    'flutter.selected': 'p-vless',
    'flutter.subs': ?(subs == null ? null : jsonEncode(subs)),
  });
  return fresh();
}

/// Every AppState a test created; disposed by [done] (their periodic timer).
final _live = <AppState>[];

Future<AppState> fresh() async {
  final s = AppState();
  await s.init();
  _live.add(s);
  return s;
}

Future<void> done(WidgetTester t) async {
  await t.pumpWidget(const SizedBox());
  for (final s in _live) {
    s.dispose();
  }
  _live.clear();
}

void phone(WidgetTester t, {double width = 360, double height = 780, double dpr = 3}) {
  t.view.physicalSize = Size(width * dpr, height * dpr);
  t.view.devicePixelRatio = dpr;
}

Future<void> tapTab(WidgetTester t, int i) => t.tap(find.byKey(ValueKey('tab-$i')), warnIfMissed: false);

Future<void> settle(WidgetTester t, [int frames = 25]) async {
  for (var i = 0; i < frames; i++) {
    await t.pump(frame);
  }
}

int fetchCount() => calls.where((c) => c.method == 'fetchSub').length;

// ---------- frame capture ----------

Future<ui.Image> grab(WidgetTester t, GlobalKey k, double pr) async {
  final b = k.currentContext!.findRenderObject()! as RenderRepaintBoundary;
  return (await t.runAsync(() => b.toImage(pixelRatio: pr)))!;
}

Future<double> meanLuma(WidgetTester t, ui.Image img) async {
  final bd = (await t.runAsync(() => img.toByteData(format: ui.ImageByteFormat.rawRgba)))!;
  final px = bd.buffer.asUint8List();
  var sum = 0.0;
  for (var i = 0; i < px.length; i += 4) {
    sum += 0.2126 * px[i] + 0.7152 * px[i + 1] + 0.0722 * px[i + 2];
  }
  return sum / (px.length / 4);
}

/// Writes the frames side by side (up to 12 per row) as one PNG.
Future<void> saveStrip(WidgetTester t, List<ui.Image> frames, String name) async {
  if (frames.isEmpty) return;
  await t.runAsync(() async {
    final w = frames.first.width, h = frames.first.height;
    const perRow = 12;
    final cols = frames.length < perRow ? frames.length : perRow;
    final rows = (frames.length + perRow - 1) ~/ perRow;
    final rec = ui.PictureRecorder();
    final c = Canvas(rec);
    c.drawRect(Rect.fromLTWH(0, 0, (w * cols).toDouble(), (h * rows).toDouble()), Paint()..color = Palette.bg);
    for (var i = 0; i < frames.length; i++) {
      c.drawImage(frames[i], Offset(((i % perRow) * w).toDouble(), ((i ~/ perRow) * h).toDouble()), Paint());
    }
    final img = await rec.endRecording().toImage(w * cols, h * rows);
    final bd = await img.toByteData(format: ui.ImageByteFormat.png);
    Directory(animDir).createSync(recursive: true);
    File('$animDir/$name.png').writeAsBytesSync(Uint8List.view(bd!.buffer));
  });
}

/// Largest change between consecutive values.
double maxStep(List<double> v) {
  var m = 0.0;
  for (var i = 1; i < v.length; i++) {
    final d = (v[i] - v[i - 1]).abs();
    if (d > m) m = d;
  }
  return m;
}

// ---------- power button harness ----------

class PortalFrame {
  PortalFrame(PortalPainter p)
      : fill = p.fill,
        comet = p.comet,
        error = p.error,
        shock = p.shock;
  final double fill, comet, error, shock;
}

class PortalRun {
  final frames = <PortalFrame>[];
  final images = <ui.Image>[];

  void check(String name) {
    for (final (label, values) in [
      ('fill', frames.map((f) => f.fill).toList()),
      ('comet', frames.map((f) => f.comet).toList()),
      ('error', frames.map((f) => f.error).toList()),
    ]) {
      expect(maxStep(values), lessThanOrEqualTo(0.16), reason: '$name: $label jumps between 16 ms frames: $values');
    }
    // The shock ring only ever restarts from its finished (invisible) end.
    for (var i = 1; i < frames.length; i++) {
      final a = frames[i - 1].shock, b = frames[i].shock;
      if (b < a) expect(a, greaterThanOrEqualTo(0.99), reason: '$name: shock restarted mid-ring at frame $i ($a → $b)');
      if (b > a) expect(b - a, lessThanOrEqualTo(0.1), reason: '$name: shock jumped at frame $i ($a → $b)');
    }
  }
}

Future<PortalRun> runPortal(WidgetTester t, String name, ConnState start, List<(ConnState, int)> steps) async {
  phone(t, width: 300, height: 300, dpr: 1);
  addTearDown(t.view.reset);
  final state = ValueNotifier(start);
  final k = GlobalKey();
  await t.pumpWidget(MaterialApp(
    home: Scaffold(
      backgroundColor: Palette.bg,
      body: Center(
        child: RepaintBoundary(
          key: k,
          child: ColoredBox(
            color: Palette.bg,
            child: ValueListenableBuilder<ConnState>(
              valueListenable: state,
              builder: (_, s, _) => Portal(state: s, onTap: () {}, size: 220),
            ),
          ),
        ),
      ),
    ),
  ));
  await settle(t, 5);
  final run = PortalRun();
  PortalPainter painter() => t
      .widget<CustomPaint>(find.descendant(of: find.byType(Portal), matching: find.byType(CustomPaint)).first)
      .painter! as PortalPainter;
  var n = 0;
  for (final (s, frames) in steps) {
    state.value = s;
    for (var i = 0; i < frames; i++) {
      await t.pump(frame);
      run.frames.add(PortalFrame(painter()));
      if (n++ % 3 == 0) run.images.add(await grab(t, k, 0.4));
    }
  }
  await saveStrip(t, run.images, 'power_$name');
  run.check(name);
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 1));
  return run;
}

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    fetchSubGate = null;
    fakeApps = null;
    installNativeMocks();
  });

  // ---------- 1. TUN stack: gVisor only (picker removed in 1.0.4) ----------

  test('stored stack / experimentalStack are ignored and never sent to the core', () {
    for (final j in [<String, dynamic>{}, {'stack': 'mixed'}, {'stack': 'system', 'experimentalStack': true}]) {
      final st = AppSettings.fromJson(j);
      final core = st.toCoreJson(assetDir: '', appPackage: '');
      expect(core.containsKey('stack'), isFalse);
      expect(core.containsKey('experimentalStack'), isFalse);
      expect(st.toJson().containsKey('stack'), isFalse);
      expect(st.toJson().containsKey('experimentalStack'), isFalse);
    }
  });

  for (final lang in ['fa', 'en']) {
    testWidgets('settings has no stack picker and no resolver scanner ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang, settings: {'stack': 'system', 'experimentalStack': true, 'dnsResolvers': ['1.1.1.1:53']});
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 10);
      await tapTab(t, 3);
      await settle(t);
      final scroll = find.descendant(of: find.byType(SettingsPage), matching: find.byType(Scrollable)).first;
      await t.scrollUntilVisible(find.byIcon(Icons.receipt_long_outlined), 300, scrollable: scroll);
      await settle(t, 5);
      expect(find.byKey(const ValueKey('stack-picker')), findsNothing);
      expect(find.text('gVisor'), findsNothing);
      expect(find.byIcon(Icons.layers_outlined), findsNothing);
      expect(find.byIcon(Icons.radar_rounded), findsNothing);
      expect(state.settings.toJson().containsKey('dnsResolvers'), isFalse);
      await done(t);
    });
  }

  // ---------- 2. Servers view mode ----------

  for (final lang in ['fa', 'en']) {
    testWidgets('servers view-mode button toggles compact rows and persists ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 10);
      await tapTab(t, 1);
      await settle(t);
      final tile = find.byKey(const ValueKey('p-vless'));
      final full = t.getSize(tile).height;
      expect(state.serversCompact, isFalse);

      await t.tap(find.byKey(const ValueKey('view-mode')));
      await settle(t);
      expect(state.serversCompact, isTrue);
      final compact = t.getSize(tile).height;
      expect(compact, lessThan(full - 10), reason: 'compact $compact vs full $full');
      expect((await SharedPreferences.getInstance()).getBool('serversCompact'), isTrue);
      await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/servers_compact_$lang.png'));

      final again = await fresh();
      expect(again.serversCompact, isTrue);

      await t.tap(find.byKey(const ValueKey('view-mode')));
      await settle(t);
      expect(state.serversCompact, isFalse);
      expect(t.getSize(tile).height, closeTo(full, 0.5));
      expect((await SharedPreferences.getInstance()).getBool('serversCompact'), isFalse);
      await done(t);
    });
  }

  // ---------- 3. Subscription refresh ----------

  testWidgets('no global refresh button; refresh only on each subscription group (1.0.4)', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    var state = await seeded('en');
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t, 10);
    await tapTab(t, 1);
    await settle(t);
    expect(find.byKey(const ValueKey('subs-refresh')), findsNothing);
    expect(find.byTooltip('Update'), findsNothing);
    expect(find.byKey(const ValueKey('view-mode')), findsOneWidget);
    await done(t);

    state = await seeded('en', subs: [sub1]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t, 10);
    await tapTab(t, 1);
    await settle(t);
    expect(find.byKey(const ValueKey('subs-refresh')), findsNothing);
    expect(find.byKey(const ValueKey('group-refresh-s1')), findsOneWidget);
    expect(find.byKey(const ValueKey('group-refresh-_manual')), findsNothing);
    await done(t);
  });

  testWidgets('no automatic subscription refresh on open, resume or while open (no 15-min timer)', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    // updatedAt 0 + autoUpdateSubs on: due, which 1.0.2 fetched 3 s after open.
    final state = await seeded('en', subs: [sub1]);
    await t.pumpWidget(VeilonApp(state: state));
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(seconds: 1));
    }
    expect(fetchCount(), 0);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await t.pump(const Duration(seconds: 1));
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    for (var i = 0; i < 5; i++) {
      await t.pump(const Duration(seconds: 1));
    }
    expect(fetchCount(), 0);
    // 1.0.3: the 15-min foreground periodic check is gone too. Stay in the
    // foreground for 46 minutes (three former ticks): still nothing fetched.
    for (var i = 0; i < 46; i++) {
      await t.pump(const Duration(minutes: 1));
    }
    expect(fetchCount(), 0);
    expect(state.busyUpdating, isFalse);
    await done(t);
  });

  testWidgets('manual refresh: spinner stops after 10 s on a hanging server, no retry', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', subs: [sub1]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t, 10);
    await tapTab(t, 1);
    await settle(t);
    fetchSubGate = Completer<void>();
    final btn = find.byKey(const ValueKey('group-refresh-s1'));
    await t.ensureVisible(btn);
    await t.pump(frame);
    await t.tap(btn);
    await t.pump(frame);
    expect(fetchCount(), 1);
    expect(state.busyUpdating, isTrue);
    expect(find.descendant(of: btn, matching: find.byType(CircularProgressIndicator)), findsOneWidget);

    await t.pump(const Duration(milliseconds: 9500));
    expect(state.busyUpdating, isTrue, reason: 'still within 10 s');
    await t.pump(const Duration(milliseconds: 600));
    await t.pump(frame);
    expect(state.busyUpdating, isFalse, reason: 'spinner must stop at 10 s');
    expect(find.descendant(of: btn, matching: find.byType(CircularProgressIndicator)), findsNothing);
    expect(find.byType(SnackBar), findsOneWidget, reason: 'the result is reported');

    // No automatic retry afterwards.
    for (var i = 0; i < 30; i++) {
      await t.pump(const Duration(seconds: 1));
    }
    expect(fetchCount(), 1);
    fetchSubGate!.complete();
    await t.pump(const Duration(seconds: 5));
    expect(fetchCount(), 1);
    await done(t);
  });

  testWidgets('manual refresh that answers in time ends at once with success', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', subs: [sub1]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t, 10);
    await tapTab(t, 1);
    await settle(t);
    await t.ensureVisible(find.byKey(const ValueKey('group-refresh-s1')));
    await settle(t, 2);
    await t.tap(find.byKey(const ValueKey('group-refresh-s1')));
    await settle(t, 5);
    expect(state.busyUpdating, isFalse);
    expect(fetchCount(), 1);
    expect(find.text('Subscription updated').evaluate().isNotEmpty || find.byType(SnackBar).evaluate().isNotEmpty, isTrue);
    await t.pump(const Duration(seconds: 5));
    await done(t);
  });

  // ---------- 4. Split tunneling ----------

  final apps = [
    for (final (l, p) in [('Telegram', 'org.telegram.messenger'), ('Chrome', 'com.android.chrome'), ('Instagram', 'com.instagram.android'), ('Bale', 'ir.nasim')])
      {'label': l, 'package': p, 'system': false},
  ];

  for (final lang in ['fa', 'en']) {
    for (final (w, dpr, tag) in [(320.0, 2.0, '320'), (1080 / 2.75, 2.75, '1080px'), (360.0, 3.0, '360')]) {
      testWidgets('split tunneling: one-line mode labels, list not dimmed at $tag ($lang)', (t) async {
        phone(t, width: w, dpr: dpr);
        addTearDown(t.view.reset);
        fakeApps = apps;
        final state = await seeded(lang);
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t, 10);
        Navigator.of(t.element(find.byType(Shell))).push(MaterialPageRoute(builder: (_) => const PerAppPage()));
        await settle(t, 30);
        expect(state.settings.perAppMode, 'off');
        // No dimming of the list in any mode.
        final listView = find.descendant(of: find.byType(PerAppPage), matching: find.byType(ListView));
        expect(listView, findsOneWidget);
        for (final o in t.widgetList<AnimatedOpacity>(find.ancestor(of: listView, matching: find.byType(AnimatedOpacity)))) {
          expect(o.opacity, 1.0);
        }
        for (final o in t.widgetList<Opacity>(find.ancestor(of: listView, matching: find.byType(Opacity)))) {
          expect(o.opacity, 1.0);
        }
        for (final m in ['off', 'include', 'exclude']) {
          final seg = find.byKey(ValueKey('perapp-mode-$m'));
          final text = find.descendant(of: seg, matching: find.byType(Text));
          final rp = t.renderObject<RenderParagraph>(find.descendant(of: text, matching: find.byType(RichText)));
          expect(rp.didExceedMaxLines, isFalse, reason: '$m label must fit one line');
          final shown = t.getRect(text);
          final segRect = t.getRect(seg);
          expect(shown.width, lessThanOrEqualTo(segRect.width + 0.5));
          final scale = shown.width / rp.size.width;
          expect(scale, greaterThanOrEqualTo(0.72), reason: '$m label shrunk to ${scale.toStringAsFixed(2)}');
          final label = (t.widget<Text>(text).data)!;
          expect(label.contains('\u200c'), isFalse, reason: 'no ZWNJ');
        }
        await t.tap(find.byKey(const ValueKey('perapp-mode-include')));
        await settle(t, 10);
        expect(state.settings.perAppMode, 'include');
        if (tag != '1080px') {
          await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/split_tunneling_${tag}_$lang.png'));
        }
        await done(t);
      });
    }
  }

  // ---------- 5. Animation continuity ----------

  group('power button continuity (16 ms steps)', () {
    testWidgets('on: connecting → connected', (t) async {
      final r = await runPortal(t, 'on', ConnState.disconnected, [(ConnState.connecting, 20), (ConnState.connected, 30)]);
      expect(r.frames.last.fill, closeTo(1, 0.01));
      expect(r.frames.last.comet, closeTo(0, 0.01));
    });
    testWidgets('off: disconnecting → disconnected', (t) async {
      final r = await runPortal(t, 'off', ConnState.connected, [(ConnState.disconnecting, 20), (ConnState.disconnected, 30)]);
      expect(r.frames.last.fill, closeTo(0, 0.01));
    });
    testWidgets('reverse mid-way: connected then disconnected after 120 ms', (t) async {
      final r = await runPortal(t, 'reverse', ConnState.disconnected, [(ConnState.connected, 8), (ConnState.disconnected, 30)]);
      final peak = r.frames.take(9).map((f) => f.fill).reduce((a, b) => a > b ? a : b);
      expect(peak, inExclusiveRange(0.05, 0.95), reason: 'reversed while still filling');
      expect(r.frames.last.fill, closeTo(0, 0.01));
    });
    testWidgets('connecting → error', (t) async {
      final r = await runPortal(t, 'error', ConnState.disconnected, [(ConnState.connecting, 15), (ConnState.error, 30)]);
      expect(r.frames.last.error, closeTo(1, 0.01));
      expect(r.frames.last.comet, closeTo(0, 0.01));
    });
    testWidgets('connecting → connected → (quick) error', (t) async {
      await runPortal(t, 'connected_error', ConnState.disconnected,
          [(ConnState.connecting, 10), (ConnState.connected, 6), (ConnState.error, 30)]);
    });
  });

  group('tab switch continuity (16 ms steps)', () {
    List<double> deck(WidgetTester t) => [
          for (var i = 0; i < Shell.pageCount; i++)
            () {
              final f = find.byKey(ValueKey('deck-opacity-$i'), skipOffstage: false);
              final o = t.widget<Opacity>(f);
              final off = find.ancestor(of: f, matching: find.byType(Offstage)).evaluate().any((e) => (e.widget as Offstage).offstage);
              return off ? 0.0 : o.opacity;
            }(),
        ];

    Future<void> run(WidgetTester t, String name, List<(int?, int)> steps) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('fa');
      final k = GlobalKey();
      await t.pumpWidget(RepaintBoundary(key: k, child: VeilonApp(state: state)));
      await settle(t, 10);
      final rows = <List<double>>[];
      final images = <ui.Image>[];
      var n = 0;
      for (final (tab, frames) in steps) {
        if (tab != null) await tapTab(t, tab);
        for (var i = 0; i < frames; i++) {
          await t.pump(frame);
          rows.add(deck(t));
          if (n++ % 2 == 0) images.add(await grab(t, k, 0.25));
        }
      }
      await saveStrip(t, images, 'tabs_$name');
      for (var p = 0; p < Shell.pageCount; p++) {
        final col = rows.map((r) => r[p]).toList();
        expect(maxStep(col), lessThanOrEqualTo(0.2), reason: '$name: page $p opacity jumps: $col');
      }
      // Never a blank frame: something is (nearly) fully shown at every step.
      for (final r in rows) {
        expect(r.reduce((a, b) => a + b), greaterThan(0.6), reason: '$name: frame too empty $r');
      }
      final front = Shell.indexOf(t.element(find.byType(TabRail)));
      for (var i = 0; i < Shell.pageCount; i++) {
        expect(rows.last[i], closeTo(i == front ? 1.0 : 0.0, 0.001));
      }
      await done(t);
    }

    testWidgets('home → servers', (t) => run(t, 'single', [(1, 24)]));
    testWidgets('quick double tap: servers then stats 80 ms later', (t) => run(t, 'double', [(1, 5), (2, 24)]));
    testWidgets('quick tap back: servers then home 50 ms later', (t) => run(t, 'back', [(1, 3), (0, 24)]));
  });

  testWidgets('route transition to split tunneling: no dark haze (16 ms steps)', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    fakeApps = apps;
    final state = await seeded('fa');
    final k = GlobalKey();
    await t.pumpWidget(RepaintBoundary(key: k, child: VeilonApp(state: state)));
    await settle(t, 10);
    await tapTab(t, 3);
    await settle(t);
    final before = await meanLuma(t, await grab(t, k, 0.25));
    Navigator.of(t.element(find.byType(Shell))).push(MaterialPageRoute(builder: (_) => const PerAppPage()));
    final luma = <double>[];
    final images = <ui.Image>[];
    for (var i = 0; i < 24; i++) {
      await t.pump(frame);
      final img = await grab(t, k, 0.25);
      luma.add(await meanLuma(t, img));
      if (i % 2 == 0) images.add(img);
    }
    await saveStrip(t, images, 'route_split_tunneling');
    final after = luma.last;
    final floor = (before < after ? before : after) - 4;
    for (var i = 0; i < luma.length; i++) {
      expect(luma[i], greaterThanOrEqualTo(floor), reason: 'frame $i darker than both pages: $luma (before $before)');
    }
    expect(maxStep([before, ...luma]), lessThanOrEqualTo(12), reason: 'brightness jump: $luma');
    await done(t);
  });
}
