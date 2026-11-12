import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/widgets.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'models.dart';
import 'native.dart';

/// Speed sample (bytes per second).
class SpeedSample {
  const SpeedSample(this.down, this.up);
  final double down;
  final double up;
}

enum PingState { idle, testing, done }

/// Global app state.
class AppState extends ChangeNotifier {
  AppState({Native? native}) : _native = native ?? Native.instance;

  final Native _native;
  SharedPreferences? _prefs;

  List<Profile> profiles = [];
  List<Subscription> subs = [];
  String? selectedId;
  AppSettings settings = AppSettings();
  final Map<String, int> pings = {}; // id -> ms (-1 failed)
  final Map<String, String> pingErrors = {};
  final Set<String> testing = {};
  Map<String, dynamic> device = {};

  ConnState state = ConnState.disconnected;
  String lastError = '';
  DateTime? connectedSince;
  String? connectedProfileId;
  String appliedFingerprint = '';

  static const historyLen = 60;
  final List<SpeedSample> history = List.filled(historyLen, const SpeedSample(0, 0), growable: true);
  double downSpeed = 0, upSpeed = 0, peakDown = 0, peakUp = 0;
  int sessionDown = 0, sessionUp = 0;

  /// The core's one-time proxy check failed and nothing came back from the
  /// server since (stats event 'proxy' == 'unreachable'): the home card shows
  /// an error instead of traffic. Connection state itself is unchanged.
  bool serverUnreachable = false;

  bool busyUpdating = false;
  StreamSubscription? _eventsSub;

  /// 1.0.8 (audit M2): the service lost the connection with the kill switch
  /// on and auto reconnect off, and keeps all traffic blocked (state error).
  /// The connect button then disconnects (releases the block).
  bool blocking = false;

  /// 1.0.8 (audit H1): certificates the user approved for literal-IP
  /// subscription servers, "ip:port" → SHA-256 of the leaf certificate.
  final Map<String, String> subPins = {};

  /// Asks the user whether to trust a subscription server's certificate
  /// (set by the shell; null in tests that do not exercise it). [changed]:
  /// a different certificate was approved before for this server.
  Future<bool> Function(String server, String fingerprint, bool changed)? confirmCert;
  int _certPrompts = 0;
  final List<void Function()> _afterCertPrompt = [];

  /// Ticks once per stats event (speeds, history, session totals,
  /// serverUnreachable). 1.0.7: stats no longer go through
  /// [notifyListeners], which rebuilt the whole app (every page, the
  /// MaterialApp and its theme) once a second while connected; only the
  /// widgets that show these numbers listen here (see [StatsBuilder]).
  final ValueNotifier<int> statsTick = ValueNotifier<int>(0);

  Profile? get selected {
    for (final p in profiles) {
      if (p.id == selectedId) return p;
    }
    return profiles.isEmpty ? null : profiles.first;
  }

  Subscription? subOf(Profile? p) {
    if (p?.subId == null) return null;
    for (final s in subs) {
      if (s.id == p!.subId) return s;
    }
    return null;
  }

  bool get isOn => state == ConnState.connected || state == ConnState.connecting || (state == ConnState.error && blocking);
  bool get needsReconnect => state == ConnState.connected && appliedFingerprint != settings.coreFingerprint();

  Future<void> init() async {
    _prefs = await SharedPreferences.getInstance();
    _load();
    try {
      device = await _native.deviceInfo();
    } catch (_) {}
    _eventsSub = _native.events.listen(_onEvent, onError: (_) {});
    try {
      final st = await _native.state();
      _applyState(st);
    } catch (_) {}
    // Subscriptions are never fetched automatically (not on open, not on
    // resume, not periodically): only when the user taps refresh.
    notifyListeners();
  }

  @override
  void dispose() {
    _eventsSub?.cancel();
    statsTick.dispose();
    super.dispose();
  }

  // ---------- persistence ----------

  void _load() {
    final p = _prefs!;
    try {
      final pj = p.getString('profiles');
      if (pj != null) {
        profiles = (jsonDecode(pj) as List).map((e) => Profile.fromJson(e as Map<String, dynamic>)).toList();
        // 1.0.4: SlipNet-family / Naive profiles were removed (SOCKS5 stays).
        final dropped = profiles.where((x) => x.isRemoved).map((x) => x.id).toSet();
        if (dropped.isNotEmpty) {
          profiles.removeWhere((x) => x.isRemoved);
          p.setString('profiles', jsonEncode(profiles.map((e) => e.json).toList()));
          if (dropped.contains(p.getString('selected'))) {
            profiles.isEmpty ? p.remove('selected') : p.setString('selected', profiles.first.id);
          }
        }
      }
      final sj = p.getString('subs');
      if (sj != null) {
        subs = (jsonDecode(sj) as List).map((e) => Subscription.fromJson(e as Map<String, dynamic>)).toList();
      }
      final st = p.getString('settings');
      if (st != null) settings = AppSettings.fromJson(jsonDecode(st) as Map<String, dynamic>);
      final pins = p.getString('subPins');
      if (pins != null) {
        subPins
          ..clear()
          ..addAll((jsonDecode(pins) as Map).map((k, v) => MapEntry('$k', '$v')));
      }
      // Pings are not restored: a shown delay is always measured in this
      // session, never a cached old value.
      p.remove('pings');
    } catch (e) {
      debugPrint('load: $e');
    }
    selectedId = p.getString('selected');
    collapsedGroups = (p.getStringList('collapsedGroups') ?? const <String>[]).toSet();
    serversCompact = p.getBool('serversCompact') ?? false;
  }

  /// Servers page view mode: false = full cards, true = compact rows.
  bool serversCompact = false;

  Future<void> setServersCompact(bool v) async {
    if (serversCompact == v) return;
    serversCompact = v;
    notifyListeners();
    await _prefs?.setBool('serversCompact', v);
  }

  /// Servers page groups ([manualGroup] or a subscription id) the user
  /// collapsed; saved in prefs as `collapsedGroups`.
  Set<String> collapsedGroups = {};

  /// Group id of the configs added by hand or imported singly.
  static const manualGroup = '_manual';

  bool isCollapsed(String group) => collapsedGroups.contains(group);

  Future<void> toggleGroup(String group) async {
    collapsedGroups.contains(group) ? collapsedGroups.remove(group) : collapsedGroups.add(group);
    notifyListeners();
    await _prefs?.setStringList('collapsedGroups', collapsedGroups.toList()..sort());
  }


  Future<void> _saveProfiles() async {
    await _prefs?.setString('profiles', jsonEncode(profiles.map((e) => e.json).toList()));
  }

  Future<void> _saveSubs() async => _prefs?.setString('subs', jsonEncode(subs.map((e) => e.toJson()).toList()));

  /// 1.0.8: theme setting ('dark' | 'light' | 'system'). The switch itself
  /// is ThemeHost's cross-fade (it listens to settings.theme).
  Future<void> setTheme(String mode) async {
    if (!const ['dark', 'light', 'system'].contains(mode) || settings.theme == mode) return;
    settings.theme = mode;
    await saveSettings();
  }

  /// The palette changed: native window colour / next starting window.
  void nativeThemeChanged(String mode) {
    _native.setTheme(mode).catchError((_) {});
  }

  Future<void> saveSettings() async {
    await _prefs?.setString('settings', jsonEncode(settings.toJson()));
    await _native.saveServiceOptions(_serviceOptions());
    notifyListeners();
  }

  Future<void> select(String id, {bool switchLive = true}) async {
    final changed = id != selectedId;
    selectedId = id;
    await _prefs?.setString('selected', id);
    notifyListeners();
    // Picking another config while the VPN is up switches the live session
    // to it (the core closes the old session and starts the new one).
    if (switchLive && changed) await _switchLive();
  }

  bool _switching = false;

  Future<void> _switchLive() async {
    final p = selected;
    if (p == null || p.isMTProto || _switching || _toggling) return;
    if (state != ConnState.connected && state != ConnState.connecting) return;
    if (connectedProfileId != null && connectedProfileId == p.id) return;
    _switching = true;
    try {
      await connect(skipAutoFastest: true);
    } finally {
      _switching = false;
    }
  }

  // ---------- connection ----------

  /// Profile JSON as sent to the core.
  String effectiveJson(Profile p) => p.encode();

  String coreSettingsJson() => jsonEncode(settings.toCoreJson(
        assetDir: device['assetDir'] as String? ?? '',
        appPackage: device['package'] as String? ?? '',
      ));

  Map<String, dynamic> _serviceOptions() => {
        'killSwitch': settings.killSwitch,
        'autoReconnect': settings.autoReconnect,
        'autoConnect': settings.autoConnect,
        'language': settings.language,
      };

  /// Feeds one native event (as sent on app.veilon/events); for tests.
  @visibleForTesting
  void handleEvent(Map<String, dynamic> e) => _onEvent(e);

  void _onEvent(Map<String, dynamic> e) {
    // A malformed event is dropped (1.0.6): it must not break the stream
    // or the UI.
    try {
      switch (e['type']) {
        case 'state':
          _applyState(e);
        case 'stats':
          _applyStats(e);
      }
    } catch (_) {}
  }

  void _applyState(Map<String, dynamic> e) {
    final prev = state;
    state = connStateFrom(e['state'] as String?);
    lastError = e['error'] as String? ?? '';
    blocking = state == ConnState.error && e['blocking'] == true;
    connectedProfileId = e['profileId'] as String?;
    final since = (e['since'] as num?)?.toInt() ?? 0;
    connectedSince = since > 0 ? DateTime.fromMillisecondsSinceEpoch(since) : null;
    if (state == ConnState.connected && prev != ConnState.connected) {
      sessionDown = (e['sessionDown'] as num?)?.toInt() ?? 0;
      sessionUp = (e['sessionUp'] as num?)?.toInt() ?? 0;
      if (appliedFingerprint.isEmpty) appliedFingerprint = settings.coreFingerprint();
    }
    if (state == ConnState.disconnected || state == ConnState.error) {
      downSpeed = upSpeed = 0;
    }
    if (state != ConnState.connected) serverUnreachable = false;
    statsTick.value++;
    notifyListeners();
  }

  void _applyStats(Map<String, dynamic> e) {
    downSpeed = (e['down'] as num?)?.toDouble() ?? 0;
    upSpeed = (e['up'] as num?)?.toDouble() ?? 0;
    serverUnreachable = e['proxy'] == 'unreachable';
    sessionDown = (e['sessionDown'] as num?)?.toInt() ?? sessionDown;
    sessionUp = (e['sessionUp'] as num?)?.toInt() ?? sessionUp;
    peakDown = max(peakDown, downSpeed);
    peakUp = max(peakUp, upSpeed);
    history.add(SpeedSample(downSpeed, upSpeed));
    while (history.length > historyLen) {
      history.removeAt(0);
    }
    statsTick.value++;
  }

  /// Returns an error string for the UI (or null on success / in progress).
  Future<String?> connect({bool skipAutoFastest = false}) async {
    var p = selected;
    if (p == null) return 'err.noServer';
    if (p.isMTProto) {
      await _native.openUrl(p.tgLink);
      return null;
    }
    state = ConnState.connecting;
    lastError = '';
    notifyListeners();
    if (!skipAutoFastest && settings.autoSelectFastest && p.pingable && profiles.where((x) => x.pingable).length > 1) {
      // Auto-fastest only among Xray / sing-box profiles.
      await pingAll();
      final best = fastest();
      if (best != null) {
        await select(best.id, switchLive: false);
        p = best;
      }
    }
    appliedFingerprint = settings.coreFingerprint();
    peakDown = peakUp = 0;
    history
      ..clear()
      ..addAll(List.filled(historyLen, const SpeedSample(0, 0)));
    final err = await _native.connect(effectiveJson(p), coreSettingsJson(), {..._serviceOptions(), 'profileName': p.name});
    if (err != null && err.isNotEmpty) {
      state = ConnState.error;
      lastError = err;
      notifyListeners();
      return err == 'denied' ? 'err.vpnDenied' : err;
    }
    return null;
  }

  Future<void> disconnect() async {
    state = ConnState.disconnecting;
    notifyListeners();
    await _native.disconnect();
  }

  bool _toggling = false;

  /// Connect / disconnect. Taps while a previous toggle is still being handed
  /// to the service, or while disconnecting, are ignored (no overlapping
  /// connect + disconnect from rapid taps).
  Future<String?> toggle() async {
    if (_toggling || state == ConnState.disconnecting) return null;
    _toggling = true;
    try {
      if (isOn) {
        await disconnect();
        return null;
      }
      return await connect();
    } catch (e) {
      // Never let a toggle throw into the button's tap handler.
      return '$e';
    } finally {
      _toggling = false;
    }
  }

  // ---------- import ----------

  static final _subUrl = RegExp(r'^https?://\S+$', caseSensitive: false);

  /// Imports pasted / scanned text. Returns (profilesAdded, subscriptionAdded, error).
  Future<(int, bool, String?)> importText(String text, {String name = ''}) async {
    text = text.trim();
    if (text.isEmpty) return (0, false, null);
    final r = await _native.parseText(text);
    final list = ((r['profiles'] as List?) ?? const []).map((e) => Profile.fromJson(e as Map<String, dynamic>)).toList();
    if (list.isEmpty && _subUrl.hasMatch(text) && !text.contains('\n')) {
      final err = await addSubscription(text, name: name);
      return (0, err == null, err);
    }
    if (list.isEmpty) {
      final errs = (r['errors'] as List?)?.join('\n');
      // Removed protocols (1.0.4): a clear localized message, not the raw error.
      for (final k in const ['err.slipnetRemoved', 'err.typeRemoved']) {
        if (errs?.contains(k) == true) return (0, false, k);
      }
      return (0, false, errs?.isNotEmpty == true ? errs : null);
    }
    final n = _mergeManual(list);
    // Links generated with the server's internal address (e.g. 172.x) are
    // unreachable from the phone until a host override is set.
    if (n > 0 && list.any((p) => p.json['warning'] == 'privateHost')) {
      return (n, false, 'warn:privateHost');
    }
    return (n, false, null);
  }

  Future<(int, String?)> importConf(String text, String fileName) async {
    final r = await _native.parseConf(text, fileName);
    if (r['error'] != null) return (0, r['error'] as String);
    final n = _mergeManual([Profile.fromJson(r)]);
    return (n, null);
  }

  int _mergeManual(List<Profile> list) {
    var added = 0;
    for (final p in list) {
      p.subId = null;
      if (profiles.any((q) => q.id == p.id)) continue;
      profiles.add(p);
      added++;
    }
    if (selectedId == null && profiles.isNotEmpty) selectedId = profiles.first.id;
    _saveProfiles();
    notifyListeners();
    return added;
  }

  Future<String?> addSubscription(String url, {String name = ''}) async {
    url = url.trim();
    final existing = subs.where((s) => s.url == url).toList();
    final sub = existing.isNotEmpty
        ? existing.first
        : Subscription(id: DateTime.now().microsecondsSinceEpoch.toRadixString(36), url: url, name: name);
    if (existing.isEmpty) subs.add(sub);
    final err = await updateSubscription(sub);
    if (err != null && sub.updatedAt == 0 && profiles.every((p) => p.subId != sub.id)) {
      // Keep it so the user can retry, but report the error.
    }
    await _saveSubs();
    notifyListeners();
    return err;
  }

  /// Fetches one subscription. Returns an error message or null.
  Future<String?> updateSubscription(Subscription sub, {bool certRetried = false}) async {
    final dev = {
      'hwid': device['hwid'] ?? '',
      'os': 'Android',
      'osVersion': device['osVersion'] ?? '',
      'model': device['model'] ?? '',
      'appVersion': device['appVersion'] ?? '',
    };
    Map<String, dynamic> r;
    try {
      await _native.setSubPins(jsonEncode(subPins));
      r = await _native.fetchSubscription(sub.url, dev);
    } catch (e) {
      sub.lastError = '$e';
      notifyListeners();
      return sub.lastError;
    }
    final status = (r['status'] as num?)?.toInt() ?? 0;
    final errors = ((r['errors'] as List?) ?? const []).map((e) => '$e').toList();
    sub.refused = r['refused'] as String? ?? '';
    if (status == 0 && !certRetried && errors.isNotEmpty) {
      final retry = await _askCertificate(sub, errors.first);
      if (retry != null) return retry ? updateSubscription(sub, certRetried: true) : _certRejected(sub);
    }
    if (status == 0 || status ~/ 100 != 2) {
      sub.lastError = errors.isNotEmpty ? errors.first : 'HTTP $status';
      await _saveSubs();
      notifyListeners();
      return sub.refused.isNotEmpty ? 'refused:${sub.refused}' : sub.lastError;
    }
    final title = r['title'] as String? ?? '';
    if (title.isNotEmpty) sub.title = title;
    final ui = r['userInfo'] as Map<String, dynamic>?;
    if (ui != null && ui['present'] == true) sub.userInfo = UserInfo.fromJson(ui);
    sub.updateHours = (r['updateHours'] as num?)?.toInt() ?? sub.updateHours;
    sub.supportUrl = r['supportUrl'] as String? ?? sub.supportUrl;
    sub.updatedAt = DateTime.now().millisecondsSinceEpoch;
    sub.lastError = '';
    final fresh = ((r['profiles'] as List?) ?? const []).map((e) => Profile.fromJson(e as Map<String, dynamic>)).toList();
    // Replace this subscription's servers, keeping their position and any
    // user renames.
    final renamed = <String, String>{};
    final edited = <String, Profile>{};
    for (final p in profiles.where((p) => p.subId == sub.id)) {
      if (p.json['userRenamed'] == true) renamed[p.id] = p.name;
      if (p.json['userEdited'] == true) edited[p.id] = p;
    }
    // Servers the user edited by hand keep their edits across refreshes.
    for (var i = 0; i < fresh.length; i++) {
      final e = edited[fresh[i].id];
      if (e != null) fresh[i] = e;
    }
    final firstIdx = profiles.indexWhere((p) => p.subId == sub.id);
    profiles.removeWhere((p) => p.subId == sub.id);
    for (final p in fresh) {
      p.subId = sub.id;
      if (renamed.containsKey(p.id)) {
        p.name = renamed[p.id]!;
        p.json['userRenamed'] = true;
      }
    }
    final at = firstIdx < 0 ? profiles.length : min(firstIdx, profiles.length);
    profiles.insertAll(at, fresh.where((p) => !profiles.any((q) => q.id == p.id)));
    if (sub.refused.isNotEmpty && fresh.isEmpty) {
      sub.lastError = 'refused:${sub.refused}';
    }
    if (selectedId == null || !profiles.any((p) => p.id == selectedId)) {
      selectedId = profiles.isEmpty ? null : profiles.first.id;
      if (selectedId != null) await _prefs?.setString('selected', selectedId!);
    }
    await _saveProfiles();
    await _saveSubs();
    notifyListeners();
    return sub.refused.isNotEmpty ? 'refused:${sub.refused}' : null;
  }

  /// "ip:port" of an https subscription URL whose host is a literal IP (the
  /// only kind whose certificate can be approved), else null. Same key as
  /// the core's subHostKey.
  static String? subPinKey(String url) {
    final u = Uri.tryParse(url.trim());
    if (u == null || u.scheme.toLowerCase() != 'https' || u.host.isEmpty) return null;
    final ip = InternetAddress.tryParse(u.host);
    if (ip == null) return null;
    final port = u.hasPort ? u.port : 443;
    return ip.type == InternetAddressType.IPv6 ? '[${ip.address}]:$port' : '${ip.address}:$port';
  }

  /// `err.subCertUntrusted:<sha256>` → ask the user (1.0.8, audit H1).
  /// Returns true to retry with the approved pin, false when the user
  /// declined, null when this is not a certificate question.
  Future<bool?> _askCertificate(Subscription sub, String error) async {
    const prefix = 'err.subCertUntrusted:';
    if (!error.startsWith(prefix)) return null;
    final fp = error.substring(prefix.length).trim().toLowerCase();
    final key = subPinKey(sub.url);
    final ask = confirmCert;
    if (key == null || ask == null || !RegExp(r'^[0-9a-f]{64}$').hasMatch(fp)) return null;
    final changed = subPins.containsKey(key) && subPins[key] != fp;
    _certPrompts++;
    bool ok = false;
    try {
      ok = await ask(key, fp, changed);
    } catch (_) {
      ok = false;
    } finally {
      _certPrompts--;
      if (_certPrompts == 0) {
        for (final f in List.of(_afterCertPrompt)) {
          f();
        }
        _afterCertPrompt.clear();
      }
    }
    if (!ok) return false;
    subPins[key] = fp;
    await _prefs?.setString('subPins', jsonEncode(subPins));
    return true;
  }

  Future<String?> _certRejected(Subscription sub) async {
    sub.lastError = 'err.subCertRejected';
    await _saveSubs();
    notifyListeners();
    return sub.lastError;
  }

  /// [work] capped at [refreshTimeout]; the clock is paused while a
  /// certificate question is open (the user is reading it).
  Future<T> _capped<T>(Future<T> work, T onTimeout) {
    final c = Completer<T>();
    Timer? timer;
    void arm() {
      timer?.cancel();
      timer = Timer(refreshTimeout, () {
        if (c.isCompleted) return;
        if (_certPrompts > 0) {
          _afterCertPrompt.add(arm);
          return;
        }
        c.complete(onTimeout);
      });
    }

    arm();
    work.then((v) {
      timer?.cancel();
      if (!c.isCompleted) c.complete(v);
    }, onError: (Object e, StackTrace st) {
      timer?.cancel();
      if (!c.isCompleted) c.completeError(e, st);
    });
    return c.future;
  }

  /// Longest a manual refresh keeps its spinner. After that the result so far
  /// is reported; a fetch still in flight may finish later but is never retried.
  static Duration refreshTimeout = const Duration(seconds: 10);

  /// Manual "refresh all": returns the number of subscriptions that failed or
  /// did not finish within [refreshTimeout].
  Future<int> updateAll() async {
    if (busyUpdating) return 0;
    busyUpdating = true;
    notifyListeners();
    final list = List.of(subs);
    var done = 0, fails = 0;
    final work = () async {
      for (final s in list) {
        if (await updateSubscription(s) != null) fails++;
        done++;
      }
    }();
    try {
      await _capped<void>(work, null);
    } finally {
      busyUpdating = false;
      notifyListeners();
    }
    return fails + (list.length - done);
  }

  /// Manual refresh of one subscription, capped at [refreshTimeout].
  /// Returns null on success, 'timeout' when it took too long, else the error.
  Future<String?> refreshSubscription(Subscription sub) async {
    if (busyUpdating) return null;
    busyUpdating = true;
    notifyListeners();
    try {
      return await _capped<String?>(updateSubscription(sub), 'timeout');
    } finally {
      busyUpdating = false;
      notifyListeners();
    }
  }

  Future<void> removeSubscription(Subscription s) async {
    subs.removeWhere((x) => x.id == s.id);
    profiles.removeWhere((p) => p.subId == s.id);
    // The selected config went with it: clear the selection (the connection
    // state itself is left to the service).
    if (selectedId != null && !profiles.any((p) => p.id == selectedId)) {
      selectedId = null;
      await _prefs?.remove('selected');
    }
    await _saveSubs();
    await _saveProfiles();
    notifyListeners();
  }

  Future<void> editSubscription(Subscription s, {String? name, String? url, bool? autoUpdate}) async {
    if (name != null) s.name = name;
    if (url != null) s.url = url;
    if (autoUpdate != null) s.autoUpdate = autoUpdate;
    await _saveSubs();
    notifyListeners();
  }

  Future<void> deleteProfile(Profile p) async {
    profiles.removeWhere((x) => x.id == p.id);
    pings.remove(p.id);
    if (selectedId == p.id) selectedId = profiles.isEmpty ? null : profiles.first.id;
    await _saveProfiles();
    notifyListeners();
  }

  /// Replaces [original] with the edited profile JSON (from the editor).
  /// The id is kept so selection, pings and subscription matching survive.
  /// Returns the stored profile.
  Future<Profile> saveProfile(Profile original, Map<String, dynamic> json, {bool connectionChanged = true}) async {
    final j = Map<String, dynamic>.from(json);
    j['id'] = original.id.isNotEmpty ? original.id : (j['id'] ?? 'manual-${DateTime.now().microsecondsSinceEpoch}');
    if (original.subId != null) j['subId'] = original.subId;
    if ((j['name'] ?? '') != (original.json['name'] ?? '')) j['userRenamed'] = true;
    if (connectionChanged) {
      j['userEdited'] = true;
      // The imported share link no longer describes this config: re-export
      // it with the core when it can, else drop it.
      j.remove('link');
      try {
        final link = await _native.profileToLink(jsonEncode(j));
        if (link != null) j['link'] = link;
      } catch (_) {}
      pings.remove(original.id);
      pingErrors.remove(original.id);
    } else if (original.json['userEdited'] == true) {
      j['userEdited'] = true;
    }
    final p = Profile.fromJson(j);
    final idx = profiles.indexWhere((x) => x.id == original.id);
    if (idx < 0) {
      profiles.add(p);
    } else {
      profiles[idx] = p;
    }
    selectedId ??= p.id;
    await _saveProfiles();
    try {
      await _native.profileEdited(effectiveJson(p));
    } catch (_) {}
    notifyListeners();
    return p;
  }

  /// Core edit round-trip: canonical JSON with the app-only keys kept.
  /// Returns (json, error); json is the input when the core lacks the API.
  Future<(Map<String, dynamic>, String?)> normalizeEdited(Map<String, dynamic> j) async {
    Map<String, dynamic>? r;
    try {
      r = await _native.normalizeProfile(jsonEncode(j));
    } catch (e) {
      return (j, '$e');
    }
    if (r == null) return (j, null);
    if (r['error'] is String) return (j, r['error'] as String);
    for (final k in const ['subId', 'userRenamed', 'userEdited', 'link']) {
      if (j.containsKey(k) && !r.containsKey(k)) r[k] = j[k];
    }
    // Unknown keys are dropped by the core; keep the id stable.
    r['id'] = j['id'];
    return (r, null);
  }

  /// Core validation of a profile JSON with the current settings ('' = OK).
  Future<String> checkProfileJson(String profileJson) => _native.checkProfile(profileJson, coreSettingsJson());

  /// Whether the running session uses an older version of [p].
  bool isStaleConnection(Profile p) => state == ConnState.connected && connectedProfileId == p.id;

  Future<void> renameProfile(Profile p, String name) async {
    p.name = name;
    p.json['userRenamed'] = true;
    await _saveProfiles();
    notifyListeners();
  }

  // ---------- latency ----------

  // AmneziaWG/WireGuard: the first handshake must fit in the budget (the
  // core also enforces >= 12 s).
  int _timeoutFor(Profile p) => p.isWireGuard ? 15000 : 9000;

  Future<int> ping(Profile p) async {
    if (!p.pingable) return -1;
    testing.add(p.id);
    notifyListeners();
    final (d, err) = await _native.urlTest(effectiveJson(p), coreSettingsJson(), settings.pingUrl, _timeoutFor(p));
    testing.remove(p.id);
    pings[p.id] = d;
    if (err.isNotEmpty) {
      pingErrors[p.id] = err;
    } else {
      pingErrors.remove(p.id);
    }
    notifyListeners();
    return d;
  }

  Future<void> pingAll({Iterable<Profile>? only}) async {
    final list = (only ?? profiles).where((p) => p.pingable && !testing.contains(p.id)).toList();
    var i = 0;
    Future<void> worker() async {
      while (i < list.length) {
        final p = list[i++];
        await ping(p);
      }
    }

    await Future.wait(List.generate(min(6, list.length), (_) => worker()));
    await _saveProfiles();
  }

  Profile? fastest({Iterable<Profile>? among}) {
    Profile? best;
    var bestMs = 1 << 30;
    for (final p in among ?? profiles) {
      if (!p.pingable) continue;
      final ms = pings[p.id];
      if (ms != null && ms > 0 && ms < bestMs) {
        best = p;
        bestMs = ms;
      }
    }
    return best;
  }

  Future<String> coreName(Profile p) => _native.coreFor(p.encode());

  final Map<String, (String, String)> _cores = {};
  final Set<String> _coresPending = {};

  /// Engine that will carry [p] under the current core choice, as
  /// (core, errorKey); null until the core answered (then notifies).
  (String, String)? coreOf(Profile p) {
    final key = '${p.encode().hashCode}|${settings.core}';
    final hit = _cores[key];
    if (hit != null || _coresPending.contains(key)) return hit;
    _coresPending.add(key);
    _native.resolveCore(p.encode(), coreSettingsJson()).then((r) {
      _coresPending.remove(key);
      if (r.$1.isEmpty) return;
      _cores[key] = r;
      notifyListeners();
    });
    return null;
  }

  Future<void> setLanguage(String lang) async {
    settings.language = lang;
    await saveSettings();
  }
}

class AppScope extends InheritedNotifier<AppState> {
  const AppScope({super.key, required AppState state, required super.child}) : super(notifier: state);

  static AppState of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<AppScope>()!.notifier!;
  static AppState read(BuildContext context) => context.getInheritedWidgetOfExactType<AppScope>()!.notifier!;
}

/// Rebuilds [builder] only when [select] of the app state changes (not on
/// every [AppState.notifyListeners]).
class AppSelector<T> extends StatefulWidget {
  const AppSelector({super.key, required this.select, required this.builder, this.child});
  final T Function(AppState app) select;
  final Widget Function(BuildContext context, T value, Widget? child) builder;
  final Widget? child;

  @override
  State<AppSelector<T>> createState() => _AppSelectorState<T>();
}

class _AppSelectorState<T> extends State<AppSelector<T>> {
  AppState? _app;
  late T _value;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final a = AppScope.read(context);
    if (!identical(a, _app)) {
      _app?.removeListener(_changed);
      _app = a..addListener(_changed);
      _value = widget.select(a);
    }
  }

  @override
  void didUpdateWidget(AppSelector<T> old) {
    super.didUpdateWidget(old);
    _value = widget.select(_app!);
  }

  void _changed() {
    if (!mounted) return;
    final v = widget.select(_app!);
    if (v != _value) setState(() => _value = v);
  }

  @override
  void dispose() {
    _app?.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context, _value, widget.child);
}

/// Rebuilds [builder] on every stats tick ([AppState.statsTick]) while its
/// page is in front; a hidden page (TickerMode off) skips the ticks and
/// catches up once when it comes back.
class StatsBuilder extends StatefulWidget {
  const StatsBuilder({super.key, required this.builder});
  final WidgetBuilder builder;

  @override
  State<StatsBuilder> createState() => _StatsBuilderState();
}

class _StatsBuilderState extends State<StatsBuilder> {
  ValueNotifier<int>? _tick;
  bool _stale = false;
  bool _enabled = true;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final t = AppScope.read(context).statsTick;
    if (!identical(t, _tick)) {
      _tick?.removeListener(_changed);
      _tick = t..addListener(_changed);
    }
    _enabled = TickerMode.valuesOf(context).enabled;
    if (_enabled && _stale) _stale = false; // this rebuild catches up
  }

  void _changed() {
    if (!mounted) return;
    if (_enabled) {
      setState(() {});
    } else {
      _stale = true;
    }
  }

  @override
  void dispose() {
    _tick?.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context);
}
