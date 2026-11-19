// v1.0.13: the subscription description also comes from the `announce`
// header (many panels send only that, as `announce: base64:...`). The core
// returns it as SubResult.announce (already base64-decoded, not cleaned);
// the app shows profile-description when it has text, else announce.
// Fixtures test/fixtures/sub113_core_*.json are real FetchSubscription JSON
// from the unchanged 1.0.12 core against a local mock server
// (logs/mocksub113.py, fake links with documentation IPs):
//   a  = announce only (base64, ZWNJ, newline, a bidi override)
//   ad = announce + profile-description
//   n  = neither
//   ap = announce only, plain text
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

Map<String, dynamic> coreJson(String name) =>
    jsonDecode(File('test/fixtures/sub113_core_$name.json').readAsStringSync()) as Map<String, dynamic>;

/// What the user sees for fixture "a" (ZWNJ → space, U+202E removed).
const announceShown = 'اطلاعیه پنل\nتمدید تا آخر ماه ok';

final _live = <AppState>[];

Future<AppState> seeded(String lang) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      vlessReality(),
      trojan()..['subId'] = 's1',
    ]),
    'flutter.subs': jsonEncode([
      {
        'id': 's1',
        'url': 'https://panel.example.com:2096/sub/abc',
        'title': 'Veilon Panel',
        'userInfo': {'upload': 1, 'download': 2, 'total': 10737418240, 'expire': 0, 'present': true},
        'updatedAt': DateTime(2026, 11, 19, 18, 40).millisecondsSinceEpoch,
      }
    ]),
    'flutter.settings': jsonEncode({'language': lang, 'theme': 'dark'}),
    'flutter.selected': 'p-vless',
  });
  final s = AppState();
  await s.init();
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

Future<void> settle(WidgetTester t, [int frames = 12]) async {
  for (var i = 0; i < frames; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

Future<void> refreshTap(WidgetTester t) async {
  await t.tap(find.byKey(const ValueKey('group-refresh-s1')));
  await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
  await settle(t);
}

Future<String> storedDescription() async {
  final stored = jsonDecode((await SharedPreferences.getInstance()).getString('subs')!) as List;
  return (stored.single as Map)['description'] as String? ?? '<absent>';
}

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    installNativeMocks();
    fakeSubResult = null;
    fakeSubHandler = null;
  });
  tearDown(() => Palette.use(darkPalette));

  group('subDescriptionFrom', () {
    test('announce only → shown (cleaned)', () {
      expect(subDescriptionFrom({'announce': 'Panel news\u200cok\u0000 \u202e!'}), 'Panel news ok !');
    });
    test('profile-description wins over announce', () {
      expect(subDescriptionFrom({'description': 'Desc', 'announce': 'Ann'}), 'Desc');
    });
    test('empty or junk-only profile-description falls back to announce', () {
      expect(subDescriptionFrom({'description': '', 'announce': 'Ann'}), 'Ann');
      expect(subDescriptionFrom({'description': ' \u0000\u202e\n ', 'announce': 'Ann'}), 'Ann');
    });
    test('neither → empty; non-string values ignored', () {
      expect(subDescriptionFrom({}), '');
      expect(subDescriptionFrom({'announce': '', 'description': ''}), '');
      expect(subDescriptionFrom({'announce': 42, 'description': null}), '');
    });
    test('announce gets the same cap', () {
      final s = subDescriptionFrom({'announce': 'ب' * 1500});
      expect(s.runes.length, maxSubDescription);
      expect(s.endsWith('…'), isTrue);
    });
  });

  group('real core JSON (base64 announce decoded by the 1.0.12 core)', () {
    test('fixtures have the expected shape', () {
      expect(coreJson('a')['announce'], 'اطلاعیه\u200cپنل\nتمدید تا آخر ماه \u202eok');
      expect(coreJson('a').containsKey('description'), isFalse);
      expect(coreJson('ad')['description'], 'توضیحات اشتراک');
      expect(coreJson('n').containsKey('announce'), isFalse);
      expect(coreJson('n').containsKey('description'), isFalse);
    });

    for (final c in [
      ('a', announceShown),
      ('ad', 'توضیحات اشتراک'),
      ('ap', 'Plain announce text'),
      ('n', ''),
    ]) {
      test('add subscription with core reply ${c.$1} → "${c.$2}"', () async {
        SharedPreferences.setMockInitialValues({'flutter.settings': jsonEncode({'language': 'fa'})});
        final a = AppState();
        await a.init();
        fakeSubResult = coreJson(c.$1);
        expect(await a.addSubscription('https://panel.example.com/sub/x'), isNull);
        expect(a.subs.single.description, c.$2);
        expect(a.subs.single.title, 'Mock');
        expect(a.profiles.where((p) => p.subId == a.subs.single.id).length, 2, reason: 'servers still imported');
        expect(await storedDescription(), c.$2.isEmpty ? '<absent>' : c.$2);
        a.dispose();
      });
    }
  });

  group('persistence and refresh', () {
    test('announce saved, survives restart, failed refresh keeps, refresh replaces, description wins, neither clears',
        () async {
      SharedPreferences.setMockInitialValues({'flutter.settings': jsonEncode({'language': 'fa'})});
      final a = AppState();
      await a.init();
      fakeSubResult = coreJson('a');
      expect(await a.addSubscription('https://panel.example.com/sub/x'), isNull);
      expect(a.subs.single.description, announceShown);
      a.dispose();

      final b = AppState();
      await b.init();
      expect(b.subs.single.description, announceShown, reason: 'restart');

      fakeSubResult = {'status': 500, 'errors': ['HTTP 500'], 'profiles': []};
      expect(await b.refreshSubscription(b.subs.single), isNotNull);
      expect(b.subs.single.description, announceShown, reason: 'failed refresh keeps');
      fakeSubResult = {'status': 0, 'errors': ['dial tcp: timeout'], 'profiles': []};
      expect(await b.refreshSubscription(b.subs.single), isNotNull);
      expect(b.subs.single.description, announceShown, reason: 'network error keeps');
      expect(await storedDescription(), announceShown);

      fakeSubResult = coreJson('ap');
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, 'Plain announce text', reason: 'new announce replaces');

      fakeSubResult = coreJson('ad');
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, 'توضیحات اشتراک', reason: 'profile-description wins');

      fakeSubResult = coreJson('a');
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, announceShown, reason: 'description gone, announce shown');

      fakeSubResult = coreJson('n');
      expect(await b.refreshSubscription(b.subs.single), isNull);
      expect(b.subs.single.description, '', reason: 'neither → cleared');
      expect(await storedDescription(), '<absent>');
      b.dispose();

      final c = AppState();
      await c.init();
      expect(c.subs.single.description, '');
      c.dispose();
    });
  });

  group('servers page', () {
    for (final lang in ['fa', 'en']) {
      testWidgets('announce-only panel: description shown under the sub name, then cleared ($lang)', (t) async {
        t.view.physicalSize = const Size(360 * 3, 780 * 3);
        t.view.devicePixelRatio = 3;
        addTearDown(t.view.reset);
        final state = await seeded(lang);
        await t.pumpWidget(VeilonApp(state: state));
        await settle(t);
        Shell.goTo(t.element(find.byType(TabRail)), 1);
        await settle(t);
        await t.tap(find.byKey(const ValueKey('group-toggle-_manual')));
        await settle(t);
        expect(find.byType(SubDescription), findsNothing);

        fakeSubResult = coreJson('a');
        await refreshTap(t);
        expect(find.byKey(const ValueKey('group-desc-s1')), findsOneWidget);
        expect(find.byKey(const ValueKey('group-desc-text-s1')), findsOneWidget);
        expect(find.text(announceShown), findsOneWidget);

        fakeSubResult = coreJson('ad');
        await refreshTap(t);
        expect(find.text('توضیحات اشتراک'), findsOneWidget);
        expect(find.text(announceShown), findsNothing);

        fakeSubResult = {'status': 503, 'errors': ['HTTP 503'], 'profiles': []};
        await refreshTap(t);
        expect(find.text('توضیحات اشتراک'), findsOneWidget, reason: 'failed refresh keeps');

        fakeSubResult = coreJson('n');
        await refreshTap(t);
        expect(find.byKey(const ValueKey('group-desc-s1')), findsNothing);
        expect(t.takeException(), isNull);
        await t.pump(const Duration(seconds: 5));
        await done(t);
      });
    }
  });
}
