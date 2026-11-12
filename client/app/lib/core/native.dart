import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Bridge to the Kotlin side (VpnService + Go core).
class Native {
  Native._();
  static final Native instance = Native._();

  static const _m = MethodChannel('app.veilon/core');
  static const _e = EventChannel('app.veilon/events');

  Stream<Map<String, dynamic>>? _events;

  /// Native events. Malformed events are dropped and stream errors are
  /// swallowed (1.0.6): one bad event must not end the subscription.
  Stream<Map<String, dynamic>> get events => _events ??= _e
      .receiveBroadcastStream()
      .handleError((Object _) {})
      .where((e) => e is Map)
      .map((e) => Map<String, dynamic>.from(e as Map))
      .asBroadcastStream();

  /// Tests install a fresh mock event channel per test; drop the cached stream
  /// so the next listener subscribes to it.
  @visibleForTesting
  void debugResetEvents() => _events = null;

  /// Every channel call. A missing plugin, a native error reply or a
  /// wrong result type yields null (1.0.6: never an uncaught exception).
  Future<T?> _call<T>(String method, [Map<String, Object?>? args]) async {
    try {
      return await _m.invokeMethod<T>(method, args);
    } on MissingPluginException {
      return null;
    } on PlatformException catch (e) {
      lastError = '$method: ${e.message ?? e.code}';
      return null;
    } catch (e) {
      lastError = '$method: $e';
      return null;
    }
  }

  /// The last native call failure (for diagnostics).
  String? lastError;

  Future<Map<String, dynamic>> deviceInfo() async =>
      Map<String, dynamic>.from(await _call<Map>('deviceInfo') ?? const {});

  Future<Map<String, dynamic>> parseText(String text) async =>
      _decode(await _call<String>('parseText', {'text': text}));

  Future<Map<String, dynamic>> parseConf(String text, String name) async =>
      _decode(await _call<String>('parseConf', {'text': text, 'name': name}));

  /// 1.0.8 (H1): approved certificate pins `{"ip:port": "<sha256 hex>"}`.
  Future<void> setSubPins(String pinsJson) => _call('setSubPins', {'pins': pinsJson});

  /// 1.0.8: native window colour / starting-window theme for 'dark' | 'light' | 'system'.
  Future<void> setTheme(String mode) => _call('setTheme', {'mode': mode});

  Future<Map<String, dynamic>> fetchSubscription(String url, Map<String, dynamic> device) async =>
      _decode(await _call<String>('fetchSub', {'url': url, 'device': jsonEncode(device)}));

  /// Real delay in ms, -1 on failure. Returns (delay, error).
  Future<(int, String)> urlTest(String profile, String settings, String url, int timeoutMs) async {
    final r = _decode(await _call<String>('urlTest',
        {'profile': profile, 'settings': settings, 'url': url, 'timeout': timeoutMs}));
    return ((r['delay'] as num?)?.toInt() ?? -1, r['error'] as String? ?? '');
  }

  Future<String> checkProfile(String profile, String settings) async =>
      await _call<String>('checkProfile', {'profile': profile, 'settings': settings}) ?? '';

  Future<String> coreFor(String profile) async => await _call<String>('coreFor', {'profile': profile}) ?? '';

  /// Engine under the user's core choice: (core, error key/text or '').
  Future<(String, String)> resolveCore(String profile, String settings) async {
    final r = _decode(await _call<String>('resolveCore', {'profile': profile, 'settings': settings}));
    return (r['core'] as String? ?? '', r['error'] as String? ?? '');
  }

  /// Generated core config (debug/export) or {"error":...}.
  Future<String> buildConfig(String profile, String settings) async =>
      await _call<String>('buildConfig', {'profile': profile, 'settings': settings}) ?? '';

  /// Core edit round-trip (NormalizeProfile). Returns the canonical profile
  /// map, {'error': ...}, or null when the bundled core predates the API.
  Future<Map<String, dynamic>?> normalizeProfile(String profile) async {
    final r = await _call<String>('normalizeProfile', {'profile': profile});
    return r == null || r.isEmpty ? null : _decode(r);
  }

  /// Share link for an (edited) profile, or null (unsupported / not exportable).
  Future<String?> profileToLink(String profile) async {
    final r = _decode(await _call<String>('profileToLink', {'profile': profile}));
    final l = r['link'];
    return l is String && l.isNotEmpty ? l : null;
  }

  /// Tells the native side a profile was edited so the stored last-connect
  /// profile (used by the quick tile and boot reconnect) follows the edit.
  Future<bool> profileEdited(String profile) async =>
      await _call<bool>('profileEdited', {'profile': profile}) ?? false;

  /// Starts the VPN. Returns null on success, or an error code / message.
  Future<String?> connect(String profile, String settings, Map<String, dynamic> serviceOptions) async {
    try {
      return await _m.invokeMethod<String>(
          'connect', {'profile': profile, 'settings': settings, 'options': jsonEncode(serviceOptions)});
    } on MissingPluginException {
      return null;
    } on PlatformException catch (e) {
      return e.message ?? e.code;
    } catch (e) {
      return '$e';
    }
  }

  Future<void> disconnect() => _call('disconnect');

  Future<Map<String, dynamic>> state() async => Map<String, dynamic>.from(await _call<Map>('state') ?? const {});

  Future<void> saveServiceOptions(Map<String, dynamic> options) =>
      _call('saveServiceOptions', {'options': jsonEncode(options)});

  Future<List<Map<String, dynamic>>> logs(int after) async {
    final s = await _call<String>('logs', {'after': after});
    if (s == null || s.isEmpty) return const [];
    try {
      return (jsonDecode(s) as List).map((e) => Map<String, dynamic>.from(e as Map)).toList();
    } catch (_) {
      return const [];
    }
  }

  Future<void> clearLogs() => _call('clearLogs');

  Future<List<Map<String, dynamic>>> installedApps() async {
    final l = await _call<List>('installedApps') ?? const [];
    return l.map((e) => Map<String, dynamic>.from(e as Map)).toList();
  }

  Future<Uint8List?> appIcon(String pkg) => _call<Uint8List>('appIcon', {'package': pkg});

  /// Opens the system file picker; returns {name, text} or null.
  Future<Map<String, dynamic>?> pickConfFile() async {
    final r = await _call<Map>('pickFile');
    return r == null ? null : Map<String, dynamic>.from(r);
  }

  /// Opens the native QR scanner; returns the decoded text or null.
  Future<String?> scanQr() => _call<String>('scanQr');

  Future<bool> openUrl(String url) async => await _call<bool>('openUrl', {'url': url}) ?? false;

  Future<void> openVpnSettings() => _call('openVpnSettings');

  // ---- in-app update (1.0.7) ----

  /// ABI folders (`lib/<abi>/`) of the installed APK(s).
  Future<List<String>> installedAbis() async =>
      ((await _call<List>('installedAbis')) ?? const []).map((e) => '$e').toList();

  /// Folder for downloaded updates (app cache).
  Future<String?> updateDir() => _call<String>('updateDir');

  /// sha256 (lower-case hex) of a file in the update folder ('' if missing).
  Future<String> sha256File(String path) async => await _call<String>('sha256File', {'path': path}) ?? '';

  /// Whether the app may open the system installer ("install unknown apps").
  Future<bool> canInstallApks() async => await _call<bool>('canInstallApks') ?? false;

  /// Starts the system installer: 'ok', 'permission' (settings page opened)
  /// or an error text.
  Future<String> installApk(String path) async =>
      await _call<String>('installApk', {'path': path}) ?? (lastError ?? 'install failed');

  Future<Map<String, dynamic>> versions() async => _decode(await _call<String>('versions'));

  Map<String, dynamic> _decode(String? s) {
    if (s == null || s.isEmpty) return {};
    try {
      final v = jsonDecode(s);
      return v is Map ? Map<String, dynamic>.from(v) : {'value': v};
    } catch (_) {
      return {'error': 'invalid native reply'};
    }
  }
}
