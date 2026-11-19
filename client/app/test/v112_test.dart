// v1.0.12: subscription description (profile-description header or
// "#profile-description:" body line from the core). Goldens fa/en ×
// dark/light with a short and a long description; persistence across an
// app restart, replacement and clearing on refresh, kept on a failed
// refresh; untrusted text cleaned; direction follows the text.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const shortFa = 'سرورهای پرسرعت آلمان و هلند\nتمدید ماهانه، پشتیبانی ۲۴ ساعته';
const shortEn = 'Fast servers in Germany and the Netherlands.\nRenewed monthly, 24/7 support.';
final longFa = List.filled(
        6,
        'این اشتراک برای استفاده روزانه، استریم و بازی ساخته شده است و هر ماه به صورت خودکار تمدید می شود. '
        'در صورت قطعی به پشتیبانی پیام دهید.')
    .join('\n');
final longEn = List.filled(
        6,
        'This subscription is built for daily use, streaming and gaming, and renews automatically every month. '
        'Message support if a server goes down.')
    .join('\n');

/// Usage without an expiry date: no "days left" text, so these goldens do
/// not drift with the date (the old fixtures do).
Map<String, dynamic> descSub(String description) => {
      'id': 's1',
      'url': 'https://panel.example.com:2096/sub/abc',
      'title': 'Veilon Panel',
      'userInfo': {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 0, 'present': true},
      'updatedAt': DateTime(2026, 11, 19, 18, 40).millisecondsSinceEpoch,
      if (description.isNotEmpty) 'description': description,
    };

final _live = <AppState>[];

Future<AppState> seeded(String lang, {String theme = 'dark', String description = ''}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      vlessReality(),
      trojan()..['subId'] = 's1',
      (vlessReality()
        ..['id'] = 'p-sub-v'
        ..['name'] = 'Amsterdam · Reality'
        ..['subId'] = 's1'),
    ]),
    'flutter.subs': jsonEncode([descSub(description)]),
    'flutter.settings': jsonEncode({'language': lang, 'theme': theme}),
    'flutter.selected': 'p-vless',
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

/// Opens the servers tab with "My configs" collapsed (the subscription group
/// header in view).
Future<void> openServers(WidgetTester t, AppState state) async {
  await t.pumpWidget(VeilonApp(state: state));
  await settle(t);
  Shell.goTo(t.element(find.byType(TabRail)), 1);
  await settle(t);
  await t.tap(find.byKey(const ValueKey('group-toggle-_manual')));
  await settle(t);
}

RenderParagraph descParagraph(WidgetTester t) => t.renderObject<RenderParagraph>(find.byKey(const ValueKey('group-desc-text-s1')));

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    installNativeMocks();
    fakeSubResult = null;
    fakeSubHandler = null;
  });
  tearDown(() => Palette.use(darkPalette));

  group('cleanSubDescription', () {
    test('control, bidi and ZWNJ characters; blank lines; cap', () {
      expect(cleanSubDescription('a\u0000b\u0007c\u001b[31md\u007f'), 'abc[31md');
      expect(cleanSubDescription('tab\there'), 'tab here');
      expect(cleanSubDescription('\u202eevil\u202c \u2067x\u2069 \u200fy'), 'evil x y');
      expect(cleanSubDescription('نیم\u200cفاصله'), 'نیم فاصله');
      expect(cleanSubDescription('one\r\n\r\n\n\ntwo   \n  '), 'one\n\ntwo');
      expect(cleanSubDescription('<b>x</b> <a href="javascript:1">l</a>'), '<b>x</b> <a href="javascript:1">l</a>',
          reason: 'kept as text; the widget never interprets it');
      final long = cleanSubDescription('ب' * 1500);
      expect(long.runes.length, maxSubDescription);
      expect(long.endsWith('…'), isTrue);
      expect(cleanSubDescription('x' * maxSubDescription), 'x' * maxSubDescription);
      expect(cleanSubDescription('\u{1F680} ok'), '\u{1F680} ok', reason: 'astral characters survive');
    });

    test('model: stored text is cleaned on load, empty is not written', () {
      final s = Subscription.fromJson({'id': 'a', 'url': 'https://x.test/s', 'description': ' hi\u0000\u202e '});
      expect(s.description, 'hi');
      expect(Subscription(id: 'b', url: 'u').toJson().containsKey('description'), isFalse);
      expect(Subscription.fromJson(s.toJson()).description, 'hi');
    });

    test('textDirectionOf follows the first strong character', () {
      expect(textDirectionOf('سلام hello', TextDirection.ltr), TextDirection.rtl);
      expect(textDirectionOf('Hello سلام', TextDirection.rtl), TextDirection.ltr);
      expect(textDirectionOf('۲۴/7 — پشتیبانی', TextDirection.ltr), TextDirection.rtl);
      expect(textDirectionOf('24/7 support', TextDirection.rtl), TextDirection.ltr);
      expect(textDirectionOf('12345 !!', TextDirection.rtl), TextDirection.rtl);
    });
  });

  group('persistence and refresh', () {
    test('saved, survives a restart, replaced on refresh, cleared when none, kept on a failed refresh', () async {
      SharedPreferences.setMockInitialValues({'flutter.settings': jsonEncode({'language': 'fa'})});
      final a = AppState();
      await a.init();
      fakeSubResult = {
        'status': 200,
        'title': 'Panel',
        'description': 'اول\u0000\nخط دوم',
        'profiles': [trojan()],
      };
      expect(await a.addSubscription('https://panel.example.com/sub/x'), isNull);
      final sub = a.subs.single;
      expect(sub.description, 'اول\nخط دوم', reason: 'cleaned');
      final prefs = await SharedPreferences.getInstance();
      final stored = jsonDecode(prefs.getString('subs')!) as List;
      expect((stored.single as Map)['description'], 'اول\nخط دوم');
      a.dispose();

      // App restart: loaded from storage.
      final b = AppState();
      await b.init();
      expect(b.subs.single.description, 'اول\nخط دوم');

      // Failed refresh keeps it.
      fakeSubResult = {'status': 500, 'errors': ['HTTP 500'], 'profiles': []};
      expect(await b.refreshSubscription(b.subs.single), isNotNull);
      expect(b.subs.single.description, 'اول\nخط دوم');

      // New description replaces it.
      fakeSubResult = {'status': 200, 'description': 'New text', 'profiles': [trojan()]};
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, 'New text');

      // A refresh without a description clears it, also in storage.
      fakeSubResult = {'status': 200, 'profiles': [trojan()]};
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, '');
      final stored2 = jsonDecode((await SharedPreferences.getInstance()).getString('subs')!) as List;
      expect((stored2.single as Map).containsKey('description'), isFalse);
      b.dispose();
      final c = AppState();
      await c.init();
      expect(c.subs.single.description, '');
      c.dispose();
    });

    testWidgets('header shows, updates and hides the description after refreshes', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('en', description: shortEn);
      await openServers(t, state);
      expect(find.byKey(const ValueKey('group-desc-s1')), findsOneWidget);
      expect(find.text(shortEn), findsOneWidget);
      expect(find.byKey(const ValueKey('group-desc-more-s1')), findsNothing, reason: 'two lines: no "More"');

      fakeSubResult = {'status': 200, 'description': 'Updated by the panel', 'profiles': [trojan()]};
      await t.tap(find.byKey(const ValueKey('group-refresh-s1')));
      await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
      await settle(t);
      expect(find.text('Updated by the panel'), findsOneWidget);
      expect(find.text(shortEn), findsNothing);

      fakeSubResult = {'status': 200, 'profiles': [trojan()]};
      await t.tap(find.byKey(const ValueKey('group-refresh-s1')));
      await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
      await settle(t);
      expect(find.byKey(const ValueKey('group-desc-s1')), findsNothing);
      expect(t.takeException(), isNull);
      await t.pump(const Duration(seconds: 5));
      await done(t);
    });

    testWidgets('no description: header unchanged (no description widget)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded('fa');
      await openServers(t, state);
      expect(find.byType(SubDescription), findsNothing);
      await done(t);
    });
  });

  group('display', () {
    for (final lang in ['fa', 'en']) {
      for (final theme in ['dark', 'light']) {
        testWidgets('description $lang $theme', (t) async {
          phone(t);
          addTearDown(t.view.reset);
          final state = await seeded(lang, theme: theme, description: lang == 'fa' ? shortFa : shortEn);
          await openServers(t, state);
          expect(t.takeException(), isNull);
          final p = descParagraph(t);
          expect(p.didExceedMaxLines, isFalse);
          expect(p.textDirection, lang == 'fa' ? TextDirection.rtl : TextDirection.ltr);
          await expectLater(goldenRoot(), matchesGoldenFile('goldens/sub_desc_${lang}_$theme.png'));
          await done(t);
        });

        testWidgets('long description $lang $theme: three lines, More expands, Less collapses', (t) async {
          phone(t);
          addTearDown(t.view.reset);
          final state = await seeded(lang, theme: theme, description: lang == 'fa' ? longFa : longEn);
          await openServers(t, state);
          expect(t.takeException(), isNull);
          final p = descParagraph(t);
          expect(p.didExceedMaxLines, isTrue);
          final collapsed = p.size.height;
          expect(find.byKey(const ValueKey('group-desc-more-s1')), findsOneWidget);
          await expectLater(goldenRoot(), matchesGoldenFile('goldens/sub_desc_long_${lang}_$theme.png'));
          await t.tap(find.byKey(const ValueKey('group-desc-more-s1')));
          await settle(t, 4);
          final open = descParagraph(t);
          expect(open.didExceedMaxLines, isFalse);
          expect(open.size.height, greaterThan(collapsed * 3), reason: 'whole text shown');
          expect(find.text(lang == 'fa' ? 'کمتر' : 'Less'), findsOneWidget);
          if (lang == 'fa' && theme == 'dark') {
            await expectLater(goldenRoot(), matchesGoldenFile('goldens/sub_desc_long_open_fa_dark.png'));
          }
          await t.tap(find.byKey(const ValueKey('group-desc-more-s1')));
          await settle(t, 4);
          expect(descParagraph(t).didExceedMaxLines, isTrue);
          expect(t.takeException(), isNull);
          await done(t);
        });
      }
    }

    testWidgets('English text in the Persian UI is LTR; Persian text in the English UI is RTL (320 dp)', (t) async {
      phone(t, width: 320, height: 700);
      addTearDown(t.view.reset);
      var state = await seeded('fa', description: shortEn);
      await openServers(t, state);
      expect(descParagraph(t).textDirection, TextDirection.ltr);
      expect(t.takeException(), isNull);
      await done(t);
      state = await seeded('en', description: shortFa);
      await openServers(t, state);
      expect(descParagraph(t).textDirection, TextDirection.rtl);
      expect(t.takeException(), isNull);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/sub_desc_mixed_en_320.png'));
      await done(t);
    });

    testWidgets('markup and links are plain text (nothing tappable besides More)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      const evil = '<a href="https://evil.test">click</a> https://evil.test/x';
      final state = await seeded('en', description: evil);
      await openServers(t, state);
      expect(find.text(evil), findsOneWidget);
      final opened = calls.where((c) => c.method == 'openUrl').length;
      await t.tap(find.text(evil));
      await settle(t, 3);
      expect(calls.where((c) => c.method == 'openUrl').length, opened);
      await done(t);
    });
  });
}
