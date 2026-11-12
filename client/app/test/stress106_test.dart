// 1.0.6: rapid connect / disconnect from the UI side. The native side is a
// fake service that answers like VeilonVpnService (connecting → connected
// after a delay, disconnecting → disconnected), with its events arriving
// late and interleaved with the next taps.
import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/models.dart';

import 'helpers.dart';

/// Fake service: state machine + delayed events, records overlap.
class FakeService {
  String state = 'disconnected';
  String? profileId;
  int connects = 0, disconnects = 0;
  int inFlight = 0, maxInFlight = 0;
  bool throwOnConnect = false;
  final _timers = <Timer>[];

  void _emit(String s, {String? id}) {
    state = s;
    if (id != null) profileId = id;
    eventSink?.success({'type': 'state', 'state': s, 'profileId': profileId, 'since': s == 'connected' ? 1 : 0, 'error': ''});
  }

  void _later(int ms, void Function() f) => _timers.add(Timer(Duration(milliseconds: ms), f));

  Future<Object?> handle(MethodCall call) async {
    switch (call.method) {
      case 'connect':
        inFlight++;
        maxInFlight = inFlight > maxInFlight ? inFlight : maxInFlight;
        await Future<void>.delayed(const Duration(milliseconds: 2));
        inFlight--;
        if (throwOnConnect) throw PlatformException(code: 'native', message: 'boom');
        connects++;
        final id = (jsonDecode((call.arguments as Map)['profile'] as String) as Map)['id'] as String;
        _emit('connecting', id: id);
        _later(15, () {
          if (state == 'connecting') _emit('connected');
        });
        return null;
      case 'disconnect':
        inFlight++;
        maxInFlight = inFlight > maxInFlight ? inFlight : maxInFlight;
        await Future<void>.delayed(const Duration(milliseconds: 2));
        inFlight--;
        disconnects++;
        if (state != 'disconnected') _emit('disconnecting');
        _later(10, () => _emit('disconnected'));
        return null;
      case 'state':
        return {'type': 'state', 'state': state};
    }
    return null;
  }

  void dispose() {
    for (final t in _timers) {
      t.cancel();
    }
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late FakeService svc;

  setUp(() {
    calls.clear();
    installNativeMocks();
    svc = FakeService();
    final m = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    m.setMockMethodCallHandler(coreChannel, (call) async {
      calls.add(call);
      final r = await svc.handle(call);
      if (call.method == 'deviceInfo') return {'assetDir': '/data/rules', 'package': 'app.veilon'};
      return r;
    });
  });
  tearDown(() => svc.dispose());

  Future<AppState> boot() async {
    SharedPreferences.setMockInitialValues({
      'flutter.profiles': jsonEncode([vlessReality(), trojan()]),
      'flutter.settings': jsonEncode({'language': 'en', 'autoSelectFastest': false}),
      'flutter.selected': 'p-vless',
    });
    final s = AppState();
    await s.init();
    return s;
  }

  Future<void> settleEvents() => Future<void>.delayed(const Duration(milliseconds: 80));

  test('60 rapid toggles (taps not awaited): never overlapping, never throws, ends consistent', () async {
    final s = await boot();
    final futures = <Future<String?>>[];
    for (var i = 0; i < 60; i++) {
      futures.add(s.toggle());
      // tap faster than the service answers
      await Future<void>.delayed(Duration(milliseconds: i % 4));
    }
    final results = await Future.wait(futures);
    await settleEvents();
    expect(svc.maxInFlight, 1, reason: 'connect and disconnect handed to the service at the same time');
    expect(results.whereType<String>().where((e) => e.isNotEmpty), isEmpty);
    expect(svc.connects, greaterThan(0));
    // The UI ends in the service's final state.
    expect(s.state, connStateFrom(svc.state));
    expect(s.state == ConnState.connected || s.state == ConnState.disconnected, isTrue, reason: 'stuck in ${s.state}');
  });

  test('100 sequential toggles waiting only for the call, not the events', () async {
    final s = await boot();
    for (var i = 0; i < 100; i++) {
      await s.toggle();
      await Future<void>.delayed(const Duration(milliseconds: 1));
    }
    await settleEvents();
    expect(svc.maxInFlight, 1);
    expect(s.state, connStateFrom(svc.state));
  });

  test('disconnect while still connecting', () async {
    final s = await boot();
    await s.toggle(); // connect
    expect(s.state, ConnState.connecting);
    await s.toggle(); // disconnect before "connected" arrives
    await settleEvents();
    expect(svc.disconnects, 1);
    expect(s.state, ConnState.disconnected);
  });

  test('switching config while disconnecting does not reconnect', () async {
    final s = await boot();
    await s.toggle();
    await settleEvents();
    expect(s.state, ConnState.connected);
    await s.disconnect();
    await s.select('p-trojan');
    await settleEvents();
    expect(svc.connects, 1, reason: 'a switch during disconnect must not start a new session');
    expect(s.state, ConnState.disconnected);
  });

  test('switch during connecting and rapid switches settle on the last pick', () async {
    final s = await boot();
    await s.toggle();
    for (var i = 0; i < 20; i++) {
      unawaited(s.select(i.isEven ? 'p-trojan' : 'p-vless'));
      await Future<void>.delayed(const Duration(milliseconds: 1));
    }
    await settleEvents();
    expect(svc.maxInFlight, 1);
    expect(s.state, ConnState.connected);
  });

  test('native errors and malformed events never throw', () async {
    final s = await boot();
    svc.throwOnConnect = true;
    final err = await s.toggle();
    expect(err, isNotNull);
    expect(s.state, ConnState.error);
    for (final e in <Map<String, dynamic>>[
      {'type': 'state', 'state': 5},
      {'type': 'state', 'since': 'x'},
      {'type': 'stats', 'down': 'fast'},
      {'type': null},
      {},
    ]) {
      s.handleEvent(e);
    }
    eventSink?.success('not a map');
    eventSink?.error(code: 'E', message: 'stream error');
    await settleEvents();
    svc.throwOnConnect = false;
    expect(await s.toggle(), isNull);
    await settleEvents();
    expect(s.state, ConnState.connected, reason: 'events still flow after bad ones');
  });
}
