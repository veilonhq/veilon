// Visual check: renders the main screens and the profile editor to PNG.
// Run: flutter test --update-goldens test/screens_test.dart
// Output: test/goldens/*.png.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/profile_editor_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

Map<String, dynamic> grpcMasked() => vlessReality()
  ..['id'] = 'p-grpc'
  ..['name'] = 'gRPC · masks'
  ..['network'] = 'grpc'
  ..['serviceName'] = 'veilon'
  ..['mode'] = 'multi'
  ..['finalMask'] = jsonEncode({
    'tcp': [
      {'type': 'fragment', 'settings': {'packets': 'tlshello', 'length': '100-200', 'interval': '10-20'}},
      {'type': 'header-custom'},
    ]
  })
  ..['sockopt'] = jsonEncode({'tcpFastOpen': true, 'domainStrategy': 'UseIPv4'})
  ..['mux'] = jsonEncode({'enabled': true, 'concurrency': 8});

Map<String, dynamic> kcpProfile() => vlessReality()
  ..['id'] = 'p-kcp'
  ..['name'] = 'mKCP'
  ..['network'] = 'kcp'
  ..['security'] = 'none'
  ..['headerType'] = 'wechat-video'
  ..['seed'] = 'seed123'
  ..['kcp'] = {'mtu': 1350, 'tti': 20, 'uplinkCapacity': 5, 'downlinkCapacity': 20};

/// A subscription with usage (subscription-userinfo) and two servers.
Map<String, dynamic> panelSub() => {
      'id': 's1',
      'url': 'https://panel.example.com:2096/sub/abc',
      'title': 'Veilon Panel',
      'userInfo': {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 1798761600, 'present': true},
      'updatedAt': DateTime(2026, 11, 11, 18, 40).millisecondsSinceEpoch,
    };

Future<AppState> seeded(String lang) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([
      vlessReality(), wireguard(), grpcMasked(), kcpProfile(),
      trojan()..['subId'] = 's1',
      (vlessReality()..['id'] = 'p-sub-v'..['name'] = 'Amsterdam · Reality'..['subId'] = 's1'),
    ]),
    'flutter.subs': jsonEncode([panelSub()]),
    'flutter.settings': jsonEncode({'language': lang}),
    'flutter.selected': 'p-vless',
  });
  final s = AppState();
  await s.init();
  s.pings.addAll({'p-vless': 142}); // pings are measured per session, never restored
  return s;
}

void phone(WidgetTester t, {double height = 780}) {
  t.view.physicalSize = Size(1080, height * 3);
  t.view.devicePixelRatio = 3;
}

Future<void> settle(WidgetTester t) async {
  for (var i = 0; i < 12; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  setUpAll(loadAppFonts);
  setUp(installNativeMocks);

  for (final lang in ['fa', 'en']) {
    testWidgets('shell pages $lang', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/home_$lang.png'));
      final names = ['servers', 'stats', 'settings'];
      for (var i = 0; i < names.length; i++) {
        Shell.goTo(t.element(find.byType(TabRail)), i + 1);
        await t.pump(const Duration(milliseconds: 400));
        await settle(t);
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/${names[i]}_$lang.png'));
      }
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });
  }

  for (final lang in ['fa', 'en']) {
    testWidgets('home connection error $lang', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      state.handleEvent({'type': 'state', 'state': 'error', 'error': 'start: initialize cache-file: open cache.db: read-only file system'});
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/home_error_$lang.png'));
      await t.tap(find.text(S(lang).t('err.details')));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/error_details_$lang.png'));
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });

    testWidgets('home connected with traffic $lang', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch - 754000});
      for (var i = 0; i < 6; i++) {
        state.handleEvent({'type': 'stats', 'down': 180000.0 * (i + 1), 'up': 24000.0 * (i + 1), 'sessionDown': 52428800 * (i + 1), 'sessionUp': 3145728 * (i + 1)});
        await t.pump(const Duration(milliseconds: 300));
      }
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/home_connected_$lang.png'));
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });
  }

  for (final lang in ['fa', 'en']) {
    testWidgets('servers groups and add sheet $lang', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      Shell.goTo(t.element(find.byType(TabRail)), 1);
      await settle(t);
      // "My configs" collapsed: the subscription group with its usage bar.
      await t.tap(find.byKey(const ValueKey('group-toggle-_manual')));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/servers_groups_$lang.png'));
      await t.tap(find.byKey(const ValueKey('group-toggle-_manual')));
      await settle(t);
      showAddSheet(t.element(find.byType(ServersPage)));
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/add_sheet_$lang.png'));
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });

    testWidgets('servers at 320dp $lang', (t) async {
      t.view.physicalSize = const Size(960, 2000);
      t.view.devicePixelRatio = 3;
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      Shell.goTo(t.element(find.byType(TabRail)), 1);
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/servers_320_$lang.png'));
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });
  }

  for (final (id, type) in [('p-vless', 'vless'), ('p-wg', 'awg'), ('p-grpc', 'grpc_masks'), ('p-kcp', 'mkcp'), ('', 'new')]) {
    for (final lang in ['fa', 'en']) {
      testWidgets('editor $type $lang', (t) async {
        phone(t, height: 4600);
        addTearDown(t.view.reset);
        final state = await seeded(lang);
        final isNew = id.isEmpty;
        final p = isNew ? Profile(<String, dynamic>{'type': 'vless'}) : state.profiles.firstWhere((p) => p.id == id);
        await t.pumpWidget(AppScope(
          state: state,
          child: SScope(
            s: S(lang),
            child: MaterialApp(
              debugShowCheckedModeBanner: false,
              theme: buildTheme(),
              builder: (c, child) => Directionality(textDirection: S(lang).dir, child: SScope(s: S(lang), child: child!)),
              home: ProfileEditorPage(profile: p, isNew: isNew),
            ),
          ),
        ));
        await settle(t);
        await expectLater(goldenRoot(), matchesGoldenFile('goldens/editor_${type}_$lang.png'));
        await t.pump(const Duration(seconds: 4));
        state.dispose();
      });
    }
  }
}
