// v1.0.2: which profiles are pingable, opaque sheets, speed numbers
// without count-up. (Categories, the scanner and SlipNet were removed in
// 1.0.4; see v104_test.dart.)
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';
import 'package:veilon/ui/shell.dart';
import 'package:veilon/ui/widgets/glass.dart';

import 'helpers.dart';

Future<AppState> seeded(String lang, {Map<String, Object> extra = const {}}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([vlessReality(), trojan(), wireguard()]),
    'flutter.settings': jsonEncode({'language': lang}),
    'flutter.selected': 'p-vless',
    ...extra,
  });
  final s = AppState();
  await s.init();
  return s;
}

void phone(WidgetTester t, {double width = 360, double height = 780}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
}

Future<void> toServers(WidgetTester t) async {
  Shell.goTo(t.element(find.byType(TabRail)), 1);
  for (var i = 0; i < 6; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

List<String> visibleTiles() => find
    .descendant(of: find.byType(ServersPage), matching: find.byWidgetPredicate((w) => w.key is ValueKey<String> && '${(w.key as ValueKey).value}'.startsWith('p-')))
    .evaluate()
    .map((e) => '${(e.widget.key as ValueKey).value}')
    .toList();

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    fakeParseText = null;
    installNativeMocks();
  });

  test('which kinds are pingable', () {
    Profile p(String type) => Profile({'id': type, 'type': type});
    const xray = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'http', 'socks', 'masque'];
    for (final t in xray) {
      expect(p(t).category, ProfileCategory.xray, reason: t);
      expect(p(t).pingable, isTrue, reason: t);
    }
    for (final t in ['wireguard', 'amneziawg']) {
      expect(p(t).category, ProfileCategory.amnezia, reason: t);
      expect(p(t).pingable, isFalse, reason: t);
    }
    expect(p('mtproto').pingable, isFalse);
  });

  test('ping all / fastest never test AmneziaWG profiles', () async {
    final state = await seeded('en');
    await state.pingAll();
    final tested = calls.where((c) => c.method == 'urlTest').map((c) => (jsonDecode((c.arguments as Map)['profile'] as String) as Map)['id']).toSet();
    expect(tested, {'p-vless', 'p-trojan'});
    expect(await state.ping(state.profiles.firstWhere((p) => p.id == 'p-wg')), -1);
    expect(state.pings.containsKey('p-wg'), isFalse);
    expect(state.fastest()?.category, ProfileCategory.xray);
    // shown exactly as measured
    expect(state.pings['p-vless'], fakeDelay);
    state.dispose();
  });

  test('pings are never restored from an earlier session', () async {
    final state = await seeded('en', extra: {'flutter.pings': jsonEncode({'p-vless': 142})});
    expect(state.pings, isEmpty);
    state.dispose();
  });

  testWidgets('AmneziaWG rows show a neutral dash that never pings', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en');
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    await toServers(t);
    expect(visibleTiles(), ['p-vless', 'p-trojan', 'p-wg']);
    expect(find.byKey(const ValueKey('ping-none')), findsOneWidget);
    await t.tap(find.byKey(const ValueKey('ping-none')));
    await t.pump(const Duration(milliseconds: 100));
    expect(calls.where((c) => c.method == 'urlTest'), isEmpty);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('add sheet is opaque and the + button is hidden while it is open', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('fa');
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    await toServers(t);
    final fab = find.byIcon(Icons.add_rounded);
    expect(fab, findsWidgets);
    showAddSheet(t.element(find.byType(ServersPage)));
    for (var f = 0; f < 30; f++) {
      await t.pump(const Duration(milliseconds: 16));
      // From the first frame on the FAB is gone (no frame shows it under the sheet).
      final vis = t.widgetList<Visibility>(find.descendant(of: find.byType(ServersPage), matching: find.byType(Visibility)));
      expect(vis.any((v) => v.visible == false), isTrue, reason: 'frame $f');
    }
    final sheet = t.widget<SolidSheet>(find.byType(SolidSheet));
    expect(sheet, isNotNull);
    final box = t.widget<Container>(find.descendant(of: find.byType(SolidSheet), matching: find.byType(Container)).first);
    final color = (box.decoration as BoxDecoration).color!;
    expect(color.a, 1.0, reason: 'sheet background must be fully opaque');
    expect(color, Palette.bg2);
    Navigator.of(t.element(find.byType(AddSheet))).pop();
    await t.pump(const Duration(milliseconds: 600));
    expect(addSheetOpen.value, isFalse);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('speed numbers show the current value at once (no count-up, no reset)', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en');
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
    await t.pump(const Duration(milliseconds: 300));
    final seen = <String>{};
    for (final (down, label) in [(1024.0 * 1024 * 12.4, '12.4'), (1024.0 * 300, '300'), (1024.0 * 1024 * 3.1, '3.1')]) {
      state.handleEvent({'type': 'stats', 'down': down, 'up': 1000.0, 'sessionDown': 1, 'sessionUp': 1});
      // Frame-by-frame: the very first frame shows the new value, and no
      // intermediate number ever appears.
      for (var f = 0; f < 20; f++) {
        await t.pump(const Duration(milliseconds: 16));
        expect(find.text(label), findsOneWidget, reason: 'frame $f for $label');
        for (final w in t.widgetList<Text>(find.byType(Text))) {
          final d = w.data ?? '';
          if (RegExp(r'^\d+(\.\d)?$').hasMatch(d)) seen.add(d);
        }
      }
    }
    // only real values (download + upload "1000 B/s" -> "1000"), nothing in between
    expect(seen.difference({'12.4', '300', '3.1', '1000', '0'}), isEmpty, reason: '$seen');
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });
}
