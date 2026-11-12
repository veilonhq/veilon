// v1.0.4: SlipNet / Naive / scanner / stack picker removed (generic SOCKS5
// stays: it is a standard Xray / sing-box protocol);
// Servers page as one list of collapsible groups ("My configs" + one group
// per subscription); "Add manually" for every Xray / sing-box protocol
// (AmneziaWG only by .conf import or paste); home status "Connected".
//
// The manual-add test writes the saved profile JSONs to
// core/veiloncore/testdata/manual_profiles.json; the Go test
// TestManualProfilesFromApp then checks each with the real core
// (CheckProfile + ToLink → ParseLink round trip).
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/core/profile_schema.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/theme/palette.dart';
import 'package:veilon/ui/pages/profile_editor_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const manualOut = '../core/veiloncore/testdata/manual_profiles.json';

final _live = <AppState>[];

Future<AppState> seeded(String lang, {List<Map<String, dynamic>>? profiles, List<Map<String, dynamic>>? subs, Map<String, Object> extra = const {}}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode(profiles ?? [vlessReality(), wireguard()]),
    'flutter.settings': jsonEncode({'language': lang}),
    'flutter.selected': 'p-vless',
    'flutter.subs': ?(subs == null ? null : jsonEncode(subs)),
    ...extra,
  });
  final s = AppState();
  await s.init();
  _live.add(s);
  return s;
}

Future<void> done(WidgetTester t) async {
  await t.pumpWidget(const SizedBox());
  await t.pump(const Duration(seconds: 4));
  for (final s in _live) {
    s.dispose();
  }
  _live.clear();
}

void phone(WidgetTester t, {double width = 360, double height = 780}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
}

Future<void> settle(WidgetTester t, [int n = 8]) async {
  for (var i = 0; i < n; i++) {
    await t.pump(const Duration(milliseconds: 100));
  }
}

Future<void> toServers(WidgetTester t) async {
  Shell.goTo(t.element(find.byType(TabRail)), 1);
  await settle(t);
}

List<String> tileIds() => find
    .descendant(of: find.byType(ServersPage), matching: find.byWidgetPredicate((w) => w.key is ValueKey<String> && '${(w.key as ValueKey).value}'.startsWith('p-')))
    .evaluate()
    .map((e) => '${(e.widget.key as ValueKey).value}')
    .toList();

Map<String, dynamic> subProfile(String id, String sub) => vlessReality()
  ..['id'] = id
  ..['name'] = 'Sub $id'
  ..['subId'] = sub;

/// 1.5 GiB up + 6 GiB down of 10 GiB, expiring 2027-01-01.
const usage = {'upload': 1610612736, 'download': 6442450944, 'total': 10737418240, 'expire': 1798761600, 'present': true};

void main() {
  setUpAll(loadAppFonts);
  setUp(() {
    calls.clear();
    fakeParseText = null;
    fakeSubResult = null;
    exportLink = null;
    installNativeMocks();
  });

  // ---------- Servers page: groups instead of tabs ----------

  for (final lang in ['fa', 'en']) {
    testWidgets('servers: no protocol tabs / filter chips; Xray and AmneziaWG in one "My configs" group ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      await toServers(t);
      final s = S(lang);
      for (final c in ['xray', 'amnezia', 'slipnet', 'ssh']) {
        expect(find.byKey(ValueKey('cat-$c')), findsNothing);
      }
      expect(find.text('Xray / sing-box'), findsNothing);
      expect(find.text(s.t('servers.pingAll')), findsNothing);
      expect(find.text(lang == 'fa' ? 'همه' : 'All'), findsNothing);
      expect(find.text(lang == 'fa' ? 'دستی' : 'Manual'), findsNothing);
      expect(find.byKey(const ValueKey('group-_manual')), findsOneWidget);
      expect(find.text(s.t('servers.myConfigs')), findsOneWidget);
      expect(s.t('servers.myConfigs'), lang == 'fa' ? 'کانفیگ های من' : 'My configs');
      expect(tileIds(), ['p-vless', 'p-wg']);
      await done(t);
    });
  }

  testWidgets('adding a subscription creates its own group with usage bar, expiry and update time', (t) async {
    phone(t, height: 900);
    addTearDown(t.view.reset);
    final state = await seeded('en');
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    expect(find.byWidgetPredicate((w) => w.key is ValueKey<String> && '${(w.key as ValueKey).value}'.startsWith('group-')).evaluate().length,
        greaterThan(0));
    fakeSubResult = {
      'status': 200,
      'title': 'Veilon Panel',
      'userInfo': usage,
      'profiles': [subProfile('p-s1', 'x'), subProfile('p-s2', 'x')],
    };
    final err = await state.addSubscription('https://panel.example.com:2096/sub/abc');
    expect(err, isNull);
    await settle(t);
    final sub = state.subs.single;
    expect(find.byKey(ValueKey('group-${sub.id}')), findsOneWidget);
    // Manual group first, then the subscription; its configs live in it.
    expect(tileIds(), ['p-vless', 'p-wg', 'p-s1', 'p-s2']);
    final header = find.byKey(ValueKey('group-${sub.id}'));
    expect(find.descendant(of: header, matching: find.text('Veilon Panel')), findsOneWidget);
    // Usage: used = upload + download = 7.5 GiB of 10 GiB.
    final ui = UserInfo.fromJson(usage);
    expect(ui.used, 1610612736 + 6442450944);
    expect(ui.usedFraction, closeTo(0.75, 1e-9));
    final label = UsageBar.label(ui);
    expect(label, contains(' / '));
    expect(find.descendant(of: header, matching: find.text(label)), findsOneWidget);
    final bar = t.widget<LinearProgressIndicator>(find.descendant(of: header, matching: find.byKey(const ValueKey('usage-bar'))));
    expect(bar.value, closeTo(0.75, 1e-9));
    final meta = t.widget<Text>(find.byKey(ValueKey('group-meta-${sub.id}'))).data!;
    expect(meta, contains('2 configs'));
    expect(meta, contains('Expires ${fmtDate(DateTime.fromMillisecondsSinceEpoch(1798761600 * 1000))}'));
    expect(meta, contains('Updated '));
    // Refresh lives on the group only.
    expect(find.byKey(ValueKey('group-refresh-${sub.id}')), findsOneWidget);
    expect(find.byKey(const ValueKey('subs-refresh')), findsNothing);
    await done(t);
  });

  test('unlimited plan shows ∞ and an empty bar', () {
    const ui = UserInfo(upload: 100, download: 900, total: 0, present: true);
    expect(UsageBar.label(ui), endsWith(' / ∞'));
    expect(ui.usedFraction, 0);
  });

  testWidgets('collapse state is remembered across restarts', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: [vlessReality(), subProfile('p-s1', 's1')], subs: [
      {'id': 's1', 'url': 'https://sub.example.com/x', 'name': 'My sub'}
    ]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    expect(tileIds(), ['p-vless', 'p-s1']);
    await t.tap(find.byKey(const ValueKey('group-toggle-s1')));
    await settle(t);
    expect(tileIds(), ['p-vless']);
    expect((await SharedPreferences.getInstance()).getStringList('collapsedGroups'), ['s1']);
    final again = AppState();
    await again.init();
    expect(again.isCollapsed('s1'), isTrue);
    expect(again.isCollapsed(AppState.manualGroup), isFalse);
    again.dispose();
    await t.tap(find.byKey(const ValueKey('group-toggle-s1')));
    await settle(t);
    expect(tileIds(), ['p-vless', 'p-s1']);
    await done(t);
  });

  testWidgets('test-all and sort work per group', (t) async {
    phone(t, height: 1000);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: [vlessReality(), wireguard(), subProfile('p-s1', 's1'), subProfile('p-s2', 's1')], subs: [
      {'id': 's1', 'url': 'https://sub.example.com/x', 'name': 'My sub'}
    ]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    await t.tap(find.byKey(const ValueKey('group-ping-s1')));
    await settle(t);
    final tested = calls.where((c) => c.method == 'urlTest').map((c) => (jsonDecode((c.arguments as Map)['profile'] as String) as Map)['id']).toList();
    expect(tested..sort(), ['p-s1', 'p-s2'], reason: 'only this group, never AmneziaWG');
    // Sort by delay inside the group.
    state.pings
      ..['p-s1'] = 300
      ..['p-s2'] = 90;
    await t.tap(find.byKey(const ValueKey('group-sort-s1')));
    await settle(t);
    expect(tileIds(), ['p-vless', 'p-wg', 'p-s2', 'p-s1']);
    // AmneziaWG-only content has nothing to ping: the manual group still
    // pings its VLESS row only.
    calls.clear();
    await t.tap(find.byKey(const ValueKey('group-ping-_manual')));
    await settle(t);
    expect(calls.where((c) => c.method == 'urlTest').map((c) => (jsonDecode((c.arguments as Map)['profile'] as String) as Map)['id']), ['p-vless']);
    await done(t);
  });

  testWidgets('delete subscription: confirm, removes its configs, clears a selection inside it', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: [vlessReality(), subProfile('p-s1', 's1')], subs: [
      {'id': 's1', 'url': 'https://sub.example.com/x', 'name': 'My sub'}
    ]);
    await state.select('p-s1');
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    await t.tap(find.byKey(const ValueKey('group-menu-s1')));
    await settle(t, 4);
    await t.tap(find.byKey(const ValueKey('group-delete-s1')));
    await settle(t, 4);
    // Cancel keeps it.
    await t.tap(find.text('Cancel'));
    await settle(t, 4);
    expect(state.subs, hasLength(1));
    await t.tap(find.byKey(const ValueKey('group-menu-s1')));
    await settle(t, 4);
    await t.tap(find.byKey(const ValueKey('group-delete-s1')));
    await settle(t, 4);
    await t.tap(find.byKey(const ValueKey('sub-delete-confirm')));
    await settle(t);
    expect(state.subs, isEmpty);
    expect(state.profiles.map((p) => p.id), ['p-vless']);
    expect(state.selectedId, isNull);
    expect((await SharedPreferences.getInstance()).getString('selected'), isNull);
    expect(find.byKey(const ValueKey('group-s1')), findsNothing);
    expect(state.state, ConnState.disconnected, reason: 'connection state untouched');
    await done(t);
  });

  // ---------- Row subtitle: protocol · transport · security ----------

  test('label is always protocol · transport · security', () {
    String l(Map<String, dynamic> j) => Profile(j).label;
    expect(l(vlessReality()), 'VLESS · TCP · Reality');
    expect(l({'type': 'vless', 'security': 'reality'}), 'VLESS · TCP · Reality', reason: 'no type= in the link → TCP');
    expect(l({'type': 'vless', 'network': 'raw', 'security': 'reality'}), 'VLESS · TCP · Reality');
    expect(l({'type': 'vless', 'network': 'ws', 'security': 'tls'}), 'VLESS · WS · TLS');
    expect(l({'type': 'trojan', 'network': 'grpc', 'security': 'tls'}), 'Trojan · gRPC · TLS');
    expect(l({'type': 'vmess', 'network': 'tcp', 'security': 'none'}), 'VMess · TCP · None');
    expect(l({'type': 'vmess'}), 'VMess · TCP · None');
    expect(l({'type': 'vless', 'network': 'xhttp', 'security': 'tls'}), 'VLESS · XHTTP · TLS');
    expect(l({'type': 'vless', 'network': 'kcp', 'security': 'none'}), 'VLESS · mKCP · None');
    expect(l({'type': 'vless', 'network': 'httpupgrade', 'security': 'tls'}), 'VLESS · HTTPUpgrade · TLS');
    expect(l({'type': 'shadowsocks', 'network': 'tcp', 'security': 'none'}), 'Shadowsocks · TCP · None');
    expect(l({'type': 'hysteria2', 'security': 'tls'}), 'Hysteria2 · QUIC · TLS');
    expect(l({'type': 'amneziawg'}), 'AmneziaWG · UDP · Noise');
    expect(l({'type': 'socks', 'network': 'tcp', 'security': 'none'}), 'SOCKS · TCP · None');
    expect(l({'type': 'socks'}), 'SOCKS · TCP · None');
    expect(l({'type': 'socks', 'security': 'tls'}), 'SOCKS · TCP · TLS');
  });

  for (final compact in [false, true]) {
    testWidgets('server rows and the home card show the same three-part label (compact: $compact)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final ws = trojan()
        ..['id'] = 'p-ws'
        ..['type'] = 'vless'
        ..['uuid'] = '2f1c8a3e-6b4d-4c1a-9e2f-7a8b9c0d1e2f';
      final state = await seeded('fa', profiles: [vlessReality(), ws, trojan()..['network'] = 'grpc'], extra: {'flutter.serversCompact': compact});
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      expect(find.text('VLESS · TCP · Reality'), findsWidgets, reason: 'home card');
      await toServers(t);
      String row(String id) => t.widget<Text>(find.byKey(ValueKey('label-$id'))).data!;
      expect(row('p-vless'), 'VLESS · TCP · Reality');
      expect(row('p-ws'), 'VLESS · WS · TLS');
      expect(row('p-trojan'), 'Trojan · gRPC · TLS');
      expect(t.widget<Text>(find.byKey(const ValueKey('label-p-vless'))).textDirection, TextDirection.ltr);
      await done(t);
    });
  }

  // ---------- Removed protocols ----------

  test('stored SlipNet / Naive profiles are dropped on load (SOCKS5 kept); stack and resolvers ignored', () async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final legacy = [
      vlessReality(),
      dnstt(),
      slipnetSsh(),
      {'id': 'p-socks', 'type': 'socks', 'server': '1.2.3.4', 'port': 1080},
      {'id': 'p-naive', 'type': 'naive', 'server': 'n.example.com', 'port': 443},
      {'id': 'p-stun', 'type': 'ssh', 'server': 's.example.com', 'port': 443, 'ssh': {'ws': true, 'wsTls': true}},
      wireguard(),
    ];
    final state = await seeded('en', profiles: legacy, extra: {
      'flutter.selected': 'p-dnstt',
      'flutter.settings': jsonEncode({'language': 'en', 'stack': 'system', 'experimentalStack': true, 'dnsResolvers': ['1.1.1.1:53']}),
    });
    expect(state.profiles.map((p) => p.id), ['p-vless', 'p-socks', 'p-wg']);
    expect(state.selectedId, 'p-vless');
    final prefs = await SharedPreferences.getInstance();
    expect((jsonDecode(prefs.getString('profiles')!) as List).map((e) => (e as Map)['id']), ['p-vless', 'p-socks', 'p-wg']);
    expect(state.profiles[1].label, 'SOCKS · TCP · None');
    final core = jsonDecode(state.coreSettingsJson()) as Map;
    expect(core.containsKey('stack'), isFalse);
    expect(core.containsKey('experimentalStack'), isFalse);
    expect(state.settings.toJson().containsKey('dnsResolvers'), isFalse);
    _live.remove(state);
    state.dispose();
  });

  for (final lang in ['fa', 'en']) {
    testWidgets('a slipnet / ssh link is rejected with a clear message ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      fakeParseText = jsonEncode({'profiles': [], 'errors': ['slipnet://MjJ8c3No…: err.slipnetRemoved']});
      final r = await state.importText('slipnet://MjJ8c3No');
      expect(r, (0, false, 'err.slipnetRemoved'));
      fakeParseText = jsonEncode({'profiles': [], 'errors': ['naive+https://x…: err.typeRemoved']});
      expect(await state.importText('naive+https://u:p@n.example.com:443'), (0, false, 'err.typeRemoved'));
      expect(S(lang).t('err.typeRemoved'), isNot(contains('SOCKS')));
      // Through the add sheet: the localized text, not the raw key.
      await toServers(t);
      showAddSheet(t.element(find.byType(ServersPage)));
      await settle(t);
      fakeParseText = jsonEncode({'profiles': [], 'errors': ['ssh://u@h: err.slipnetRemoved']});
      await t.enterText(find.byType(TextField).first, 'ssh://u:p@198.51.100.9:22');
      await t.tap(find.text(S(lang).t('add.add')).last);
      await settle(t);
      expect(find.text(S(lang).t('err.slipnetRemoved')), findsOneWidget);
      expect(S(lang).t('err.slipnetRemoved'), contains(lang == 'fa' ? 'SlipNet' : 'no longer supported'));
      expect(state.profiles, hasLength(2));
      await done(t);
    });
  }

  // ---------- Home status text ----------

  for (final (lang, text) in [('fa', 'متصل شد'), ('en', 'Connected')]) {
    testWidgets('home shows "$text" when connected ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
      await settle(t);
      expect(find.text(text), findsWidgets);
      expect(find.text(lang == 'fa' ? 'محافظت شده' : 'Protected'), findsNothing);
      await done(t);
    });
  }

  // ---------- Add sheet: manual + AmneziaWG import ----------

  for (final lang in ['fa', 'en']) {
    testWidgets('add sheet offers "Add manually"; the form lists Xray / sing-box types only ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t);
      await toServers(t);
      showAddSheet(t.element(find.byType(ServersPage)));
      await settle(t);
      final s = S(lang);
      expect(find.text(s.t('add.manual')), findsOneWidget);
      expect(find.text(s.t('add.awgHint')), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('add-manual')));
      await settle(t, 10);
      expect(find.byType(ProfileEditorPage), findsOneWidget);
      expect(find.text(s.t('add.manualTitle')), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('field:type')));
      await settle(t, 5);
      for (final ty in manualTypes) {
        expect(find.text(Profile.protocolName(ty)), findsWidgets, reason: ty);
      }
      expect(find.text('AmneziaWG'), findsNothing);
      expect(manualTypes, contains('socks'));
      for (final gone in ['SSH', 'NaiveProxy', 'DNSTT', 'NoizDNS', 'VayDNS', 'Slipstream']) {
        expect(find.text(gone), findsNothing, reason: gone);
      }
      await done(t);
    });
  }

  testWidgets('AmneziaWG: pasted .conf text is imported into "My configs"', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: [vlessReality()]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    const conf = '[Interface]\nPrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\nAddress = 10.8.0.2/32\nJc = 4\n\n[Peer]\nPublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\nEndpoint = 203.0.113.7:51820\n';
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (c) async {
      if (c.method == 'Clipboard.getData') return {'text': conf};
      return null;
    });
    fakeParseText = jsonEncode({
      'profiles': [wireguard()..['id'] = 'p-awg-paste']
    });
    showAddSheet(t.element(find.byType(ServersPage)));
    await settle(t);
    await t.tap(find.text('Paste from clipboard'));
    await settle(t);
    final pc = calls.lastWhere((c) => c.method == 'parseText');
    expect((pc.arguments as Map)['text'], conf.trim());
    expect(state.profiles.map((p) => p.id), ['p-vless', 'p-awg-paste']);
    expect(state.profiles.last.subId, isNull);
    expect(tileIds(), ['p-vless', 'p-awg-paste']);
    await done(t);
  });

  testWidgets('AmneziaWG: a picked .conf file is imported via parseConf', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: [vlessReality()]);
    await t.pumpWidget(VeilonApp(state: state));
    await settle(t);
    await toServers(t);
    const conf = '[Interface]\nPrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\nAddress = 10.8.0.2/32\n\n[Peer]\nPublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\nEndpoint = 203.0.113.7:51820\n';
    final m = t.binding.defaultBinaryMessenger;
    m.setMockMethodCallHandler(coreChannel, (call) async {
      calls.add(call);
      switch (call.method) {
        case 'pickFile':
          return {'text': conf, 'name': 'home.conf'};
        case 'parseConf':
          return jsonEncode(wireguard()..['id'] = 'p-awg-file'..['name'] = 'home');
        case 'state':
          return {'type': 'state', 'state': 'disconnected'};
        case 'urlTest':
          return jsonEncode({'delay': 50});
        case 'checkProfile':
          return '';
      }
      return null;
    });
    showAddSheet(t.element(find.byType(ServersPage)));
    await settle(t);
    await t.tap(find.text('Open .conf file'));
    await settle(t);
    final pc = calls.lastWhere((c) => c.method == 'parseConf');
    expect((pc.arguments as Map)['name'], 'home.conf');
    expect(state.profiles.map((p) => p.id), ['p-vless', 'p-awg-file']);
    expect(state.profiles.last.type, 'amneziawg');
    await done(t);
  });

  // ---------- Manual add of every Xray / sing-box protocol ----------

  testWidgets('manual add of each protocol saves a profile (checked by the core in Go)', (t) async {
    phone(t, height: 900);
    addTearDown(t.view.reset);
    final state = await seeded('en', profiles: []);
    const uuid = 'b831381d-6324-4d53-ad4f-8cda48b30811';
    const pbk = 'nOgeQ91pGXuAOrULdAGVkC0sBrgLuKWXGq7tj0gqFEA';
    // Values in display order (general → auth → transport → TLS).
    final cases = <String, List<(String, String)>>{
      'vless-reality': [('name', 'vless-reality'), ('server', '198.51.100.1'), ('port', '443'), ('uuid', uuid), ('flow', 'xtls-rprx-vision'),
        ('security', 'reality'), ('sni', 'www.microsoft.com'), ('fp', 'chrome'), ('pbk', pbk), ('sid', '6ba85179')],
      'vless-tls-ws': [('name', 'vless-tls-ws'), ('server', 'cdn.example.com'), ('port', '443'), ('uuid', uuid),
        ('network', 'ws'), ('path', '/ws'), ('host', 'cdn.example.com'), ('security', 'tls'), ('sni', 'cdn.example.com')],
      'vless-grpc': [('name', 'vless-grpc'), ('server', 'g.example.com'), ('port', '443'), ('uuid', uuid),
        ('network', 'grpc'), ('serviceName', 'gsvc'), ('security', 'tls'), ('sni', 'g.example.com')],
      'vless-h2': [('name', 'vless-h2'), ('server', 'h.example.com'), ('port', '443'), ('uuid', uuid),
        ('network', 'h2'), ('path', '/h2'), ('security', 'tls'), ('sni', 'h.example.com')],
      'vless-httpupgrade': [('name', 'vless-httpupgrade'), ('server', 'u.example.com'), ('port', '443'), ('uuid', uuid),
        ('network', 'httpupgrade'), ('path', '/up'), ('host', 'u.example.com'), ('security', 'tls'), ('sni', 'u.example.com')],
      'vless-xhttp': [('name', 'vless-xhttp'), ('server', 'x.example.com'), ('port', '443'), ('uuid', uuid),
        ('network', 'xhttp'), ('path', '/xh'), ('mode', 'auto'), ('security', 'tls'), ('sni', 'x.example.com')],
      // Cleartext VLESS is refused by Xray toward a public address
      // (err.xrayCleartextPublicIP), so the mKCP VLESS case uses a LAN host.
      'vless-mkcp': [('name', 'vless-mkcp'), ('server', '10.20.30.40'), ('port', '8443'), ('uuid', uuid),
        ('network', 'kcp'), ('headerType', 'wechat-video'), ('seed', 'seed123')],
      'vmess-mkcp': [('type', 'vmess'), ('name', 'vmess-mkcp'), ('server', '198.51.100.4'), ('port', '8443'), ('uuid', uuid),
        ('network', 'kcp'), ('headerType', 'wechat-video'), ('seed', 'seed123')],
      'vless-quic': [('name', 'vless-quic'), ('server', '198.51.100.3'), ('port', '443'), ('uuid', uuid),
        ('network', 'quic'), ('security', 'tls'), ('sni', 'q.example.com')],
      'vmess-ws-tls': [('type', 'vmess'), ('name', 'vmess-ws-tls'), ('server', 'vm.example.com'), ('port', '443'), ('uuid', uuid),
        ('vmessSecurity', 'auto'), ('network', 'ws'), ('path', '/vm'), ('security', 'tls'), ('sni', 'vm.example.com')],
      'trojan-tls': [('type', 'trojan'), ('name', 'trojan-tls'), ('server', 't.example.com'), ('port', '443'), ('password', 'trojanpass'),
        ('security', 'tls'), ('sni', 't.example.com')],
      'shadowsocks': [('type', 'shadowsocks'), ('name', 'shadowsocks'), ('server', '198.51.100.5'), ('port', '8388'),
        ('method', 'chacha20-ietf-poly1305'), ('password', 'sspass')],
      'shadowsocks-2022': [('type', 'shadowsocks'), ('name', 'shadowsocks-2022'), ('server', '198.51.100.6'), ('port', '8388'),
        ('method', '2022-blake3-aes-128-gcm'), ('password', 'AAECAwQFBgcICQoLDA0ODw==')],
      'hysteria2': [('type', 'hysteria2'), ('name', 'hysteria2'), ('server', 'hy.example.com'), ('port', '443'), ('password', 'hypass'),
        ('obfsPassword', 'obfspass'), ('sni', 'hy.example.com')],
      'tuic': [('type', 'tuic'), ('name', 'tuic'), ('server', 'tu.example.com'), ('port', '443'), ('uuid', uuid), ('password', 'tupass'),
        ('congestion', 'bbr'), ('sni', 'tu.example.com')],
      'wireguard': [('type', 'wireguard'), ('name', 'wireguard'), ('server', '198.51.100.8'), ('port', '51820'),
        ('wg.privateKey', '0mske3O80qVSmGm2nHiR2us1ZgH8GQwWFCbDgZJmNt4='), ('wg.peerPublicKey', 'uoiOdrOiF/SKQmzpqsctzB7H4J6tS+DT5XXeSeSWVEY='),
        ('wg.address', '10.7.0.2/32'), ('wg.mtu', '1280')],
      'http-tls': [('type', 'http'), ('name', 'http-tls'), ('server', 'p.example.com'), ('port', '443'), ('username', 'u'), ('password', 'p'),
        ('security', 'tls'), ('sni', 'p.example.com')],
      'socks': [('type', 'socks'), ('name', 'socks'), ('server', '198.51.100.14'), ('port', '1080'), ('username', 'su'), ('password', 'sp')],
      'socks-tls': [('type', 'socks'), ('name', 'socks-tls'), ('server', 's5.example.com'), ('port', '443'), ('username', 'su'), ('password', 'sp'),
        ('security', 'tls'), ('sni', 's5.example.com')],
      'mtproto': [('type', 'mtproto'), ('name', 'mtproto'), ('server', '198.51.100.13'), ('port', '443'), ('secret', 'ee00112233445566778899aabbccddeeff')],
    };
    final saved = <String, Map<String, dynamic>>{};

    String labelFor(String path, String v) =>
        path == 'type' ? Profile.protocolName(v) : (path == 'network' ? (networkLabels[v] ?? v) : v);

    for (final MapEntry(key: name, value: fields) in cases.entries) {
      await t.pumpWidget(AppScope(
        state: state,
        child: SScope(
          s: S('en'),
          child: MaterialApp(theme: buildTheme(), home: ProfileEditorPage(key: ValueKey(name), profile: Profile(<String, dynamic>{'type': 'vless'}), isNew: true)),
        ),
      ));
      await settle(t, 4);
      final scroll = find.descendant(of: find.byType(Form), matching: find.byType(Scrollable)).first;
      for (final (path, value) in fields) {
        debugPrint('manual $name: $path');
        final f = find.byKey(ValueKey('field:$path'));
        await t.scrollUntilVisible(f, 200, scrollable: scroll);
        await t.pump();
        final w = t.widget(f);
        if (w is DropdownButtonFormField<String>) {
          await t.tap(f);
          await settle(t, 4);
          await t.tap(find.text(labelFor(path, value)).last);
          await settle(t, 4);
        } else {
          await t.enterText(f, value);
          await t.pump();
        }
      }
      final before = state.profiles.length;
      await t.tap(find.byIcon(Icons.check_rounded));
      await settle(t, 6);
      expect(state.profiles.length, before + 1, reason: '$name was not saved');
      final p = state.profiles.last;
      expect(p.subId, isNull, reason: name);
      expect(p.json['name'], name);
      saved[name] = Map<String, dynamic>.from(p.json)..remove('id');
    }
    // Every saved profile goes into "My configs".
    expect(ServerGroup.of(state).single.id, AppState.manualGroup);
    expect(saved.keys, containsAll(['vless-reality', 'vless-grpc', 'vless-mkcp', 'vless-quic', 'vmess-ws-tls', 'trojan-tls', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'socks', 'socks-tls']));
    expect(saved['socks']!['type'], 'socks');
    expect(saved['socks-tls']!['security'], 'tls');
    expect(saved['vless-reality']!['security'], 'reality');
    expect(saved['vless-mkcp']!['network'], 'kcp');
    expect(saved['wireguard']!['type'], 'wireguard');
    File(manualOut)
      ..createSync(recursive: true)
      ..writeAsStringSync(const JsonEncoder.withIndent('  ').convert(saved));
    await done(t);
  });

  // ---------- Screenshots ----------

  for (final lang in ['fa', 'en']) {
    testWidgets('screens v104 $lang', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang, profiles: [vlessReality(), wireguard(), trojan()..['subId'] = 's1', subProfile('p-s2', 's1')], subs: [
        {'id': 's1', 'url': 'https://panel.example.com:2096/sub/abc', 'title': 'Veilon Panel', 'userInfo': usage,
          'updatedAt': DateTime(2026, 11, 11, 18, 40).millisecondsSinceEpoch}
      ]);
      state.pings['p-vless'] = 142;
      await t.pumpWidget(VeilonApp(state: state));
      await settle(t, 12);
      await toServers(t);
      await settle(t);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/v104_servers_$lang.png'));
      showAddSheet(t.element(find.byType(ServersPage)));
      await settle(t, 10);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/v104_add_sheet_$lang.png'));
      Navigator.of(t.element(find.byType(AddSheet))).pop();
      await settle(t, 6);
      Shell.goTo(t.element(find.byType(TabRail)), 0);
      await settle(t);
      state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch - 65000});
      await settle(t, 12);
      await expectLater(goldenRoot(), matchesGoldenFile('goldens/v104_home_connected_$lang.png'));
      expect(Palette.lime, isNotNull);
      await done(t);
    });
  }
}
