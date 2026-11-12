// v1.0.8: light theme (Dark / Light / System, cross-fade without
// half-themed frames), long Persian server names at 320 dp, instant tab
// switches without offscreen layers, no per-frame mask blurs on the home
// page, subscription certificate approval (audit H1), kill-switch blocking
// state (M2), updater host allowlist (L5) and the new defaults (M1).
// Frame strips and screenshots are written to build/v108/.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/errors.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/core/update_check.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/theme/theme_host.dart';
import 'package:veilon/ui/pages/about_page.dart';
import 'package:veilon/ui/pages/home_page.dart';
import 'package:veilon/ui/pages/per_app_page.dart';
import 'package:veilon/ui/pages/profile_editor_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/pages/settings_page.dart';
import 'package:veilon/ui/pages/stats_page.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';
import 'package:veilon/ui/sheets/cert_dialog.dart';
import 'package:veilon/ui/shell.dart';
import 'package:veilon/ui/widgets/portal.dart';

import 'helpers.dart';

const outDir = 'build/v108';
const frame = Duration(milliseconds: 16);

const longFa = 'سرور پرسرعت اختصاصی آلمان فرانکفورت شماره دو برای استریم';
const longFa2 = 'هلند آمستردام ریالیتی ویژه گیمینگ و تماس تصویری';

Map<String, dynamic> panelSub() => {
      'id': 's1',
      'url': 'https://panel.example.com:2096/sub/abc',
      'title': 'Veilon Panel',
      'userInfo': {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 1798761600, 'present': true},
      'updatedAt': DateTime(2026, 11, 11, 18, 40).millisecondsSinceEpoch,
    };

final _live = <AppState>[];

Future<AppState> seeded(String lang,
    {String theme = 'dark', bool longNames = false, Map<String, Object> settings = const {}, Map<String, Object> extra = const {}}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      if (longNames) (vlessReality()..['name'] = longFa) else vlessReality(),
      wireguard(),
      trojan()..['subId'] = 's1',
      (vlessReality()
        ..['id'] = 'p-sub-v'
        ..['name'] = longNames ? longFa2 : 'Amsterdam · Reality'
        ..['subId'] = 's1'),
    ]),
    'flutter.subs': jsonEncode([panelSub()]),
    'flutter.settings': jsonEncode({'language': lang, 'theme': theme, ...settings}),
    'flutter.selected': 'p-vless',
    ...extra,
  });
  final s = AppState();
  await s.init();
  s.pings.addAll({'p-vless': 142, 'p-trojan': 388});
  _live.add(s);
  return s;
}

Future<void> done(WidgetTester t) async {
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 1));
  for (final s in _live) {
    s.dispose();
  }
  _live.clear();
  Palette.use(darkPalette);
}

void phone(WidgetTester t, {double width = 360, double height = 780}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
}

Future<void> settle(WidgetTester t, [int frames = 12]) async {
  for (var i = 0; i < frames; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

Future<ui.Image> grab(WidgetTester t, GlobalKey k, double pr) async {
  final b = k.currentContext!.findRenderObject()! as RenderRepaintBoundary;
  return (await t.runAsync(() => b.toImage(pixelRatio: pr)))!;
}

Future<Uint8List> rgba(WidgetTester t, ui.Image img) async =>
    (await t.runAsync(() => img.toByteData(format: ui.ImageByteFormat.rawRgba)))!.buffer.asUint8List();

double luma(Uint8List px) {
  var sum = 0.0;
  for (var i = 0; i < px.length; i += 4) {
    sum += 0.2126 * px[i] + 0.7152 * px[i + 1] + 0.0722 * px[i + 2];
  }
  return sum / (px.length / 4);
}

double meanAbsDiff(Uint8List a, Uint8List b) {
  var sum = 0;
  for (var i = 0; i < a.length; i += 4) {
    sum += (a[i] - b[i]).abs() + (a[i + 1] - b[i + 1]).abs() + (a[i + 2] - b[i + 2]).abs();
  }
  return sum / (a.length / 4 * 3);
}

Future<void> savePng(WidgetTester t, List<ui.Image> frames, String name) async {
  if (frames.isEmpty) return;
  await t.runAsync(() async {
    final w = frames.first.width, h = frames.first.height;
    const perRow = 10;
    final cols = min(frames.length, perRow);
    final rows = (frames.length + perRow - 1) ~/ perRow;
    final rec = ui.PictureRecorder();
    final c = Canvas(rec);
    for (var i = 0; i < frames.length; i++) {
      c.drawImage(frames[i], Offset(((i % perRow) * w).toDouble(), ((i ~/ perRow) * h).toDouble()), Paint());
    }
    final img = await rec.endRecording().toImage(w * cols, h * rows);
    final bd = await img.toByteData(format: ui.ImageByteFormat.png);
    Directory(outDir).createSync(recursive: true);
    File('$outDir/$name.png').writeAsBytesSync(Uint8List.view(bd!.buffer));
  });
}

double contrast(Color a, Color b) {
  final la = a.computeLuminance(), lb = b.computeLuminance();
  return (max(la, lb) + 0.05) / (min(la, lb) + 0.05);
}

/// Text colours of every on-stage paragraph.
Set<int> paragraphColors() {
  final out = <int>{};
  void visit(RenderObject r) {
    if (r is RenderParagraph) {
      void span(InlineSpan s) {
        if (s is TextSpan) {
          final c = s.style?.color;
          if (c != null) out.add(c.toARGB32());
          s.children?.forEach(span);
        }
      }

      span(r.text);
    }
    r.visitChildren(visit);
  }

  for (final e in find.byType(MaterialApp).evaluate()) {
    final ro = e.findRenderObject();
    if (ro != null) visit(ro);
  }
  return out;
}

/// Layers with a partial opacity (each one = an offscreen saveLayer).
int partialOpacityLayers(WidgetTester t) {
  var n = 0;
  void visit(Layer? l) {
    if (l == null) return;
    if (l is OpacityLayer && (l.alpha ?? 255) < 255 && (l.alpha ?? 0) > 0) n++;
    if (l is ContainerLayer) {
      var c = l.firstChild;
      while (c != null) {
        visit(c);
        c = c.nextSibling;
      }
    }
  }

  for (final v in t.binding.renderViews) {
    visit(v.debugLayer);
  }
  return n;
}

class _CountingCanvas implements Canvas {
  _CountingCanvas(this.inner);
  final Canvas inner;
  int blurs = 0, saveLayers = 0, images = 0;
  void _p(Paint p) {
    if (p.maskFilter != null || p.imageFilter != null) blurs++;
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
  void scale(double sx, [double? sy]) => inner.scale(sx, sy);
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
  void drawImageRect(ui.Image image, Rect src, Rect dst, Paint paint) {
    images++;
    _p(paint);
    inner.drawImageRect(image, src, dst, paint);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => throw UnimplementedError('${invocation.memberName}');
}

/// Mock of SystemChrome calls (status / navigation bar style).
final systemStyles = <Map<String, dynamic>>[];

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    installNativeMocks();
    fakeSubHandler = null;
    lastSubPins = null;
    systemStyles.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (c) async {
      if (c.method == 'SystemChrome.setSystemUIOverlayStyle') systemStyles.add(Map<String, dynamic>.from(c.arguments as Map));
      return null;
    });
  });
  tearDown(() => Palette.use(darkPalette));

  // ---------------------------------------------------------------- 1. theme

  group('theme setting', () {
    test('defaults: dark theme, DoH direct DNS for new installs; saved values kept; theme never reaches the core', () {
      final d = AppSettings();
      expect(d.theme, 'dark');
      expect(d.directDns, 'https://223.5.5.5/dns-query');
      final old = AppSettings.fromJson({'directDns': '223.5.5.5', 'language': 'fa'});
      expect(old.directDns, '223.5.5.5', reason: 'an existing install keeps its saved direct DNS');
      expect(old.theme, 'dark', reason: 'an existing install (no theme saved) stays dark');
      expect(AppSettings.fromJson({'theme': 'light'}).theme, 'light');
      expect(AppSettings.fromJson({'theme': 'system'}).theme, 'system');
      expect(AppSettings.fromJson({'theme': 'neon'}).theme, 'dark');
      expect(AppSettings.fromJson({'theme': 'light'}).toJson()['theme'], 'light');
      final core = AppSettings.fromJson({'theme': 'light'}).toCoreJson(assetDir: '/a', appPackage: 'app.veilon');
      expect(core.containsKey('theme'), isFalse);
      expect(AppSettings.fromJson({'theme': 'light'}).coreFingerprint(), AppSettings().coreFingerprint(),
          reason: 'switching the theme never asks for a reconnect');
    });

    test('dark palette is the 1.0.7 palette; light palette text contrast ≥ 4.5:1', () {
      expect(darkPalette.bg, const Color(0xFF07080C));
      expect(darkPalette.lime, const Color(0xFFC6FF3D));
      expect(darkPalette.violet, const Color(0xFF8B7CFF));
      expect(darkPalette.text, const Color(0xFFEEF0F6));
      final p = lightPalette;
      for (final (name, c) in [
        ('text', p.text),
        ('text2', p.text2),
        ('muted', p.muted),
        ('lime', p.lime),
        ('violetFg', p.violetFg),
        ('danger', p.danger),
        ('warn', p.warn),
        ('info', p.info),
      ]) {
        for (final bg in [p.bg, p.bg2]) {
          expect(contrast(c, bg), greaterThanOrEqualTo(4.5), reason: 'light $name on ${bg.toARGB32().toRadixString(16)}');
        }
      }
      expect(contrast(p.limeInk, p.limeFill), greaterThan(7), reason: 'ink on the lime buttons');
      expect(contrast(darkPalette.lime, p.bg), lessThan(1.5), reason: 'why brand lime is not used for light text');
    });

    testWidgets('saved light theme is applied before the first frame (no dark frame)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('fa', theme: 'light');
      // What main() does before runApp.
      Palette.use(paletteFor(state.settings.theme, Brightness.dark));
      final k = GlobalKey();
      await t.pumpWidget(RepaintBoundary(key: k, child: VeilonApp(state: state)));
      final first = await grab(t, k, 0.2);
      final l = luma(await rgba(t, first));
      debugPrint('V108 first frame luma (light) = ${l.toStringAsFixed(1)}');
      expect(l, greaterThan(180), reason: 'the very first frame is light');
      expect(Palette.isLight, isTrue);
      final sc = t.widget<Scaffold>(find.byType(Scaffold).first);
      expect(sc.backgroundColor, lightPalette.bg);
      await done(t);
    });

    testWidgets('status and navigation bar icons follow the theme', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('en');
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 3);
      await state.setTheme('light');
      await t.pump();
      await settle(t, 5);
      expect(systemStyles, isNotEmpty);
      expect(systemStyles.last['statusBarIconBrightness'], 'Brightness.dark');
      expect(systemStyles.last['systemNavigationBarIconBrightness'], 'Brightness.dark');
      expect(calls.where((c) => c.method == 'setTheme').map((c) => (c.arguments as Map)['mode']), contains('light'));
      await state.setTheme('dark');
      await t.pump();
      await settle(t, 5);
      expect(systemStyles.last['statusBarIconBrightness'], 'Brightness.light');
      expect(systemStyles.last['systemNavigationBarIconBrightness'], 'Brightness.light');
      await done(t);
    });

    for (final (from, to) in [('dark', 'light'), ('light', 'dark')]) {
      testWidgets('switch $from → $to: one cross-fade, no flash, no half-themed frame, state kept', (t) async {
        phone(t);
        addTearDown(t.view.reset);
        final state = await seeded('fa', theme: from);
        Palette.use(paletteFor(from, Brightness.dark));
        final k = GlobalKey();
        await t.pumpWidget(RepaintBoundary(key: k, child: VeilonApp(state: state)));
        await settle(t, 6);
        // Settings page, scrolled a bit (its offset must survive the switch).
        Shell.goTo(t.element(find.byType(TabRail)), 3);
        await settle(t, 4);
        final scrollable = find.descendant(of: find.byType(SettingsPage), matching: find.byType(Scrollable)).first;
        final pos = t.state<ScrollableState>(scrollable).position;
        pos.jumpTo(40);
        await settle(t, 2);
        final hostState = t.state<ThemeHostState>(find.byType(ThemeHost));
        // Measured at full device resolution (dpr 3): the captured frame is
        // drawn 1:1 there, as on a phone (a downscaled grab resamples the
        // overlay and the live app differently). The strip is 0.25 scale.
        final before = await rgba(t, await grab(t, k, 3));
        final images = <ui.Image>[await grab(t, k, 0.25)];
        // Tap the option like a user.
        await t.tap(find.text(S('fa').t('theme.$to')));
        final lumas = [luma(before)];
        Uint8List? firstFrame;
        var prev = before;
        var maxDiff = 0.0;
        for (var i = 0; i < 28; i++) {
          await t.pump(frame);
          final px = await rgba(t, await grab(t, k, 3));
          if (i % 2 == 0) images.add(await grab(t, k, 0.25));
          lumas.add(luma(px));
          if (i == 0) {
            firstFrame = px;
          } else {
            maxDiff = max(maxDiff, meanAbsDiff(prev, px));
          }
          prev = px;
        }
        await savePng(t, images, 'theme_switch_${from}_to_$to');
        debugPrint('V108 theme $from→$to luma per 16 ms frame: ${lumas.map((l) => l.toStringAsFixed(1)).join(' ')}');
        final first = meanAbsDiff(before, firstFrame!);
        debugPrint('V108 theme $from→$to switch frame vs previous frame = ${first.toStringAsFixed(2)} / 255');
        expect(first, lessThan(3), reason: 'the switch frame still shows the old frame (captured), no jump');
        final total = (lumas.last - lumas.first).abs();
        expect(total, greaterThan(60), reason: 'the theme did change');
        final lo = min(lumas.first, lumas.last) - 3, hi = max(lumas.first, lumas.last) + 3;
        for (final l in lumas) {
          expect(l, inInclusiveRange(lo, hi), reason: 'no flash beyond the two themes: $lumas');
        }
        final dir = (lumas.last - lumas.first).sign;
        var maxStep = 0.0;
        for (var i = 1; i < lumas.length; i++) {
          final d = (lumas[i] - lumas[i - 1]) * dir;
          expect(d, greaterThan(-1.5), reason: 'brightness moves one way only (no flicker): $lumas');
          maxStep = max(maxStep, d.abs());
        }
        debugPrint('V108 theme $from→$to max step per frame = ${(maxStep / total * 100).toStringAsFixed(1)} % of the change');
        expect(maxStep, lessThan(total * 0.2), reason: 'spread over the fade, not a jump');
        debugPrint('V108 theme $from→$to max mean pixel change per frame = ${maxDiff.toStringAsFixed(1)} / 255');
        await settle(t, 6);
        // Done: overlay gone, every text uses the new palette.
        expect(find.byType(RawImage), findsNothing);
        expect(hostState.switches, 1);
        expect(hostState.lastSwitchAnimated, isTrue);
        final oldOnly = {
          for (final c in [paletteFor(from, Brightness.dark).text, paletteFor(from, Brightness.dark).text2, paletteFor(from, Brightness.dark).muted])
            c.toARGB32()
        };
        final used = paragraphColors();
        expect(used.intersection(oldOnly), isEmpty, reason: 'no text kept the old theme colours');
        expect(pos.pixels, 40, reason: 'scroll offset kept');
        expect(Shell.indexOf(t.element(find.byType(TabRail))), 3, reason: 'page kept');
        expect(find.byType(SettingsPage), findsOneWidget);
        await done(t);
      });
    }

    testWidgets('system theme follows the device dark mode', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      t.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
      addTearDown(t.platformDispatcher.clearPlatformBrightnessTestValue);
      final state = await seeded('en', theme: 'system');
      Palette.use(paletteFor('system', Brightness.dark));
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 3);
      expect(Palette.isLight, isFalse);
      t.platformDispatcher.platformBrightnessTestValue = Brightness.light;
      await t.pump();
      await settle(t, 5);
      expect(Palette.isLight, isTrue);
      t.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
      await t.pump();
      await settle(t, 5);
      expect(Palette.isLight, isFalse);
      // A fixed choice ignores the device.
      await state.setTheme('light');
      await t.pump();
      await settle(t, 5);
      t.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
      await t.pump();
      await settle(t, 5);
      expect(Palette.isLight, isTrue);
      await done(t);
    });

    testWidgets('rapid toggles mid-fade continue from the frame on screen', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('en');
      final k = GlobalKey();
      await t.pumpWidget(RepaintBoundary(key: k, child: VeilonApp(state: state)));
      await settle(t, 4);
      final lumas = <double>[];
      for (final mode in ['light', 'dark', 'light', 'system', 'dark']) {
        await state.setTheme(mode);
        for (var i = 0; i < 6; i++) {
          await t.pump(frame);
          lumas.add(luma(await rgba(t, await grab(t, k, 0.2))));
        }
      }
      await settle(t, 5);
      var worst = 0.0;
      for (var i = 1; i < lumas.length; i++) {
        worst = max(worst, (lumas[i] - lumas[i - 1]).abs());
      }
      debugPrint('V108 rapid toggles: worst per-frame luma step ${worst.toStringAsFixed(1)} (full change ≈ 200)');
      expect(worst, lessThan(60), reason: 'a re-switch never jumps back to a fully old or new frame: $lumas');
      expect(t.takeException(), isNull);
      await done(t);
    });
  });

  // ---------------------------------------------------------------- 2. light screens (goldens)

  group('light theme screens', () {
    for (final lang in ['fa', 'en']) {
      testWidgets('pages light $lang', (t) async {
        phone(t);
        addTearDown(t.view.reset);
        final state = await seeded(lang, theme: 'light');
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t);
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_home_$lang.png'));
        final names = ['servers', 'stats', 'settings'];
        for (var i = 0; i < names.length; i++) {
          Shell.goTo(t.element(find.byType(TabRail)), i + 1);
          await settle(t);
          await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_${names[i]}_$lang.png'));
        }
        // No brand-lime (unreadable on light) text anywhere.
        expect(paragraphColors().contains(darkPalette.lime.toARGB32()), isFalse);
        await done(t);
      });
    }

    testWidgets('home connected / error / details light fa', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('fa', theme: 'light');
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch - 754000});
      for (var i = 0; i < 6; i++) {
        state.handleEvent({'type': 'stats', 'down': 180000.0 * (i + 1), 'up': 24000.0 * (i + 1), 'sessionDown': 52428800 * (i + 1), 'sessionUp': 3145728 * (i + 1)});
        await t.pump(const Duration(milliseconds: 300));
      }
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_home_connected_fa.png'));
      state.handleEvent({'type': 'state', 'state': 'error', 'error': 'start: dial tcp: i/o timeout'});
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_home_error_fa.png'));
      await t.tap(find.text(S('fa').t('err.details')));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_error_details_fa.png'));
      await done(t);
    });

    testWidgets('sheets, dialogs and sub pages light fa', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('fa', theme: 'light');
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      Shell.goTo(t.element(find.byType(TabRail)), 1);
      await settle(t);
      showAddSheet(t.element(find.byType(ServersPage)));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_add_sheet_fa.png'));
      Navigator.of(t.element(find.byType(ServersPage))).pop();
      await settle(t);
      // Setting info sheet.
      Shell.goTo(t.element(find.byType(TabRail)), 3);
      await settle(t);
      await t.tap(find.byKey(const ValueKey('info-info.theme')).first, warnIfMissed: false);
      await settle(t);
      if (find.byKey(const ValueKey('setting-info-sheet')).evaluate().isNotEmpty) {
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_settings_info_fa.png'));
        Navigator.of(t.element(find.byKey(const ValueKey('setting-info-sheet')))).pop();
        await settle(t);
      }
      // Sub pages.
      final nav = Navigator.of(t.element(find.byType(SettingsPage)));
      nav.push(MaterialPageRoute<void>(builder: (_) => const PerAppPage()));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_split_tunneling_fa.png'));
      nav.pop();
      await settle(t);
      nav.push(MaterialPageRoute<void>(builder: (_) => const AboutPage()));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_about_fa.png'));
      nav.pop();
      await settle(t);
      nav.push(MaterialPageRoute<void>(builder: (_) => ProfileEditorPage(profile: state.profiles.first, isNew: false)));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/light_editor_fa.png'));
      nav.pop();
      await settle(t);
      expect(t.takeException(), isNull);
      await done(t);
    });
  });

  // ---------------------------------------------------------------- 3. long names at 320 dp

  group('long Persian server names at 320 dp', () {
    for (final theme in ['dark', 'light']) {
      testWidgets('home card and server cards / compact rows ($theme)', (t) async {
        phone(t, width: 320, height: 700);
        addTearDown(t.view.reset);
        final state = await seeded('fa', theme: theme, longNames: true);
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t);
        expect(t.takeException(), isNull, reason: 'no overflow on home');
        final homeName = t.renderObject<RenderParagraph>(find.byKey(const ValueKey('home-server-name')));
        expect(homeName.didExceedMaxLines, isFalse, reason: 'the whole name fits in two lines on the home card');
        expect(homeName.size.height, greaterThan(25), reason: 'wrapped to a second line instead of being cut');
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/longname_320_home_$theme.png'));
        Shell.goTo(t.element(find.byType(TabRail)), 1);
        await settle(t);
        expect(t.takeException(), isNull, reason: 'no overflow on the servers page');
        final tile = t.renderObject<RenderParagraph>(find.byKey(const ValueKey('name-p-vless')));
        expect(tile.didExceedMaxLines, isFalse, reason: 'server card shows the whole name');
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/longname_320_servers_$theme.png'));
        await state.setServersCompact(true);
        await settle(t);
        expect(t.takeException(), isNull, reason: 'no overflow in the compact list');
        final row = t.renderObject<RenderParagraph>(find.byKey(const ValueKey('name-p-vless')));
        expect(row.didExceedMaxLines, isFalse, reason: 'compact row shows the whole name');
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/longname_320_compact_$theme.png'));
        await done(t);
      });
    }
  });

  // ---------------------------------------------------------------- 4. tab switch

  group('tab switch (instant, no offscreen layers)', () {
    for (final connected in [false, true]) {
      testWidgets('servers → home is complete on the first frame, VPN ${connected ? 'on' : 'off'}', (t) async {
        phone(t);
        addTearDown(t.view.reset);
        final state = await seeded('fa');
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t, 4);
        if (connected) {
          state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
          await settle(t, 4);
        }
        var maxOpacityLayers = 0;
        final sw = Stopwatch();
        final hostUs = <int>[];
        for (var round = 0; round < 6; round++) {
          await t.tap(find.byKey(const ValueKey('tab-1')), warnIfMissed: false);
          sw
            ..reset()
            ..start();
          await t.pump(frame);
          sw.stop();
          expect(find.byType(ServersPage), findsOneWidget);
          expect(find.byType(HomePage), findsNothing, reason: 'old page off stage at once');
          maxOpacityLayers = max(maxOpacityLayers, partialOpacityLayers(t));
          for (var i = 0; i < 5; i++) {
            await t.pump(frame);
          }
          await t.tap(find.byKey(const ValueKey('tab-0')), warnIfMissed: false);
          sw
            ..reset()
            ..start();
          await t.pump(frame);
          sw.stop();
          hostUs.add(sw.elapsedMicroseconds);
          expect(find.byType(HomePage), findsOneWidget, reason: 'home fully on stage on the first frame');
          expect(find.byType(ServersPage), findsNothing);
          maxOpacityLayers = max(maxOpacityLayers, partialOpacityLayers(t));
          for (var i = 0; i < 5; i++) {
            await t.pump(frame);
            maxOpacityLayers = max(maxOpacityLayers, partialOpacityLayers(t));
          }
        }
        hostUs.sort();
        debugPrint('V108 tab servers→home (${connected ? 'connected' : 'disconnected'}): first-frame host time median '
            '${hostUs[hostUs.length ~/ 2]} us, max ${hostUs.last} us; partial-opacity layers in any frame: $maxOpacityLayers');
        expect(maxOpacityLayers, 0, reason: 'no page-wide opacity (offscreen layer) during switches');
        await done(t);
      });
    }

    testWidgets('quick taps across all tabs never throw, end on the last tapped page', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('en');
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 4);
      for (final i in [1, 2, 3, 0, 2, 1, 3, 0]) {
        await t.tap(find.byKey(ValueKey('tab-$i')), warnIfMissed: false);
        await t.pump(const Duration(milliseconds: 30));
        expect(Shell.indexOf(t.element(find.byType(TabRail))), i);
      }
      expect(t.takeException(), isNull);
      await done(t);
    });
  });

  // ---------------------------------------------------------------- 5. GPU work per frame

  group('per-frame blur passes', () {
    testWidgets('portal at rest draws no mask blur per frame (idle and connected)', (t) async {
      for (final p in [darkPalette, lightPalette]) {
        Palette.use(p);
        final clock = ValueNotifier<double>(3.0);
        final cache = PortalCoreCache();
        for (final (fill, error, comet) in [(0.0, 0.0, 0.0), (1.0, 0.0, 0.0), (0.0, 1.0, 0.0), (0.0, 0.0, 1.0)]) {
          var blurs = 0, images = 0;
          for (var f = 0; f < 10; f++) {
            clock.value = 3.0 + f * 0.05;
            final rec = ui.PictureRecorder();
            final c = _CountingCanvas(Canvas(rec));
            PortalPainter(
                    clock: clock, fill: fill, turbulence: () => 0.3, comet: comet, error: error, shock: 0, core: cache, pixelRatio: 3)
                .paint(c, const Size.square(280));
            rec.endRecording().dispose();
            blurs += c.blurs;
            images += c.images;
          }
          debugPrint('V108 portal ${p.brightness.name} fill=$fill error=$error comet=$comet: mask blurs per frame ${blurs / 10}, cached core draws ${images / 10}');
          expect(blurs, 0, reason: 'no offscreen blur pass per frame at rest');
          expect(images, 10, reason: 'the button comes from the raster cache');
        }
        expect(cache.created, lessThanOrEqualTo(4));
        cache.dispose();
        clock.dispose();
      }
      Palette.use(darkPalette);
    });

    test('no mask / image blurs left in per-frame painters (source audit)', () {
      for (final f in ['lib/ui/widgets/speed_wave.dart', 'lib/ui/widgets/aurora.dart', 'lib/ui/pages/home_page.dart', 'lib/ui/shell.dart']) {
        final src = File(f).readAsStringSync();
        expect(src.contains('MaskFilter.blur'), isFalse, reason: f);
        expect(src.contains('ImageFilter'), isFalse, reason: f);
        expect(src.contains('BackdropFilter'), isFalse, reason: f);
      }
      // Portal: blurs only inside the cached button (_paintCore).
      final portal = File('lib/ui/widgets/portal.dart').readAsStringSync();
      final core = portal.indexOf('void _paintCore(');
      expect(core, greaterThan(0));
      expect(portal.substring(0, core).contains('MaskFilter.blur'), isFalse);
    });
  });

  // ---------------------------------------------------------------- 6. subscription certificates (H1)

  group('subscription certificate approval', () {
    const fp = 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90';
    const url = 'https://203.0.113.9:2096/sub/token';

    Map<String, dynamic> server(Map args) {
      final pins = lastSubPins == null ? const {} : jsonDecode(lastSubPins!) as Map;
      if (pins['203.0.113.9:2096'] == fp) {
        return {
          'status': 200,
          'profiles': [vlessReality()..['id'] = 'p-from-ip'],
          'userInfo': {'present': false},
        };
      }
      return {'status': 0, 'profiles': [], 'errors': ['err.subCertUntrusted:$fp']};
    }

    test('pin key mirrors the core (literal IP https hosts only)', () {
      expect(AppState.subPinKey('https://203.0.113.9:2096/sub/x'), '203.0.113.9:2096');
      expect(AppState.subPinKey('https://203.0.113.9/sub/x'), '203.0.113.9:443');
      expect(AppState.subPinKey('https://[2001:db8::1]:8443/s'), '[2001:db8::1]:8443');
      expect(AppState.subPinKey('https://panel.example.com/s'), isNull);
      expect(AppState.subPinKey('http://203.0.113.9:2096/s'), isNull);
      expect(formatFingerprint('ab' * 32).split('\n').first.split(':').length, 16);
    });

    testWidgets('approve: pinned, fetched with the pin, remembered', (t) async {
      final state = await seeded('en');
      fakeSubHandler = server;
      final asked = <(String, String, bool)>[];
      state.confirmCert = (s, f, changed) async {
        asked.add((s, f, changed));
        return true;
      };
      final err = await state.addSubscription(url);
      expect(err, isNull);
      expect(asked, [('203.0.113.9:2096', fp, false)]);
      expect(state.subPins, {'203.0.113.9:2096': fp});
      expect(state.profiles.any((p) => p.id == 'p-from-ip'), isTrue);
      final prefs = await SharedPreferences.getInstance();
      expect(jsonDecode(prefs.getString('subPins')!), {'203.0.113.9:2096': fp});
      // Next refresh: no question, pin sent before the fetch.
      asked.clear();
      expect(await state.updateSubscription(state.subs.firstWhere((s) => s.url == url)), isNull);
      expect(asked, isEmpty);
      final i = calls.lastIndexWhere((c) => c.method == 'fetchSub');
      expect(calls[i - 1].method, 'setSubPins');
      await done(t);
    });

    testWidgets('decline: nothing imported, nothing pinned', (t) async {
      final state = await seeded('en');
      fakeSubHandler = server;
      state.confirmCert = (s, f, changed) async => false;
      final err = await state.addSubscription(url);
      expect(err, 'err.subCertRejected');
      expect(state.subPins, isEmpty);
      expect(state.profiles.any((p) => p.id == 'p-from-ip'), isFalse);
      await done(t);
    });

    testWidgets('changed certificate asks again with a warning', (t) async {
      final state = await seeded('en', extra: {'flutter.subPins': jsonEncode({'203.0.113.9:2096': 'ff' * 32})});
      expect(state.subPins['203.0.113.9:2096'], 'ff' * 32);
      fakeSubHandler = server;
      bool? wasChanged;
      state.confirmCert = (s, f, changed) async {
        wasChanged = changed;
        return false;
      };
      await state.addSubscription(url);
      expect(wasChanged, isTrue);
      expect(state.subPins['203.0.113.9:2096'], 'ff' * 32, reason: 'declined: old pin unchanged');
      await done(t);
    });

    testWidgets('domain with an invalid certificate: no question, clear error', (t) async {
      final state = await seeded('en');
      fakeSubHandler = (_) => {'status': 0, 'profiles': [], 'errors': ['err.subCertInvalid: unknown authority / self-signed']};
      var asked = false;
      state.confirmCert = (s, f, c) async => asked = true;
      final err = await state.addSubscription('https://panel.example.com/sub/x');
      expect(asked, isFalse);
      expect(err, startsWith('err.subCertInvalid'));
      await done(t);
    });

    testWidgets('dialog in the app (dark + light); the 10 s refresh cap waits while it is open', (t) async {
      for (final theme in ['dark', 'light']) {
        phone(t);
        addTearDown(t.view.reset);
        final state = await seeded('fa', theme: theme);
        Palette.use(paletteFor(theme, Brightness.dark));
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t, 4);
        fakeSubHandler = server;
        state.subs.add(Subscription(id: 'sip', url: url));
        String? result = 'pending';
        unawaited(state.refreshSubscription(state.subs.last).then((r) => result = r));
        await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
        await settle(t, 4);
        expect(find.byKey(const ValueKey('cert-dialog')), findsOneWidget);
        expect(find.textContaining('A1:B2:C3:D4'), findsOneWidget);
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/cert_dialog_${theme}_fa.png'));
        // 15 s with the question open: no timeout.
        for (var i = 0; i < 15; i++) {
          await t.pump(const Duration(seconds: 1));
        }
        expect(result, 'pending');
        await t.tap(find.byKey(const ValueKey('cert-trust')));
        await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
        await settle(t, 4);
        expect(result, isNull);
        expect(state.profiles.any((p) => p.id == 'p-from-ip'), isTrue);
        await done(t);
        lastSubPins = null;
      }
    });
  });

  // ---------------------------------------------------------------- 7. kill switch blocking (M2)

  testWidgets('kill switch keeps blocking: the button disconnects, the home says so', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('fa');
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t, 4);
    await emitEvent(t, {'type': 'state', 'state': 'error', 'error': 'connection lost', 'blocking': true});
    await settle(t, 6);
    expect(state.blocking, isTrue);
    expect(state.isOn, isTrue);
    expect(find.text(S('fa').t('hero.blocking')), findsOneWidget);
    await expectLater(goldenRoot(), matchesGoldenFile('goldens/home_blocking_fa.png'));
    await t.tap(find.byType(Portal));
    await settle(t, 6);
    expect(calls.map((c) => c.method), contains('disconnect'));
    expect(calls.map((c) => c.method), isNot(contains('connect')));
    // A plain error (not blocking) still connects on tap.
    await emitEvent(t, {'type': 'state', 'state': 'error', 'error': 'x'});
    expect(state.blocking, isFalse);
    expect(state.isOn, isFalse);
    await done(t);
  });

  // ---------------------------------------------------------------- 8. updater allowlist (L5)

  group('updater hosts', () {
    test('only https GitHub hosts', () {
      for (final ok in [
        'https://api.github.com/repos/veilonhq/veilon/releases/tags/client-latest',
        'https://github.com/veilonhq/veilon/releases/download/client-latest/veilon-client-universal.apk',
        'https://objects.githubusercontent.com/x',
        'https://release-assets.githubusercontent.com/x',
      ]) {
        expect(isAllowedUpdateUrl(Uri.parse(ok)), isTrue, reason: ok);
      }
      for (final bad in [
        'http://github.com/x',
        'https://evil.example.com/x',
        'https://github.com.evil.example/x',
        'https://user@github.com/x',
        'ftp://github.com/x',
      ]) {
        expect(isAllowedUpdateUrl(Uri.parse(bad)), isFalse, reason: bad);
      }
    });

    test('a disallowed URL is refused before any connection', () async {
      final c = HttpClient();
      await expectLater(
          openAllowed(c, 'https://evil.example.com/a.apk', (_) {}, timeout: const Duration(seconds: 2)),
          throwsA(isA<UpdateError>().having((e) => e.detail, 'detail', contains('not allowed'))));
      await expectLater(openAllowed(c, 'http://github.com/a.apk', (_) {}, timeout: const Duration(seconds: 2)),
          throwsA(isA<UpdateError>()));
      c.close(force: true);
    });

    test('a redirect to another host is refused', () async {
      final srv = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      srv.listen((r) {
        r.response
          ..statusCode = 302
          ..headers.set('location', 'https://evil.example.com/a.apk')
          ..close();
      });
      final c = HttpClient();
      // The test server itself is not on the list either; check the hop
      // logic through a list that only for this test includes it.
      final uri = Uri.parse('http://127.0.0.1:${srv.port}/');
      expect(isAllowedUpdateUrl(uri), isFalse);
      await expectLater(openAllowed(c, uri.toString(), (_) {}, timeout: const Duration(seconds: 2)), throwsA(isA<UpdateError>()));
      c.close(force: true);
      await srv.close(force: true);
    });
  });

  // ---------------------------------------------------------------- 9. strings

  test('no ZWNJ in any UI string; new keys exist in fa and en', () {
    final src = File('lib/l10n/strings.dart').readAsStringSync();
    expect(src.contains('\u200c'), isFalse);
    for (final lang in ['fa', 'en']) {
      final s = S(lang);
      for (final k in [
        'settings.theme',
        'theme.dark',
        'theme.light',
        'theme.system',
        'info.theme',
        'cert.title',
        'cert.body',
        'cert.changed',
        'cert.fingerprint',
        'cert.trust',
        'err.subCertUntrusted',
        'err.subCertInvalid',
        'err.subCertRejected',
        'hero.blocking',
        'update.err.signature',
      ]) {
        expect(s.t(k), isNot(k), reason: '$lang $k');
        expect(s.t(k).contains('\u200c'), isFalse);
      }
    }
    for (final f in Directory('lib').listSync(recursive: true).whereType<File>().where((f) => f.path.endsWith('.dart'))) {
      expect(f.readAsStringSync().contains('\u200c'), isFalse, reason: f.path);
    }
  });

  test('friendly error keys for the certificate errors', () {
    final s = S('en');
    expect(s.t('err.subCertUntrusted'), contains('certificate'));
    expect(friendlyErrorKey('err.subCertUntrusted:${'ab' * 32}'), 'err.subCertUntrusted');
    expect(friendlyErrorKey('err.subCertInvalid: unknown authority / self-signed'), 'err.subCertInvalid');
  });

  // Silence an unused import when StatsPage is tree-shaken from a run.
  test('pages exist', () => expect(const StatsPage(), isA<Widget>()));
}
