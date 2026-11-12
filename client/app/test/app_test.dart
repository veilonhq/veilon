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

import 'helpers.dart';

Future<AppState> makeState(List<Map<String, dynamic>> profiles, {String lang = 'fa', List<Map<String, dynamic>>? subs}) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode(profiles),
    'flutter.settings': jsonEncode({'language': lang}),
    'flutter.subs': ?(subs == null ? null : jsonEncode(subs)),
  });
  final s = AppState();
  await s.init();
  return s;
}

Widget editorApp(AppState state, Profile p, {String lang = 'fa'}) => AppScope(
      state: state,
      child: SScope(
        s: S(lang),
        child: MaterialApp(
          theme: buildTheme(),
          builder: (c, child) => Directionality(textDirection: S(lang).dir, child: SScope(s: S(lang), child: child!)),
          home: ProfileEditorPage(profile: p),
        ),
      ),
    );

void main() {
  setUp(() {
    calls.clear();
    fakeSubResult = null;
    normalizeError = null;
    exportLink = null;
    fakeLogs = [];
    installNativeMocks();
  });

  testWidgets('app boots in Persian RTL by default', (t) async {
    SharedPreferences.setMockInitialValues({});
    final state = AppState();
    await state.init();
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 500));
    expect(state.settings.language, 'fa');
    final dir = Directionality.of(t.element(find.byType(Scaffold).first));
    expect(dir, TextDirection.rtl);
    expect(find.text('خانه'), findsWidgets);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('editor validates and saves host override + fields', (t) async {
    t.view.physicalSize = const Size(1080, 15000);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    final state = await makeState([vlessReality()], lang: 'en');
    await t.pumpWidget(editorApp(state, state.profiles.first, lang: 'en'));
    await t.pump(const Duration(milliseconds: 300));

    expect(find.text('Reality public key'), findsOneWidget);
    await t.enterText(find.byKey(const ValueKey('field:port')), '70000');
    await t.pump();
    expect(find.textContaining('Out of range'), findsOneWidget);
    await t.tap(find.text('Save').last);
    await t.pump(const Duration(milliseconds: 300));
    expect(state.profiles.first.port, 443, reason: 'invalid form must not save');

    await t.enterText(find.byKey(const ValueKey('field:port')), '8443');
    await t.enterText(find.byKey(const ValueKey('field:addressOverride')), '104.16.1.2');
    await t.enterText(find.byKey(const ValueKey('field:sni')), 'cdn.example.org');
    await t.pump();
    await t.tap(find.text('Save').last);
    await t.pump(const Duration(milliseconds: 500));

    final p = state.profiles.first;
    expect(p.id, 'p-vless');
    expect(p.port, 8443);
    expect(p.json['addressOverride'], '104.16.1.2');
    expect(p.json['sni'], 'cdn.example.org');
    expect(p.json['userEdited'], true);
    expect(p.json.containsKey('link'), isFalse);
    expect(p.json['futureField'], {'keep': true});
    expect(calls.map((c) => c.method), containsAll(['checkProfile', 'profileEdited']));
    // Persisted for the next launch.
    final prefs = await SharedPreferences.getInstance();
    final stored = (jsonDecode(prefs.getString('profiles')!) as List).first as Map;
    expect(stored['addressOverride'], '104.16.1.2');
    expect(stored['port'], 8443);
    // And it is what connect sends to the core.
    expect(jsonDecode(state.effectiveJson(p))['addressOverride'], '104.16.1.2');
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('security switch shows/hides reality fields', (t) async {
    t.view.physicalSize = const Size(1080, 15000);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    final state = await makeState([vlessReality()], lang: 'en');
    await t.pumpWidget(editorApp(state, state.profiles.first, lang: 'en'));
    await t.pump(const Duration(milliseconds: 300));
    expect(find.text('Reality short ID'), findsOneWidget);
    await t.tap(find.byKey(const ValueKey('field:security')));
    await t.pump(const Duration(milliseconds: 500));
    await t.tap(find.text('tls').last);
    await t.pump(const Duration(milliseconds: 500));
    expect(find.text('Reality short ID'), findsNothing);
    expect(find.text('ALPN'), findsOneWidget);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('core normalize error blocks save; exported link replaces stale one', (t) async {
    t.view.physicalSize = const Size(1080, 15000);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    final state = await makeState([vlessReality()], lang: 'en');
    await t.pumpWidget(editorApp(state, state.profiles.first, lang: 'en'));
    await t.pump(const Duration(milliseconds: 300));
    await t.enterText(find.byKey(const ValueKey('field:port')), '2083');
    normalizeError = 'bad profile';
    await t.tap(find.text('Save').last);
    await t.pump(const Duration(milliseconds: 500));
    expect(state.profiles.first.port, 443);
    expect(find.text('bad profile'), findsOneWidget);
    normalizeError = null;
    exportLink = 'vless://edited@de1.example.com:2083';
    await t.tap(find.text('Save').last);
    await t.pump(const Duration(milliseconds: 500));
    expect(state.profiles.first.port, 2083);
    expect(state.profiles.first.link, 'vless://edited@de1.example.com:2083');
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('editor: pick mKCP, add a TCP mask on gRPC, save', (t) async {
    t.view.physicalSize = const Size(1080, 15000);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    final state = await makeState([vlessReality()], lang: 'en');
    await t.pumpWidget(editorApp(state, state.profiles.first, lang: 'en'));
    await t.pump(const Duration(milliseconds: 300));
    expect(find.text('Seed'), findsNothing);
    await t.tap(find.byKey(const ValueKey('field:network')));
    await t.pump(const Duration(milliseconds: 500));
    await t.tap(find.text('mKCP').last);
    await t.pump(const Duration(milliseconds: 500));
    expect(find.text('Seed'), findsOneWidget);
    expect(find.text('TTI (ms)'), findsOneWidget);
    await t.tap(find.byKey(const ValueKey('field:network')));
    await t.pump(const Duration(milliseconds: 500));
    await t.tap(find.text('gRPC').last);
    await t.pump(const Duration(milliseconds: 500));
    expect(find.text('Seed'), findsNothing);
    expect(find.text('Service name'), findsOneWidget);
    await t.enterText(find.byKey(const ValueKey('field:serviceName')), 'veilon');
    await t.tap(find.byKey(const ValueKey('add:finalMask.tcp')));
    await t.pump(const Duration(milliseconds: 300));
    expect(find.text('Layer 1'), findsOneWidget);
    await t.tap(find.text('Save').last);
    await t.pump(const Duration(milliseconds: 500));
    final p = state.profiles.first;
    expect(p.network, 'grpc');
    expect(p.json['serviceName'], 'veilon');
    expect(jsonDecode(p.json['finalMask'] as String), {
      'tcp': [
        {'type': 'header-custom'}
      ]
    });
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  test('battery saver removed: a stored true from 1.0.2 is ignored', () async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final state = await makeState([vlessReality()]);
    // Settings saved by 1.0.2 with the saver on.
    final old = state.settings.toJson()..['batterySaver'] = true;
    state.settings = AppSettings.fromJson(old);
    expect(state.effectiveJson(state.profiles.last), state.profiles.last.encode());
    expect((jsonDecode(state.coreSettingsJson()) as Map).containsKey('batterySaver'), isFalse);
    expect(state.settings.toJson().containsKey('batterySaver'), isFalse);
    state.dispose();
  });

  test('subscription refresh keeps user-edited servers', () async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final edited = vlessReality()
      ..['subId'] = 's1'
      ..['userEdited'] = true
      ..['addressOverride'] = '9.9.9.9'
      ..['port'] = 2053;
    final state = await makeState([edited], subs: [
      {'id': 's1', 'url': 'https://panel.example/sub/abc'}
    ]);
    fakeSubResult = {
      'status': 200,
      'profiles': [
        vlessReality(),
        {'id': 'p-new', 'name': 'New', 'type': 'trojan', 'server': 'n.example.com', 'port': 443, 'password': 'x'},
      ],
    };
    final err = await state.updateSubscription(state.subs.first);
    expect(err, isNull);
    expect(state.profiles.length, 2);
    final p = state.profiles.firstWhere((p) => p.id == 'p-vless');
    expect(p.port, 2053);
    expect(p.json['addressOverride'], '9.9.9.9');
    state.dispose();
  });
}
