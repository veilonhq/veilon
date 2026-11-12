// Picking another config while connected switches the live session to it.
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';

import 'helpers.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    calls.clear();
    installNativeMocks();
  });

  Future<AppState> boot() async {
    SharedPreferences.setMockInitialValues({
      'flutter.profiles': jsonEncode([vlessReality(), trojan()]),
      'flutter.settings': jsonEncode({'language': 'en', 'autoSelectFastest': true}),
      'flutter.selected': 'p-vless',
    });
    final s = AppState();
    await s.init();
    return s;
  }

  List<String> connectedIds() => calls
      .where((c) => c.method == 'connect')
      .map((c) => (jsonDecode((c.arguments as Map)['profile'] as String) as Map)['id'] as String)
      .toList();

  test('select while connected reconnects with the new config', () async {
    final s = await boot();
    s.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': 1});
    calls.clear();
    await s.select('p-trojan');
    expect(s.selectedId, 'p-trojan');
    expect(connectedIds(), ['p-trojan']);
    expect(calls.where((c) => c.method == 'ping' || c.method == 'pingAll'), isEmpty, reason: 'auto-fastest must not override the pick');
  });

  test('select while disconnected only selects', () async {
    final s = await boot();
    calls.clear();
    await s.select('p-trojan');
    expect(connectedIds(), isEmpty);
  });

  test('re-selecting the live config does not reconnect', () async {
    final s = await boot();
    s.handleEvent({'type': 'state', 'state': 'connected', 'profileId': 'p-vless', 'since': 1});
    calls.clear();
    await s.select('p-vless');
    expect(connectedIds(), isEmpty);
  });
}
