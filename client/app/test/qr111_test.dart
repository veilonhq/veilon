// v1.0.11: gallery pick → adjust screen (pinch-zoom / drag the picked image so
// the QR sits in the frame, "Scan this area" decodes only that area natively,
// then the whole image). Goldens: test/goldens/qr_adjust_*.png. The native
// side (crop rect → EXIF raw rect, region decode, upscaling, the 1.0.10
// bounds-pass regression) is tested off-device in android/kt-tests/QrCropTest.kt
// and QrImageLoadTest.kt.
import 'dart:convert';
import 'dart:io';

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
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const link = 'vless://00000000-0000-4000-8000-000000000000@server.example.com:443?security=reality#QR';
const insets = EdgeInsets.only(top: 24, bottom: 48);

const picked = <String, Object?>{
  'path': '/data/user/0/app.veilon/cache/qr-pick/pick-1.img',
  'preview': '/data/user/0/app.veilon/cache/qr-pick/pick-1.img.preview.jpg',
  'width': 600,
  'height': 800,
};

late MemoryImage photo;
final popped = <String?>[];

void phone(WidgetTester t, {double width = 360, double height = 780, EdgeInsets pad = insets}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
  t.view.padding = FakeViewPadding(top: pad.top * 3, bottom: pad.bottom * 3, left: pad.left * 3, right: pad.right * 3);
  t.view.viewPadding = FakeViewPadding(top: pad.top * 3, bottom: pad.bottom * 3, left: pad.left * 3, right: pad.right * 3);
}

Future<void> settle(WidgetTester t, [int frames = 10]) async {
  for (var i = 0; i < frames; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

class _Cam extends StatelessWidget {
  const _Cam();
  @override
  Widget build(BuildContext context) => const ColoredBox(color: Color(0xFF4A423C));
}

Future<AppState> pumpScanner(WidgetTester t, String lang, {String theme = 'dark'}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.settings': jsonEncode({'language': lang, 'theme': theme}),
  });
  final state = AppState();
  await state.init();
  await t.pumpWidget(VeilonApp(state: state));
  await settle(t);
  final ctx = t.element(find.byType(Shell));
  await t.runAsync(() => precacheImage(photo, ctx));
  QrScanPage.open(ctx).then((v) => popped.add(v));
  await settle(t);
  return state;
}

Future<void> done(WidgetTester t, AppState s) async {
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 1));
  s.dispose();
  Palette.use(darkPalette);
}

Future<void> openAdjust(WidgetTester t) async {
  fakeQrPick = picked;
  await t.tap(find.byKey(const ValueKey('qr-gallery')));
  await settle(t);
  expect(find.byType(QrAdjustPage), findsOneWidget);
}

QrAdjustPageState adjustState(WidgetTester t) => t.state<QrAdjustPageState>(find.byType(QrAdjustPage));

List<Map> areaCalls() => calls.where((c) => c.method == 'qrDecodeArea').map((c) => c.arguments as Map).toList();

void expectInsideSafe(WidgetTester t, String hint, {required double w, required double h, EdgeInsets pad = insets}) {
  final safe = Rect.fromLTRB(pad.left, pad.top, w - pad.right, h - pad.bottom);
  for (final f in [
    find.byKey(const ValueKey('qr-adjust-back')),
    find.byKey(const ValueKey('qr-scan-area')),
    find.byKey(const ValueKey('qr-pick-another')),
    find.text(hint),
  ]) {
    final r = t.getRect(f);
    expect(safe.contains(r.topLeft) && safe.contains(r.bottomRight - const Offset(0.01, 0.01)), isTrue, reason: '$f at $r outside $safe');
  }
  expect(t.renderObject<RenderParagraph>(find.text(hint)).didExceedMaxLines, isFalse, reason: 'hint cut: $hint');
  expect(t.getRect(find.text(hint)).bottom, lessThanOrEqualTo(t.getRect(find.byKey(const ValueKey('qr-scan-area'))).top));
  // Button labels shown whole.
  for (final p in t.renderObjectList<RenderParagraph>(find.descendant(of: find.byKey(const ValueKey('qr-scan-area')), matching: find.byType(RichText)))) {
    expect(p.didExceedMaxLines, isFalse);
  }
}

void main() {
  setUpAll(() async {
    await loadAppFonts();
    photo = MemoryImage(File('test/fixtures/qr_photo.png').readAsBytesSync());
  });

  setUp(() {
    installNativeMocks();
    calls.clear();
    popped.clear();
    fakeCamera = ['granted'];
    fakeHasTorch = true;
    fakeTorchResult = true;
    fakeQrPick = null;
    fakeQrArea = [
      {'error': 'noQr'}
    ];
    QrScanPage.debugPreview = (_) => const _Cam();
    QrAdjustPage.debugImage = (_) => photo;
  });

  group('look (goldens)', () {
    for (final (w, h) in [(360.0, 780.0), (320.0, 640.0)]) {
      for (final lang in ['fa', 'en']) {
        for (final theme in ['dark', 'light']) {
          testWidgets('adjust $lang $theme ${w.toInt()}dp', (t) async {
            phone(t, width: w, height: h);
            addTearDown(t.view.reset);
            final st = await pumpScanner(t, lang, theme: theme);
            await openAdjust(t);
            final hint = S(lang).t('qr.adjustHint');
            expect(find.text(hint), findsOneWidget);
            expect(find.text(S(lang).t('qr.scanArea')), findsOneWidget);
            expect(find.text(S(lang).t('qr.adjustTitle')), findsOneWidget);
            expect(t.widget<RawImage>(find.byType(RawImage)).image, isNotNull, reason: 'preview painted');
            expectInsideSafe(t, hint, w: w, h: h);
            await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_adjust_${lang}_${theme}_${w.toInt()}.png'));
            await done(t, st);
          });
        }
      }
    }

    testWidgets('no QR in the framed area or the whole image: error in the pill (fa dark 320), zoomed in', (t) async {
      phone(t, width: 320, height: 640);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'fa');
      await openAdjust(t);
      final a = adjustState(t);
      // zoomed on the card at the lower right of the photo
      a.controller.value = Matrix4.identity()
        ..translateByDouble(-375, -680, 0, 1)
        ..scaleByDouble(2.2, 2.2, 1, 1);
      await settle(t, 2);
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t, 4);
      final msg = S('fa').t('qr.noQr');
      expect(find.text(msg), findsOneWidget);
      expect(find.byType(QrAdjustPage), findsOneWidget);
      expectInsideSafe(t, msg, w: 320, h: 640);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/qr_adjust_noqr_fa_dark_320.png'));
      await done(t, st);
    });
  });

  group('behaviour', () {
    testWidgets('pick opens the adjust screen with nothing decoded; Scan sends the framed area (whole fallback on)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrArea = [
        {'text': ' $link '}
      ];
      final st = await pumpScanner(t, 'en');
      await openAdjust(t);
      expect(areaCalls(), isEmpty, reason: 'no automatic decode after picking');
      expect(popped, isEmpty);
      // fitted: 600×800 in 360×780 → 360×480 at y 150
      final img = t.getRect(find.byType(RawImage));
      expect(img.left, closeTo(0, 0.01));
      expect(img.top, closeTo(150, 0.01));
      expect(img.width, closeTo(360, 0.01));
      final want = adjustState(t).currentArea()!;
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t);
      final a = areaCalls().single;
      expect(a['path'], picked['path']);
      expect(a['whole'], isTrue);
      expect([a['left'], a['top'], a['width'], a['height']], [want.left, want.top, want.width, want.height]);
      // frame 266.4 dp square at the centre → 444 px of the 600 px wide image
      expect(want.width, inInclusiveRange(443, 445));
      expect(popped, [link]);
      expect(find.byType(QrAdjustPage), findsNothing);
      expect(find.byType(QrScanPage), findsNothing);
      expect(calls.where((c) => c.method == 'qrDiscard').map((c) => (c.arguments as Map)['path']), [picked['path']]);
      await done(t, st);
    });

    testWidgets('not found → error, stays; drag + pinch-zoom → retry sends a new rect → found → returned once', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrArea = [
        {'error': 'noQr'},
        {'text': link},
      ];
      final st = await pumpScanner(t, 'fa');
      await openAdjust(t);
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t, 3);
      expect(find.text(S('fa').t('qr.noQr')), findsOneWidget);
      expect(find.byType(QrAdjustPage), findsOneWidget);
      expect(popped, isEmpty);
      final first = adjustState(t).currentArea()!;

      // drag the photo up-left (brings its lower right into the frame)
      await t.dragFrom(const Offset(180, 600), const Offset(-60, -140));
      await settle(t, 3);
      // pinch: two fingers move apart around the frame centre
      final c = adjustState(t).frame.center;
      final g1 = await t.startGesture(c - const Offset(40, 0), pointer: 1);
      final g2 = await t.startGesture(c + const Offset(40, 0), pointer: 2);
      await t.pump(const Duration(milliseconds: 50));
      for (var i = 1; i <= 6; i++) {
        await g1.moveTo(c - Offset(40.0 + i * 12, 0));
        await g2.moveTo(c + Offset(40.0 + i * 12, 0));
        await t.pump(const Duration(milliseconds: 30));
      }
      await g1.up();
      await g2.up();
      await settle(t, 6);
      final st2 = adjustState(t);
      expect(st2.controller.value.getMaxScaleOnAxis(), greaterThan(1.3));
      final second = st2.currentArea()!;
      expect(second.width, lessThan(first.width), reason: 'zoomed in → smaller area of the image');
      expect(second.left, greaterThan(first.left));
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t);
      final calls2 = areaCalls();
      expect(calls2, hasLength(2));
      expect([calls2[1]['left'], calls2[1]['top'], calls2[1]['width'], calls2[1]['height']], [second.left, second.top, second.width, second.height]);
      expect(popped, [link]);
      await done(t, st);
    });

    testWidgets('back → camera again, copy discarded, nothing imported', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'en');
      await openAdjust(t);
      expect(find.byType(_Cam), findsNothing, reason: 'camera dropped while adjusting');
      await t.tap(find.byKey(const ValueKey('qr-adjust-back')));
      await settle(t);
      expect(find.byType(QrAdjustPage), findsNothing);
      expect(find.byType(QrScanPage), findsOneWidget);
      expect(find.byType(_Cam), findsOneWidget, reason: 'camera back');
      expect(popped, isEmpty);
      expect(areaCalls(), isEmpty);
      expect(calls.where((c) => c.method == 'qrDiscard'), hasLength(1));
      // the gallery works again
      fakeQrArea = [
        {'text': link}
      ];
      await openAdjust(t);
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t);
      expect(popped, [link]);
      await done(t, st);
    });

    testWidgets('pick another image → picker again → new adjust screen; cancelled picker → camera', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'en');
      await openAdjust(t);
      fakeQrPick = {...picked, 'path': '/data/user/0/app.veilon/cache/qr-pick/pick-2.img'};
      await t.tap(find.byKey(const ValueKey('qr-pick-another')));
      await settle(t);
      expect(calls.where((c) => c.method == 'pickQrImage'), hasLength(2));
      expect(find.byType(QrAdjustPage), findsOneWidget);
      expect(adjustState(t).widget.picked.path, endsWith('pick-2.img'));
      fakeQrPick = null; // picker cancelled
      await t.tap(find.byKey(const ValueKey('qr-pick-another')));
      await settle(t);
      expect(find.byType(QrAdjustPage), findsNothing);
      expect(find.byType(QrScanPage), findsOneWidget);
      final discarded = calls.where((c) => c.method == 'qrDiscard').map((c) => (c.arguments as Map)['path'] as String).toList();
      expect(discarded, [endsWith('pick-1.img'), endsWith('pick-2.img')]);
      expect(popped, isEmpty);
      await done(t, st);
    });

    testWidgets('unreadable image at scan time → «این تصویر باز نشد», stays', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrArea = [
        {'error': 'unreadable'}
      ];
      final st = await pumpScanner(t, 'fa');
      await openAdjust(t);
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t, 3);
      expect(find.text('این تصویر باز نشد'), findsOneWidget);
      expect(find.byType(QrAdjustPage), findsOneWidget);
      // the error goes away, the hint comes back
      await t.pump(const Duration(seconds: 5));
      await settle(t, 3);
      expect(find.text(S('fa').t('qr.adjustHint')), findsOneWidget);
      await done(t, st);
    });

    testWidgets('frame moved off the image → only the whole image is tried (empty rect)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'en');
      await openAdjust(t);
      final a = adjustState(t);
      a.controller.value = Matrix4.identity()..translateByDouble(0, 700, 0, 1);
      await settle(t, 2);
      expect(a.currentArea(), isNull);
      await t.tap(find.byKey(const ValueKey('qr-scan-area')));
      await settle(t, 3);
      final c = areaCalls().single;
      expect([c['width'], c['height'], c['whole']], [0, 0, true]);
      expect(find.text(S('en').t('qr.noQr')), findsOneWidget);
      await done(t, st);
    });

    testWidgets('a camera result while adjusting is ignored', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final st = await pumpScanner(t, 'en');
      await openAdjust(t);
      final data = const StandardMethodCodec().encodeMethodCall(const MethodCall('scanned', 'camera-text'));
      await TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.handlePlatformMessage(QrScanPage.channel.name, data, (_) {});
      await settle(t);
      expect(find.byType(QrAdjustPage), findsOneWidget);
      expect(popped, isEmpty);
      await done(t, st);
    });

    testWidgets('picker reply that cannot be opened → error on the scanner, no adjust screen', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      fakeQrPick = {'error': 'unreadable'};
      final st = await pumpScanner(t, 'en');
      await t.tap(find.byKey(const ValueKey('qr-gallery')));
      await settle(t, 3);
      expect(find.byType(QrAdjustPage), findsNothing);
      expect(find.text(S('en').t('qr.unreadable')), findsOneWidget);
      await done(t, st);
    });
  });

  group('crop math', () {
    const vp = Size(360, 780);
    const img = Size(600, 800);
    final frame = Rect.fromCenter(center: const Offset(180, 390), width: 266.4, height: 266.4);

    test('fitted: contain, centred', () {
      expect(QrCropMath.fitted(vp, img), const Rect.fromLTWH(0, 150, 360, 480));
      final land = QrCropMath.fitted(vp, const Size(4000, 3000));
      expect(land.width, closeTo(360, 1e-9));
      expect(land.height, closeTo(270, 1e-9));
      expect(land.top, closeTo(255, 1e-9));
    });

    test('zoom 1: frame → image pixels', () {
      final r = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: img)!;
      const k = 600 / 360;
      expect(r.left, closeTo(frame.left * k, 1e-6));
      expect(r.top, closeTo((frame.top - 150) * k, 1e-6));
      expect(r.width, closeTo(266.4 * k, 1e-6));
    });

    test('zoom 2× about the frame centre → half the area, same centre', () {
      final c = frame.center;
      final m = Matrix4.identity()
        ..translateByDouble(c.dx, c.dy, 0, 1)
        ..scaleByDouble(2, 2, 1, 1)
        ..translateByDouble(-c.dx, -c.dy, 0, 1);
      final r1 = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: img)!;
      final r2 = QrCropMath.imageRect(viewport: vp, frame: frame, transform: m, image: img)!;
      expect(r2.width, closeTo(r1.width / 2, 1e-6));
      expect(r2.center.dx, closeTo(r1.center.dx, 1e-6));
      expect(r2.center.dy, closeTo(r1.center.dy, 1e-6));
    });

    test('pan: moving the image left/up by d shows pixels d·k further right/down', () {
      final m = Matrix4.identity()..translateByDouble(-30, -60, 0, 1);
      final r1 = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: img)!;
      final r2 = QrCropMath.imageRect(viewport: vp, frame: frame, transform: m, image: img)!;
      expect(r2.left - r1.left, closeTo(30 * 600 / 360, 1e-6));
      expect(r2.top - r1.top, closeTo(60 * 600 / 360, 1e-6));
    });

    test('zoom 3× + pan, compared with the inverse mapping of the image corners', () {
      final m = Matrix4.identity()
        ..translateByDouble(-500, -700, 0, 1)
        ..scaleByDouble(3, 3, 1, 1);
      final r = QrCropMath.imageRect(viewport: vp, frame: frame, transform: m, image: img)!;
      // screen x = 3·(0 + px/k) − 500 → px = (x + 500)/3 · k (image starts at x 0, y 150)
      const k = 600 / 360;
      expect(r.left, closeTo((frame.left + 500) / 3 * k, 1e-6));
      expect(r.top, closeTo(((frame.top + 700) / 3 - 150) * k, 1e-6));
      expect(r.width, closeTo(266.4 / 3 * k, 1e-6));
    });

    test('clamped at the image edge; off the image → null', () {
      final m = Matrix4.identity()..translateByDouble(250, 0, 0, 1); // image moved right
      final r = QrCropMath.imageRect(viewport: vp, frame: frame, transform: m, image: img)!;
      expect(r.left, 0);
      expect(r.width, closeTo((frame.right - 250) * 600 / 360, 1e-6));
      final off = Matrix4.identity()..translateByDouble(0, 700, 0, 1);
      expect(QrCropMath.imageRect(viewport: vp, frame: frame, transform: off, image: img), isNull);
      expect(QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.zero(), image: img), isNull);
    });

    test('pixels: outward rounding inside the image', () {
      final p = QrCropMath.pixels(const Rect.fromLTRB(10.2, 20.9, 100.1, 599.99), img);
      expect([p.left, p.top, p.width, p.height], [10, 20, 91, 580]);
      final q = QrCropMath.pixels(const Rect.fromLTRB(-3, -1, 700, 900), img);
      expect([q.left, q.top, q.width, q.height], [0, 0, 600, 800]);
    });

    test('EXIF-rotated photo: the rect is in the upright size the native side reports', () {
      // A phone photo stored 4000×3000 with EXIF orientation 6 (rotate 90°)
      // is shown upright, and pickQrImage reports the upright 3000×4000; the
      // preview is drawn upright too, so the crop rect is upright pixels.
      // QrCrop.orientedToRaw (kt-tests/QrCropTest.kt) maps it to the file.
      final p = QrPicked.fromMap({'path': '/c/qr-pick/pick-9.img', 'preview': '/c/p.jpg', 'width': 3000, 'height': 4000})!;
      expect(p.size, const Size(3000, 4000));
      final r = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: p.size)!;
      // portrait 3:4 fills the width like the 600×800 fixture: same relative rect
      final r0 = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: img)!;
      expect(r.left / 3000, closeTo(r0.left / 600, 1e-9));
      expect(r.top / 4000, closeTo(r0.top / 800, 1e-9));
      expect(r.right, lessThanOrEqualTo(3000));
      expect(r.bottom, lessThanOrEqualTo(4000));
      // the raw (landscape) size would give a different, wrong rect
      final wrong = QrCropMath.imageRect(viewport: vp, frame: frame, transform: Matrix4.identity(), image: const Size(4000, 3000));
      expect(wrong == null || (wrong.top / 3000 - r.top / 4000).abs() > 0.05, isTrue);
    });

    test('picker reply parsing', () {
      expect(QrPicked.fromMap(picked), isNotNull);
      expect(QrPicked.fromMap({'error': 'unreadable'}), isNull);
      expect(QrPicked.fromMap({...picked, 'width': 0}), isNull);
      expect(QrPicked.fromMap({...picked, 'path': ''}), isNull);
    });

    test('adjust strings: fa and en, no ZWNJ, translated', () {
      const keys = ['qr.adjustTitle', 'qr.adjustHint', 'qr.scanArea', 'qr.pickAnother', 'qr.back'];
      for (final l in ['fa', 'en']) {
        for (final k in keys) {
          final v = S(l).t(k);
          expect(v, isNot(k));
          expect(v.contains('\u200c'), isFalse, reason: '$l $k has ZWNJ');
        }
      }
      for (final k in keys) {
        expect(S('fa').t(k), isNot(S('en').t(k)));
      }
      expect(S('fa').t('qr.scanArea'), 'اسکن این قسمت');
      expect(S('fa').t('qr.adjustHint'), 'کد QR را با بزرگ نمایی و جابه جایی داخل کادر قرار دهید');
    });
  });
}
