// Run 105: a failed one-time proxy check (stats 'proxy' == 'unreachable')
// shows an error on the home card instead of normal traffic; real traffic
// from the server ('ok') clears it. Connection state is unchanged.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';
import 'package:veilon/main.dart';

import 'helpers.dart';

void main() {
  setUp(() {
    calls.clear();
    installNativeMocks();
  });

  testWidgets('server unreachable replaces normal traffic on the home card', (t) async {
    t.view.physicalSize = const Size(360 * 3, 780 * 3);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    SharedPreferences.setMockInitialValues({
      'flutter.profiles': jsonEncode([vlessReality(), trojan(), wireguard()]),
      'flutter.settings': jsonEncode({'language': 'en'}),
      'flutter.selected': 'p-vless',
    });
    final state = AppState();
    await state.init();
    await t.pumpWidget(VeilonApp(state: state));
    await t.pump(const Duration(milliseconds: 300));
    state.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': DateTime.now().millisecondsSinceEpoch});
    state.handleEvent({'type': 'stats', 'down': 0.0, 'up': 0.0, 'sessionDown': 0, 'sessionUp': 0, 'proxy': 'unreachable'});
    await t.pump(const Duration(milliseconds: 300));
    expect(state.state, ConnState.connected);
    expect(state.serverUnreachable, isTrue);
    expect(find.byKey(const ValueKey('server-unreachable')), findsOneWidget);
    state.handleEvent({'type': 'stats', 'down': 2048.0, 'up': 512.0, 'sessionDown': 4096, 'sessionUp': 1024, 'proxy': 'ok'});
    await t.pump(const Duration(milliseconds: 300));
    expect(state.serverUnreachable, isFalse);
    expect(find.byKey(const ValueKey('server-unreachable')), findsNothing);
    state.handleEvent({'type': 'stats', 'down': 0.0, 'up': 0.0, 'sessionDown': 0, 'sessionUp': 0, 'proxy': 'unreachable'});
    state.handleEvent({'type': 'state', 'state': 'disconnected'});
    await t.pump(const Duration(milliseconds: 300));
    expect(state.serverUnreachable, isFalse);
    await t.pump(const Duration(seconds: 4));
    state.dispose();
  });
}
