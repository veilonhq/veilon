// Navigation, rapid-tap and layout robustness tests for v1.0.1.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/errors.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/pages/home_page.dart';
import 'package:veilon/ui/pages/servers_page.dart';
import 'package:veilon/ui/pages/settings_page.dart';
import 'package:veilon/ui/pages/stats_page.dart';
import 'package:veilon/ui/shell.dart';
import 'package:veilon/ui/widgets/portal.dart';

import 'helpers.dart';

Future<AppState> seeded(String lang) async {
  SharedPreferences.setMockInitialValues({
    'flutter.profiles': jsonEncode([vlessReality(), wireguard(), trojan()]),
    'flutter.settings': jsonEncode({'language': lang}),
    'flutter.pings': jsonEncode({'p-vless': 142, 'p-wg': -1}),
    'flutter.selected': 'p-vless',
  });
  final s = AppState();
  await s.init();
  s.pings.addAll({'p-vless': 142}); // pings are measured per session, never restored
  return s;
}

void phone(WidgetTester t, {double width = 360, double height = 780}) {
  t.view.physicalSize = Size(width * 3, height * 3);
  t.view.devicePixelRatio = 3;
}

/// Pages currently on stage (Offstage pages are skipped by finders).
List<Type> onstage() => [
      if (find.byType(HomePage).evaluate().isNotEmpty) HomePage,
      if (find.byType(ServersPage).evaluate().isNotEmpty) ServersPage,
      if (find.byType(StatsPage).evaluate().isNotEmpty) StatsPage,
      if (find.byType(SettingsPage).evaluate().isNotEmpty) SettingsPage,
    ];

Future<void> tapTab(WidgetTester t, int i) => t.tap(find.byKey(ValueKey('tab-$i')), warnIfMissed: false);

void main() {
  setUp(() {
    calls.clear();
    installNativeMocks();
  });

  for (final lang in ['fa', 'en']) {
    testWidgets('tab tap jumps directly, never through the pages in between ($lang)', (t) async {
      phone(t);
      addTearDown(t.view.reset);
      final state = await seeded(lang);
      await t.pumpWidget(VeilonApp(state: state));
      await t.pump(const Duration(milliseconds: 300));
      expect(onstage(), [HomePage]);

      // Home → Settings: every frame shows only Home (fading out) and Settings.
      await tapTab(t, 3);
      for (var f = 0; f < 30; f++) {
        await t.pump(const Duration(milliseconds: 16));
        final on = onstage();
        expect(on.contains(ServersPage) || on.contains(StatsPage), isFalse, reason: 'frame $f: $on');
        expect(on.contains(SettingsPage), isTrue);
      }
      expect(onstage(), [SettingsPage]);
      expect(Shell.indexOf(t.element(find.byType(TabRail))), 3);

      // Settings → Home, and every other pair.
      for (final (a, b) in [(3, 0), (0, 2), (2, 1), (1, 3), (3, 1), (1, 0)]) {
        expect(Shell.indexOf(t.element(find.byType(TabRail))), a);
        await tapTab(t, b);
        for (var f = 0; f < 25; f++) {
          await t.pump(const Duration(milliseconds: 16));
          expect(onstage().length, lessThanOrEqualTo(2));
        }
        expect(Shell.indexOf(t.element(find.byType(TabRail))), b);
      }
      await t.pump(const Duration(seconds: 4));
      state.dispose();
    });
  }

  testWidgets('rapid tab taps settle on the last tab with at most two pages on stage', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('fa');
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    for (final i in [1, 3, 2, 0, 3, 1, 2, 3]) {
      await tapTab(t, i);
      await t.pump(const Duration(milliseconds: 30));
      expect(onstage().length, lessThanOrEqualTo(2));
    }
    await t.pump(const Duration(milliseconds: 400));
    expect(onstage(), [SettingsPage]);
    expect(Shell.indexOf(t.element(find.byType(TabRail))), 3);
    // Hidden pages keep their state (still mounted, offstage).
    expect(find.byType(HomePage, skipOffstage: false), findsOneWidget);
    expect(find.byType(ServersPage, skipOffstage: false), findsOneWidget);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  testWidgets('rapid connect taps start exactly one connect', (t) async {
    phone(t);
    addTearDown(t.view.reset);
    final state = await seeded('en');
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    for (var i = 0; i < 5; i++) {
      await t.tap(find.byType(Portal));
      await t.pump(const Duration(milliseconds: 40));
    }
    await t.pump(const Duration(milliseconds: 500));
    expect(calls.where((c) => c.method == 'connect').length, 1);
    expect(calls.where((c) => c.method == 'disconnect').length, 0);
    expect(state.state, ConnState.connecting);
    // Two direct toggles racing each other: the second is ignored.
    final a = state.toggle();
    final b = state.toggle();
    await Future.wait([a, b]);
    expect(calls.where((c) => c.method == 'disconnect').length, 1);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });

  for (final lang in ['fa', 'en']) {
    for (final w in [320.0, 360.0, 412.0]) {
      testWidgets('no overflow on any page at ${w.toInt()}dp ($lang)', (t) async {
        phone(t, width: w, height: 640);
        addTearDown(t.view.reset);
        final state = await seeded(lang);
        state.handleEvent({'type': 'state', 'state': 'error', 'error': 'start: initialize cache-file: open cache.db: read-only file system'});
        await t.pumpWidget(VeilonApp(state: state));
        await t.pump(const Duration(milliseconds: 400));
        for (final i in [1, 2, 3, 0]) {
          await tapTab(t, i);
          await t.pump(const Duration(milliseconds: 400));
          // Scroll each page to the end so every row is laid out.
          final lists = find.byType(Scrollable);
          if (lists.evaluate().isNotEmpty) {
            await t.drag(lists.first, const Offset(0, -2000), warnIfMissed: false);
            await t.pump(const Duration(milliseconds: 400));
          }
        }
        // Any overflow / exception during the walk fails the test with details.
        await t.pump(const Duration(seconds: 4));
        state.dispose();
      });
    }
  }

  test('raw errors map to friendly keys', () {
    expect(friendlyErrorKey('start: initialize cache-file: open cache.db: read-only file system'), 'err.friendly.storage');
    expect(friendlyErrorKey('err.xrayCleartextPublicIP'), 'err.xrayCleartextPublicIP');
    expect(friendlyErrorKey('start: err.singboxNeedsXray'), 'err.singboxNeedsXray');
    expect(friendlyErrorKey('denied'), 'err.vpnDenied');
    expect(friendlyErrorKey('amneziawg: no handshake with 1.2.3.4:51820 (server unreachable...)'), 'err.friendly.handshake');
    expect(friendlyErrorKey('dial tcp: i/o timeout'), 'err.friendly.timeout');
    expect(friendlyErrorKey('dial tcp: lookup x.example: no such host'), 'err.friendly.dns');
    expect(friendlyErrorKey('x509: certificate signed by unknown authority'), 'err.friendly.tls');
    expect(friendlyErrorKey('something odd'), 'err.friendly.generic');
  });
}
