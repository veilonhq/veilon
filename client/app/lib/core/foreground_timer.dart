import 'dart:async';

import 'package:flutter/widgets.dart';

/// App visibility. Polling timers run only while the app is in the foreground
/// (battery): when Android pauses the activity the Dart isolate keeps running,
/// so plain periodic timers would keep waking the CPU.
class AppLifecycle with WidgetsBindingObserver {
  AppLifecycle._() {
    WidgetsBinding.instance.addObserver(this);
    final s = WidgetsBinding.instance.lifecycleState;
    foreground.value = s == null || s == AppLifecycleState.resumed;
  }

  static AppLifecycle? _i;
  static AppLifecycle get instance => _i ??= AppLifecycle._();

  final foreground = ValueNotifier<bool>(true);

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // inactive (e.g. notification shade over the app) still counts as visible.
    foreground.value = state == AppLifecycleState.resumed || state == AppLifecycleState.inactive;
  }
}

/// A periodic timer that is cancelled while the app is backgrounded and
/// restarted (optionally firing once immediately) when it returns.
class ForegroundTimer {
  ForegroundTimer(this.period, this.onTick, {this.tickOnResume = true}) {
    _fg.addListener(_sync);
    _sync();
  }

  final Duration period;
  final void Function() onTick;
  final bool tickOnResume;
  Timer? _t;
  bool _disposed = false;
  bool _wasForeground = true;

  ValueNotifier<bool> get _fg => AppLifecycle.instance.foreground;
  bool get active => _t != null;

  void _sync() {
    if (_disposed) return;
    final fg = _fg.value;
    if (fg && _t == null) {
      if (tickOnResume && !_wasForeground) onTick();
      _t = Timer.periodic(period, (_) => onTick());
    } else if (!fg) {
      _t?.cancel();
      _t = null;
    }
    _wasForeground = fg;
  }

  void cancel() {
    _disposed = true;
    _fg.removeListener(_sync);
    _t?.cancel();
    _t = null;
  }
}
