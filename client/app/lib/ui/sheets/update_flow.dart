// "Check for updates" (1.0.7): settings row → GitHub check → dialog →
// download of the installed variant → sha256 check → system installer.
import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../core/native.dart';
import '../../core/update_check.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';

bool _running = false;

/// Makes the GitHub client (replaced in tests).
@visibleForTesting
UpdateClient Function() updateClientFactory = UpdateClient.new;

/// Runs the whole update flow from [context] (a page under the app's
/// navigator). [client] and [native] are replaceable for tests.
Future<void> checkForUpdates(BuildContext context, {UpdateClient? client, Native? native}) async {
  if (_running) return;
  _running = true;
  try {
    await _flow(context, client ?? updateClientFactory(), native ?? Native.instance);
  } finally {
    _running = false;
  }
}

/// Native sha256 as the fallback hasher: '' (any native failure) becomes an
/// error carrying the native reason instead of a fake mismatch.
Future<String> nativeSha256(Native native, String path) async {
  native.lastError = null;
  final h = await native.sha256File(path);
  if (h.trim().isEmpty) throw native.lastError ?? 'no hash';
  return h;
}

String updateErrorText(S s, Object e) {
  if (e is UpdateError) {
    final key = 'update.err.${e.code}';
    final t = s.t(key);
    if (t == key) return s.t('update.err.badResponse');
    if (e.code == 'assetMissing') return t.replaceAll('{v}', e.detail);
    if (e.code == 'install' || e.code == 'verifyFailed' || e.code == 'storage') {
      return t.replaceAll('{e}', e.detail.isEmpty ? '?' : e.detail);
    }
    if (e.code == 'checksumMismatch') {
      final parts = e.detail.split('/');
      return t
          .replaceAll('{exp}', parts.isNotEmpty && parts[0].isNotEmpty ? parts[0] : '?')
          .replaceAll('{got}', parts.length > 1 && parts[1].isNotEmpty ? parts[1] : '?');
    }
    if (e.code == 'sizeMismatch') return t.replaceAll('{e}', e.detail.isEmpty ? '?' : e.detail);
    return t;
  }
  return s.t('update.err.network');
}

Future<void> _flow(BuildContext context, UpdateClient client, Native native) async {
  final s = S.of(context);
  final app = AppScope.read(context);
  final nav = Navigator.of(context);
  final messenger = ScaffoldMessenger.of(context);
  var current = '${app.device['appVersion'] ?? ''}';
  if (current.isEmpty) current = '${(await native.deviceInfo())['appVersion'] ?? ''}';

  // 1. Check.
  final checking = _BusyDialog.show(nav, s.t('update.checking'));
  UpdateCheck r;
  try {
    r = await client.check(current);
  } catch (e) {
    checking.close();
    toastOn(messenger, updateErrorText(s, e), error: true);
    return;
  }
  checking.close();
  if (!r.hasUpdate) {
    toastOn(messenger, s.t('update.latest'));
    return;
  }
  final rel = r.latest!;
  if (!nav.mounted) return;

  // 2. Ask.
  final go = await showDialog<bool>(
    context: nav.context,
    builder: (ctx) => AlertDialog(
      key: const ValueKey('update-dialog'),
      title: Text(s.f('update.available', {'v': rel.version.toString()})),
      content: ConstrainedBox(
        constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(ctx).height * 0.45),
        child: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(s.f('update.current', {'v': current.isEmpty ? '?' : current}),
                style: TextStyle(color: Palette.muted, fontSize: 12.5)),
            const SizedBox(height: Gap.m),
            Text(notesSnippet(rel.notes, s.lang), style: TextStyle(color: Palette.text2, fontSize: 13, height: 1.55)),
          ]),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(s.t('update.later'))),
        FilledButton(
            key: const ValueKey('update-go'), onPressed: () => Navigator.pop(ctx, true), child: Text(s.t('update.download'))),
      ],
    ),
  );
  if (go != true || !nav.mounted) return;

  // 3. Same variant as installed; no fallback.
  final progress = ValueNotifier<double?>(null);
  final phase = ValueNotifier<String>(s.t('update.downloading'));
  var cancelled = false;
  late final _DialogHandle dl;
  dl = _ProgressDialog.show(nav, phase, progress, onCancel: () {
    cancelled = true;
    dl.close();
  }, cancelLabel: s.t('common.cancel'));
  String? path;
  try {
    final variant = variantFromLibAbis(await native.installedAbis());
    if (variant == null) throw const UpdateError('noVariant');
    final asset = selectApkAsset(rel, variant);
    final sums = parseChecksums(await client.fetchChecksums(rel));
    final expected = expectedSha256(sums, asset.name);
    final dir = await native.updateDir();
    if (dir == null || dir.isEmpty) throw const UpdateError('install', 'no update folder');
    // Old downloads go first (keeps the cache small).
    try {
      for (final f in Directory(dir).listSync()) {
        if (f is File && f.path.endsWith('.apk')) f.deleteSync();
      }
    } catch (_) {}
    path = '$dir/${asset.name}';
    // 1.0.9: Dart hashes the file (native only as a fallback), the length
    // on disk is checked, and a damaged or unverifiable file is deleted and
    // downloaded once more before an error is shown.
    await downloadVerified(
      client,
      asset,
      path,
      expected,
      onProgress: (got, total) => progress.value = total > 0 ? (got / total).clamp(0.0, 1.0) : null,
      cancelled: () => cancelled,
      nativeHash: (p) => nativeSha256(native, p),
      onVerify: () {
        phase.value = s.t('update.verifying');
        progress.value = null;
      },
      onRetry: (_, _) {
        phase.value = s.t('update.retrying');
        progress.value = null;
      },
    );
    if (cancelled) throw const UpdateError('cancelled');
    phase.value = s.t('update.installing');
    var res = await native.installApk(path);
    if (res == 'permission') {
      phase.value = s.t('update.permission');
      // Wait for the user to come back from the "install unknown apps" page.
      await _waitForResume(const Duration(minutes: 5));
      if (cancelled) throw const UpdateError('cancelled');
      res = await native.installApk(path);
      if (res == 'permission') throw UpdateError('install', s.t('update.permission'));
    }
    // 1.0.8 (L5): the APK must be signed with the installed app's key.
    if (res == 'signature mismatch') throw const UpdateError('signature');
    if (res != 'ok') throw UpdateError('install', res);
    dl.close();
  } catch (e) {
    dl.close();
    if (path != null && e is UpdateError && retryableUpdateErrors.contains(e.code)) {
      try {
        File(path).deleteSync();
      } catch (_) {}
    }
    toastOn(messenger, updateErrorText(s, e), error: true);
  } finally {
    // Disposed after the dialog's exit animation.
    Future<void>.delayed(const Duration(seconds: 1), () {
      progress.dispose();
      phase.dispose();
    });
  }
}

Future<void> _waitForResume(Duration max) {
  final c = Completer<void>();
  var paused = false;
  late final AppLifecycleListener l;
  l = AppLifecycleListener(onStateChange: (st) {
    if (st == AppLifecycleState.paused || st == AppLifecycleState.inactive || st == AppLifecycleState.hidden) paused = true;
    if (st == AppLifecycleState.resumed && paused && !c.isCompleted) c.complete();
  });
  return c.future.timeout(max, onTimeout: () {}).whenComplete(l.dispose);
}

class _DialogHandle {
  _DialogHandle(this.nav);
  final NavigatorState nav;
  bool _open = true;
  void close() {
    if (!_open) return;
    _open = false;
    if (nav.mounted) nav.pop();
  }
}

abstract final class _BusyDialog {
  static _DialogHandle show(NavigatorState nav, String text) {
    final h = _DialogHandle(nav);
    showDialog<void>(
      context: nav.context,
      barrierDismissible: false,
      builder: (ctx) => PopScope(
        canPop: false,
        child: AlertDialog(
          key: const ValueKey('update-checking'),
          content: Row(children: [
            const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.4)),
            const SizedBox(width: Gap.l),
            Expanded(child: Text(text)),
          ]),
        ),
      ),
    ).whenComplete(() => h._open = false);
    return h;
  }
}

abstract final class _ProgressDialog {
  static _DialogHandle show(NavigatorState nav, ValueNotifier<String> phase, ValueNotifier<double?> progress,
      {required VoidCallback onCancel, required String cancelLabel}) {
    final h = _DialogHandle(nav);
    showDialog<void>(
      context: nav.context,
      barrierDismissible: false,
      builder: (ctx) => PopScope(
        canPop: false,
        child: AlertDialog(
          key: const ValueKey('update-progress'),
          content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            ValueListenableBuilder<String>(valueListenable: phase, builder: (_, t, _) => Text(t)),
            const SizedBox(height: Gap.m),
            ValueListenableBuilder<double?>(
              valueListenable: progress,
              builder: (_, v, _) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                LinearProgressIndicator(value: v, minHeight: 6, borderRadius: BorderRadius.circular(3)),
                const SizedBox(height: Gap.xs),
                Text(v == null ? ' ' : '${(v * 100).round()}%',
                    textDirection: TextDirection.ltr,
                    textAlign: TextAlign.end,
                    style: TextStyle(color: Palette.muted, fontSize: 12)),
              ]),
            ),
          ]),
          actions: [TextButton(onPressed: onCancel, child: Text(cancelLabel))],
        ),
      ),
    ).whenComplete(() => h._open = false);
    return h;
  }
}
