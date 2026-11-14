// v1.0.10: QR scanner redesign (Flutter overlay over the native camera view),
// pick-from-gallery decode, and system-bar insets (nothing behind the status
// bar or the navigation / gesture bar, 320 dp included).
// Goldens: test/goldens/qr_*.png. Native decoding itself is tested
// off-device in android/kt-tests/QrDecodeTest.kt.
import 'dart:convert';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/qr_scan_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const link = 'vless://00000000-0000-4000-8000-000000000000@server.example.com:443?security=reality#QR';

/// Status bar 24 dp and a 3-button navigation bar 48 dp (the case where the
/// old hint was cut off), edge to edge.
const insets = EdgeInsets.only(top: 24, bottom: 48);

void phone(WidgetTester t, {double width = 360, double height = 780, EdgeInsets pad = insets}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
  t.view.padding = FakeViewPadding(top: pad.top * 3, bottom: pad.bottom * 3, left: pad.left * 3, right: pad.right * 3);
  t.view.viewPadding = FakeViewPadding(top: pad.top * 3, bottom: pad.bottom * 3, left: pad.left * 3, right: pad.right * 3);
}

/// A stand-in camera picture: a dim room with a lit card holding a QR-like
/// pattern, so the goldens show how the veil and the window look over video.
class FakeCamera extends StatelessWidget {
  const FakeCamera({super.key});
  @override
  Widget build(BuildContext context) => const CustomPaint(painter: _FakeCameraPainter(), size: Size.infinite);
}

class _FakeCameraPainter extends CustomPainter {
  const _FakeCameraPainter();
  @override
  void paint(Canvas c, Size s) {
    final r = Offset.zero & s;
    c.drawRect(
        r,
        Paint()
          ..shader = const LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color(0xFF3B3430), Color(0xFF6E6258), Color(0xFF2A2522)],
          ).createShader(r));
    c.drawCircle(Offset(s.width * 0.8, s.height * 0.15), s.width * 0.5,
        Paint()..shader = RadialGradient(colors: [const Color(0x55FFE9C4), const Color(0x00FFE9C4)]).createShader(Rect.fromCircle(center: Offset(s.width * 0.8, s.height * 0.15), radius: s.width * 0.5)));
    // Card with a QR-like pattern near the centre, slightly turned.
    final side = min(s.width * 0.6, 240.0);
    c.save();
    c.translate(s.width / 2, s.height / 2);
    c.rotate(-0.06);
    final card = Rect.fromCenter(center: Offset.zero, width: side + 28, height: side + 28);
    c.drawRRect(RRect.fromRectAndRadius(card.shift(const Offset(6, 10)), const Radius.circular(10)), Paint()..color = const Color(0x55000000));
    c.drawRRect(RRect.fromRectAndRadius(card, const Radius.circular(10)), Paint()..color = const Color(0xFFF1EEE8));
    const n = 25;
    final m = side / n;
    final rnd = Random(11);
    final dark = Paint()..color = const Color(0xFF1C1C1C);
    bool finder(int x, int y) => (x < 7 && y < 7) || (x >= n - 7 && y < 7) || (x < 7 && y >= n - 7);
    for (var y = 0; y < n; y++) {
      for (var x = 0; x < n; x++) {
        bool on;
        if (finder(x, y)) {
          final fx = x >= n - 7 ? x - (n - 7) : x;
          final fy = y >= n - 7 ? y - (n - 7) : y;
          on = fx == 0 || fy == 0 || fx == 6 || fy == 6 || (fx >= 2 && fx <= 4 && fy >= 2 && fy <= 4);
        } else {
          on = rnd.nextDouble() < 0.48;
        }
        if (on) c.drawRect(Rect.fromLTWH(-side / 2 + x * m, -side / 2 + y * m, m + 0.3, m + 0.3), dark);
      }
    }
    c.restore();
  }

  @override
  bool shouldRepaint(_FakeCameraPainter old) => false;
}

/// The scanner page alone in the app's MaterialApp + theme (fa/en, dark/light).
Future<AppState> pumpScanner(WidgetTester t, String lang, {String theme = 'dark'}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.settings': jsonEncode({'language': lang, 'theme': theme}),
  });
  final state = AppState();
  await state.init();
  await t.pumpWidget(VeilonApp(state: state));
  await settle(t);
  final ctx = t.element(find.byType(Shell));
  QrScanPage.open(ctx).then((v) => popped.add(v));
  await settle(t);
  return state;
}

final popped = <String?>[];

Future<void> settle(WidgetTester t, [int frames = 10]) async {
  for (var i = 0; i < frames; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

Future<void> done(WidgetTester t, AppState s) async {
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 1));
  s.dispose();
  Palette.use(darkPalette);
}

/// Sends a native → Dart call on the scanner channel ("scanned", "cameraError").
Future<void> nativeQr(WidgetTester t, String method, Object? arg) async {
  final data = const StandardMethodCodec().encodeMethodCall(MethodCall(method, arg));
  await TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage(QrScanPage.channel.name, data, (_) {});
}

/// Everything the user must see lies inside the safe area (no clipping by
/// the status bar, the navigation bar or the screen edge) and the hint text
/// is shown whole.
void expectNothingClipped(WidgetTester t, String hint, {required double w, required double h, EdgeInsets pad = insets}) {
  final safe = Rect.fromLTRB(pad.left, pad.top, w - pad.right, h - pad.bottom);
  for (final f in [find.byKey(const ValueKey('qr-close')), find.byKey(const ValueKey('qr-gallery')), find.text(hint)]) {
    final r = t.getRect(f);
    expect(safe.contains(r.topLeft) && safe.contains(r.bottomRight - const Offset(0.01, 0.01)), isTrue, reason: '$f at $r outside $safe');
  }
  final p = t.renderObject<RenderParagraph>(find.text(hint));
  expect(p.didExceedMaxLines, isFalse, reason: 'hint cut: $hint');
  // Hint above the gallery button, frame above the hint.
  final hintR = t.getRect(find.text(hint));
  final btn = t.getRect(find.byKey(const ValueKey('qr-gallery')));
  expect(hintR.bottom, lessThanOrEqualTo(btn.top));
}

void main() {
  setUpAll(() async {
    await loadAppFonts();
  });

  setUp(() {
    installNativeMocks();
    calls.clear();
    popped.clear();
    fakeCamera = ['granted'];
    fakeHasTorch = true;
    fakeTorchResult = true;
    fakeQrPick = null;
    QrScanPage.debugPreview = (_) => const FakeCamera();
  });

  group('look (goldens)', () {
    for (final (w, h) in [(360.0, 780.0), (320.0, 640.0)]) {
      for (final lang in ['fa', 'en']) {
        for (final theme in ['dark', 'light']) {
          testWidgets('scanner $lang $theme ${w.toInt()}dp', (t) async {
            phone(t, width: w, height: h);
            addTearDown(t.view.reset);
            final st = await pumpScanner(t, lang, theme: theme);
            final hint = S(lang).t('qr.hint');
            expect(find.text(hint), findsOneWidget);
            expect(find.text(S(lang).t('qr.gallery')), findsOneWidget);
            expect(find.byKey(const ValueKey('qr-torch')), findsOneWidget);
            expectNothingClipped(t, hint, w: w, h: h);
            await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_${lang}_${theme}_${w.toInt()}.png'));
            await done(t, st);
          });
        }
      }
    }

    testWidgets('camera permission denied, then blocked (fa dark, en light 320)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeCamera = ['denied'];
      var st = await pumpScanner(t, 'fa');
      expect(find.text(S('fa').t('qr.noCamera')), findsOneWidget);
      expect(find.text(S('fa').t('qr.allow')), findsOneWidget);
      expect(find.byType(FakeCamera), findsNothing);
      expect(find.byKey(const ValueKey('qr-torch')), findsNothing);
      expectNothingClipped(t, S('fa').t('qr.noCameraHint'), w: 360, h: 780);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_denied_fa_dark_360.png'));
      await done(t, st);

      phone(t, width: 320, height: 640);
      fakeCamera = ['blocked'];
      st = await pumpScanner(t, 'en', theme: 'light');
      expect(find.text(S('en').t('qr.openSettings')), findsOneWidget);
      expectNothingClipped(t, S('en').t('qr.noCameraHint'), w: 320, h: 640);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_blocked_en_light_320.png'));
      await done(t, st);
    });

    testWidgets('no QR in the picked image: localized error (fa dark 320, en light 360), torch on', (t) async {
      phone(t, width: 320, height: 640);
      addTearDown(t.view.reset);
      fakeQrPick = {'error': 'noQr'};
      var st = await pumpScanner(t, 'fa');
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t, 4);
      final msg = S('fa').t('qr.noQr');
      expect(find.text(msg), findsOneWidget);
      expect(popped, isEmpty);
      expectNothingClipped(t, msg, w: 320, h: 640);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_noqr_fa_dark_320.png'));
      await done(t, st);

      phone(t);
      st = await pumpScanner(t, 'en', theme: 'light');
      await t.tap(find.byKey(const ValueKey('qr-torch')));
      await settle(t, 3);
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t, 4);
      expect(find.text(S('en').t('qr.noQr')), findsOneWidget);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_noqr_torch_en_light_360.png'));
      await done(t, st);
    });
  });

  group('behaviour', () {
    testWidgets('gallery QR found → page returns the text like a camera scan', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrPick = {'text': '  $link\n'};
      final st = await pumpScanner(t, 'en');
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t);
      expect(popped, [link]);
      expect(find.byType(QrScanPage), findsNothing);
      expect(calls.where((c) => c.method == 'pickQrImage'), hasLength(1));
      await done(t, st);
    });

    testWidgets('gallery: unreadable image, cancel, then found (fa)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrPick = {'error': 'unreadable'};
      final st = await pumpScanner(t, 'fa');
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t, 3);
      expect(find.text(S('fa').t('qr.unreadable')), findsOneWidget);
      // The error goes away by itself.
      await t.pump(const Duration(seconds: 5));
      await settle(t, 3);
      expect(find.text(S('fa').t('qr.unreadable')), findsNothing);
      expect(find.text(S('fa').t('qr.hint')), findsOneWidget);
      fakeQrPick = null; // cancelled picker: nothing happens
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t, 3);
      expect(find.byType(QrScanPage), findsOneWidget);
      expect(find.text(S('fa').t('qr.noQr')), findsNothing);
      expect(popped, isEmpty);
      fakeQrPick = {'text': link};
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t);
      expect(popped, [link]);
      await done(t, st);
    });

    testWidgets('camera scan from native → returned once; close returns null', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      var st = await pumpScanner(t, 'en');
      await nativeQr(t, 'scanned', link);
      await nativeQr(t, 'scanned', 'second');
      await settle(t);
      expect(popped, [link]);
      await done(t, st);
      popped.clear();
      st = await pumpScanner(t, 'fa');
      await t.tap(find.byKey(const ValueKey('qr-close')));
      await settle(t);
      expect(popped, [null]);
      await done(t, st);
    });

    testWidgets('camera error from native → message, gallery still works', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'fa');
      await nativeQr(t, 'cameraError', 'Fail to connect to camera service');
      await settle(t, 3);
      expect(find.text(S('fa').t('qr.cameraFailedTitle')), findsOneWidget);
      expect(find.text(S('fa').t('qr.cameraFailed')), findsOneWidget);
      expect(find.byType(FakeCamera), findsNothing);
      fakeQrPick = {'text': link};
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t);
      expect(popped, [link]);
      await done(t, st);
    });

    testWidgets('permission: denied → Allow asks again → camera shown; blocked → settings, recheck on resume', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeCamera = ['denied', 'granted'];
      var st = await pumpScanner(t, 'en');
      expect(find.byType(FakeCamera), findsNothing);
      await t.tap(find.byKey(const ValueKey('qr-camera-action')));
      await settle(t, 3);
      expect(find.byType(FakeCamera), findsOneWidget);
      final perm = calls.where((c) => c.method == 'cameraPermission').map((c) => (c.arguments as Map)['request']).toList();
      expect(perm, [true, true]);
      await done(t, st);

      calls.clear();
      fakeCamera = ['blocked', 'granted'];
      st = await pumpScanner(t, 'en');
      await t.tap(find.byKey(const ValueKey('qr-camera-action')));
      await settle(t, 2);
      expect(calls.where((c) => c.method == 'openAppSettings'), hasLength(1));
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await settle(t, 3);
      // first check asked (request: true); the resume recheck does not ask.
      final req = calls.where((c) => c.method == 'cameraPermission').map((c) => (c.arguments as Map)['request']).toList();
      expect(req.first, isTrue);
      expect(req.last, isFalse);
      expect(find.byType(FakeCamera), findsOneWidget);
      await done(t, st);
    });

    testWidgets('torch toggles, hidden without a flash, turned off on close', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      var st = await pumpScanner(t, 'en');
      await t.tap(find.byKey(const ValueKey('qr-torch')));
      await settle(t, 2);
      await t.tap(find.byKey(const ValueKey('qr-close')));
      await settle(t);
      final torch = calls.where((c) => c.method == 'qrTorch').map((c) => (c.arguments as Map)['on']).toList();
      expect(torch, [true, false]);
      await done(t, st);
      fakeHasTorch = false;
      st = await pumpScanner(t, 'en');
      expect(find.byKey(const ValueKey('qr-torch')), findsNothing);
      await done(t, st);
    });

    testWidgets('add sheet: scan → import exactly like before (camera and gallery)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      SharedPreferences.setMockInitialValues({'flutter.settings': jsonEncode({'language': 'en'})});
      final st = AppState();
      await st.init();
      await t.pumpWidget(VeilonApp(state: st));
      await settle(t);
      Shell.goTo(t.element(find.byType(TabRail)), 1);
      await settle(t);
      for (final viaGallery in [false, true]) {
        calls.clear();
        showAddSheet(t.element(find.byType(ServersPage)));
        await settle(t);
        await t.tap(find.text(S('en').t('add.qr')));
        await settle(t);
        expect(find.byType(QrScanPage), findsOneWidget);
        if (viaGallery) {
          fakeQrPick = {'text': link};
          await t.tap(find.byKey(const ValueKey('qr-gallery')));
        } else {
          await nativeQr(t, 'scanned', link);
        }
        await settle(t);
        expect(find.byType(QrScanPage), findsNothing);
        final parsed = calls.where((c) => c.method == 'parseText').map((c) => (c.arguments as Map)['text']).toList();
        expect(parsed, [link], reason: viaGallery ? 'gallery' : 'camera');
        await t.tapAt(const Offset(10, 60)); // close the sheet
        await settle(t);
      }
      await done(t, st);
    });

    testWidgets('real preview = hybrid-composition platform view of the native scanner', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      QrScanPage.debugPreview = null;
      final created = <Map>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform_views, (c) async {
        if (c.method == 'create') created.add(c.arguments as Map);
        return null;
      });
      addTearDown(() => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform_views, null));
      final st = await pumpScanner(t, 'en');
      expect(find.byType(PlatformViewLink), findsOneWidget);
      expect(created, isNotEmpty);
      expect(created.first['viewType'], QrScanPage.viewType);
      expect(created.first['hybrid'], isTrue);
      await done(t, st);
    });
  });

  group('geometry', () {
    test('frame, hint and button stay inside the safe area on every screen', () {
      final sizes = [const Size(320, 568), const Size(320, 640), const Size(360, 640), const Size(360, 780), const Size(411, 914), const Size(800, 1280), const Size(780, 360), const Size(640, 320)];
      final pads = [EdgeInsets.zero, insets, const EdgeInsets.only(top: 48, bottom: 24), const EdgeInsets.only(top: 24, bottom: 48, left: 48)];
      for (final s in sizes) {
        for (final p in pads) {
          for (final ts in [1.0, 1.3]) {
            final g = QrGeometry.of(s, p, textScale: ts);
            final why = '$s $p x$ts → ${g.cutout} hint ${g.hintHeight}';
            expect(g.cutout.top, greaterThanOrEqualTo(p.top + QrGeometry.barHeight), reason: why);
            expect(g.cutout.left, greaterThanOrEqualTo(p.left), reason: why);
            expect(g.cutout.right, lessThanOrEqualTo(s.width - p.right), reason: why);
            expect(g.buttonTop + QrGeometry.buttonHeight, lessThanOrEqualTo(s.height - p.bottom - QrGeometry.bottomGap + 0.001), reason: why);
            final below = g.showHint ? g.cutout.bottom + QrGeometry.hintGap + g.hintHeight : g.cutout.bottom;
            expect(below, lessThanOrEqualTo(g.buttonTop + 0.001), reason: why);
            expect(g.cutout.width, greaterThanOrEqualTo(80), reason: why);
            if (s.height >= 560) expect(g.showHint, isTrue, reason: why);
          }
        }
      }
      // Portrait phones: frame centred on the screen (decoder looks at the centre).
      final g = QrGeometry.of(const Size(360, 780), insets);
      expect(g.cutout.center.dx, closeTo(180, 0.01));
      expect(g.cutout.center.dy, closeTo(390, 0.01));
      expect(g.cutout.width, closeTo(360 * 0.74, 0.01));
    });

    test('scanner strings: fa and en, no ZWNJ', () {
      const keys = ['qr.hint', 'qr.gallery', 'qr.reading', 'qr.noQr', 'qr.unreadable', 'qr.noCamera', 'qr.noCameraHint', 'qr.cameraFailedTitle', 'qr.cameraFailed', 'qr.allow', 'qr.openSettings', 'qr.close', 'qr.torchOn', 'qr.torchOff'];
      for (final l in ['fa', 'en']) {
        for (final k in keys) {
          final v = S(l).t(k);
          expect(v, isNot(k), reason: '$l $k missing');
          expect(v.contains('\u200c'), isFalse, reason: '$l $k has ZWNJ');
        }
      }
      for (final k in keys) {
        expect(S('fa').t(k), isNot(S('en').t(k)), reason: '$k not translated');
      }
    });
  });
}
