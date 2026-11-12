// 1.0.5: Settings › "Contact us" opens https://t.me/<username> outside the app.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/contact.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

void main() {
  setUpAll(loadAppFonts);
  final opened = <(Uri, LaunchMode)>[];
  setUp(() {
    calls.clear();
    installNativeMocks();
    opened.clear();
    ContactLink.username = kContactTelegramUsername;
    ContactLink.opener = (u, m) async {
      opened.add((u, m));
      return true;
    };
  });

  Future<AppState> boot(WidgetTester t, String lang) async {
    t.view.physicalSize = const Size(1080, 780 * 3);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    SharedPreferences.setMockInitialValues({
      'flutter.profiles': jsonEncode([vlessReality()]),
      'flutter.settings': jsonEncode({'language': lang}),
      'flutter.selected': 'p-vless',
    });
    final s = AppState();
    await s.init();
    await t.pumpWidget(VeilonApp(state: s));
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
    Shell.goTo(t.element(find.byType(TabRail)), 3);
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
    return s;
  }

  Future<void> toBottom(WidgetTester t) async {
    final row = find.byKey(const ValueKey('settings-contact'));
    await t.scrollUntilVisible(row, 300, scrollable: find.byType(Scrollable).last);
    for (var i = 0; i < 5; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
  }

  test('username constant and link', () {
    expect(kContactTelegramUsername, 'lxhosein');
    expect(ContactLink.uri.toString(), 'https://t.me/lxhosein');
  });

  for (final (lang, label) in [('fa', 'ارتباط با ما'), ('en', 'Contact us')]) {
    testWidgets('contact row is the last row and opens t.me externally ($lang)', (t) async {
      final s = await boot(t, lang);
      await toBottom(t);
      final row = find.byKey(const ValueKey('settings-contact'));
      expect(row, findsOneWidget);
      expect(find.descendant(of: row, matching: find.text(label)), findsOneWidget);
      expect(find.descendant(of: row, matching: find.text('@lxhosein')), findsOneWidget);
      // Bottom of the page: nothing below the row.
      final about = find.text(lang == 'fa' ? 'درباره' : 'About');
      expect(t.getTopLeft(row).dy, greaterThan(t.getTopLeft(about).dy));
      await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/settings_contact_$lang.png'));
      await t.tap(row);
      await t.pump(const Duration(milliseconds: 200));
      expect(opened, [(Uri.parse('https://t.me/lxhosein'), LaunchMode.externalApplication)]);
      expect(calls.where((c) => c.method == 'openUrl'), isEmpty, reason: 'uses url_launcher, not the core channel');
      await t.pump(const Duration(seconds: 4));
      s.dispose();
    });
  }

  testWidgets('a failed launch shows a toast', (t) async {
    ContactLink.opener = (u, m) async => false;
    final s = await boot(t, 'en');
    await toBottom(t);
    await t.tap(find.byKey(const ValueKey('settings-contact')));
    await t.pump(const Duration(milliseconds: 300));
    expect(find.text('Could not open Telegram or a browser'), findsOneWidget);
    await t.pump(const Duration(seconds: 4));
    s.dispose();
  });

  testWidgets('empty username hides the row', (t) async {
    ContactLink.username = '';
    final s = await boot(t, 'en');
    await t.drag(find.byType(Scrollable).last, const Offset(0, -5000));
    await t.pump(const Duration(milliseconds: 500));
    expect(find.byKey(const ValueKey('settings-contact')), findsNothing);
    expect(find.text('Contact us'), findsNothing);
    await t.pump(const Duration(seconds: 4));
    s.dispose();
  });
}
