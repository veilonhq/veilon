// 1.0.6 (second batch): new defaults (delay-test URL, direct DNS), an (i)
// explanation for every setting, and the restyled connect button.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

/// Every setting row on the Settings page and its explanation key.
const _infoKeys = [
  'language', 'killSwitch', 'alwaysOn', 'reconnect', 'boot', 'autoFastest', //
  'bypassIran', 'bypassLan', 'blockAds', 'perApp', 'domainRules',
  'remoteDns', 'directDns',
  'mtu', 'disableQuic', 'ipv6', 'core', 'pingUrl', 'fullHandshakePing', 'logLevel',
  'logs', 'hwid',
];

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    installNativeMocks();
  });

  group('defaults', () {
    test('new install: Google delay-test URL, AliDNS direct, Cloudflare DoH remote', () {
      final s = AppSettings();
      expect(s.pingUrl, 'http://www.google.com/generate_204');
      expect(s.directDns, '223.5.5.5');
      expect(s.remoteDns, 'https://1.1.1.1/dns-query');
      final j = AppSettings.fromJson(const {});
      expect(j.pingUrl, 'http://www.google.com/generate_204');
      expect(j.directDns, '223.5.5.5');
      expect(j.remoteDns, 'https://1.1.1.1/dns-query');
      expect(s.toCoreJson(assetDir: '/a', appPackage: 'app.veilon')['directDns'], '223.5.5.5');
    });

    test('saved values of existing users are kept', () {
      final j = AppSettings.fromJson(const {
        'pingUrl': 'https://www.gstatic.com/generate_204',
        'directDns': '8.8.8.8',
        'remoteDns': 'tls://8.8.8.8',
      });
      expect(j.pingUrl, 'https://www.gstatic.com/generate_204');
      expect(j.directDns, '8.8.8.8');
      expect(j.remoteDns, 'tls://8.8.8.8');
    });
  });

  test('every explanation exists in Persian and English', () {
    for (final k in [..._infoKeys, 'tooltip']) {
      final fa = S('fa').t('info.$k'), en = S('en').t('info.$k');
      expect(fa, isNot('info.$k'), reason: 'fa info.$k');
      expect(en, isNot('info.$k'), reason: 'en info.$k');
      expect(fa, isNot(en), reason: 'info.$k must be translated');
    }
    // The kill switch text explains both states.
    expect(S('en').t('info.killSwitch'), allOf(contains('On:'), contains('Off:')));
    expect(S('fa').t('info.killSwitch'), allOf(contains('روشن:'), contains('خاموش:')));
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

  Future<void> settle(WidgetTester t) async {
    for (var i = 0; i < 6; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
  }

  for (final lang in ['fa', 'en']) {
    testWidgets('every setting has an (i) button that explains it ($lang)', (t) async {
      final s = await boot(t, lang);
      final scroll = find.byType(Scrollable).last;
      for (final k in _infoKeys) {
        final b = find.byKey(ValueKey('info-info.$k'));
        await t.scrollUntilVisible(b, 200, scrollable: scroll);
        await settle(t);
        expect(b, findsOneWidget, reason: k);
        await t.tap(b);
        await settle(t);
        final sheet = find.byKey(const ValueKey('setting-info-sheet'));
        expect(sheet, findsOneWidget, reason: k);
        expect(find.descendant(of: sheet, matching: find.text(S(lang).t('info.$k'))), findsOneWidget, reason: k);
        if (k == 'killSwitch') {
          await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/settings_info_killswitch_$lang.png'));
        }
        // The setting itself must not have changed by tapping (i).
        expect(s.settings.killSwitch, isFalse);
        await t.tap(find.descendant(of: sheet, matching: find.byType(TextButton)));
        await settle(t);
        expect(sheet, findsNothing, reason: k);
      }
      await t.pump(const Duration(seconds: 4));
      s.dispose();
    });
  }
}
