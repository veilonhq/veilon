// 1.0.7: in-app update check. Pure logic (release parsing, version from
// the "client-latest" release notes, installed-variant → asset mapping with
// no fallback, checksums) and the Settings row flow with a fake client.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:veilon/core/app_state.dart';
import 'package:veilon/core/update_check.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/main.dart';
import 'package:veilon/ui/pages/settings_page.dart';
import 'package:veilon/ui/sheets/update_flow.dart';
import 'package:veilon/ui/shell.dart';

import 'helpers.dart';

const _sha64 = 'b2b1c6f0d0e5c1a3f4e2d7c8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8';

Map<String, Object?> _asset(String name, [int size = 31000000]) => {
      'name': name,
      'size': size,
      'browser_download_url': 'https://github.com/veilonhq/veilon/releases/download/client-latest/$name',
    };

String _body(String version, {bool fa = true}) => [
      if (fa) ...['## فارسی', '', 'نسخه $version: بررسی بروزرسانی داخل برنامه.', '', 'Version: 9.9.9 (fa section must not win)', ''],
      '## English',
      '',
      'In-app update check, smoother tab switch and home scroll.',
      '',
      'Version: $version',
      '',
    ].join('\n');

/// GitHub "release by tag" answer for client-latest.
Map<String, Object?> clientLatest({
  String version = '1.0.7',
  List<String> variants = const ['arm64-v8a', 'armeabi-v7a', 'universal'],
  bool checksums = true,
  bool draft = false,
  String? body,
}) =>
    {
      'tag_name': 'client-latest',
      'name': 'Veilon Client (آخرین نسخه)',
      'draft': draft,
      'prerelease': false,
      'html_url': 'https://github.com/veilonhq/veilon/releases/tag/client-latest',
      'body': body ?? _body(version),
      'assets': [
        for (final v in variants) _asset('veilon-client-$v.apk'),
        if (checksums) _asset('checksums.txt', 300),
      ],
    };

/// A panel release (must never count as a client release).
Map<String, Object?> panelRelease() => {
      'tag_name': 'v0.3.9',
      'name': 'Veilon v0.3.9',
      'draft': false,
      'prerelease': false,
      'body': 'Panel release\n\nVersion: 0.3.9',
      'assets': [_asset('veilon-linux-amd64.tar.gz'), _asset('checksums.txt')],
    };

void main() {
  group('SemVer', () {
    test('parse and compare', () {
      expect(SemVer.tryParse('1.0.7'), const SemVer(1, 0, 7));
      expect(SemVer.tryParse('1.0.7+8'), const SemVer(1, 0, 7)); // build number ignored
      expect(SemVer.tryParse('1.0'), isNull);
      expect(SemVer.tryParse('v1.0.7'), isNull);
      expect(SemVer.tryParse('1.0.10')! > SemVer.tryParse('1.0.9')!, isTrue);
      expect(SemVer.tryParse('1.1.0')! > SemVer.tryParse('1.0.99')!, isTrue);
      expect(SemVer.tryParse('2.0.0')! > SemVer.tryParse('1.9.9')!, isTrue);
      expect(SemVer.tryParse('1.0.7')! > SemVer.tryParse('1.0.7')!, isFalse);
      expect(SemVer.tryParse('1.0.7-rc1')! > SemVer.tryParse('1.0.7')!, isFalse);
    });
  });

  group('client-latest release', () {
    test('version comes from the English "Version:" line', () {
      final r = clientReleaseFrom(clientLatest(version: '1.0.7'))!;
      expect(r.version, const SemVer(1, 0, 7));
      expect(r.tag, 'client-latest');
      expect(releaseBodyVersion(_body('1.2.3')), const SemVer(1, 2, 3));
      // No English section: any Version line.
      expect(releaseBodyVersion('notes\nVersion: 1.0.8\n'), const SemVer(1, 0, 8));
      expect(releaseBodyVersion('**Version:** 1.0.9'), const SemVer(1, 0, 9));
      expect(releaseBodyVersion('Version: v1.0.10'), const SemVer(1, 0, 10));
      expect(releaseBodyVersion('no version here'), isNull);
    });

    test('the published client-latest notes format', () {
      const body = '## فارسی\n\nاین صفحه آدرس ثابتی دارد:\nhttps://github.com/veilonhq/veilon/releases/tag/client-latest\n\n'
          '**نسخه فعلی: 1.0.6**\n\nتغییرات این نسخه:\n- ...\n\n## English\n\nThis page has a fixed address:\n'
          'https://github.com/veilonhq/veilon/releases/tag/client-latest\n\nVersion: 1.0.7\n\nChanges in this version:\n- ...\n\n'
          '**Which APK?**\n- `veilon-client-arm64-v8a.apk`: most phones\n';
      expect(releaseBodyVersion(body), const SemVer(1, 0, 7));
      final r = clientReleaseFrom({...clientLatest(), 'body': body})!;
      expect(r.version, const SemVer(1, 0, 7));
      expect(r.name, 'Veilon Client (آخرین نسخه)');
    });

    test('panel releases and other tags are ignored', () {
      expect(clientReleaseFrom(panelRelease()), isNull);
      expect(clientReleaseFrom({...clientLatest(), 'tag_name': 'client-v1.0.6'}), isNull);
      expect(clientReleaseFrom({...clientLatest(), 'tag_name': 'v0.3.9'}), isNull);
      expect(clientReleaseFrom(clientLatest(draft: true)), isNull);
      expect(clientReleaseFrom(clientLatest(body: 'no version line')), isNull);
      expect(clientReleaseFrom([clientLatest()]), isNull); // a list is not the by-tag answer
      expect(clientReleaseFrom(null), isNull);
      expect(clientReleaseFrom({'message': 'Not Found'}), isNull);
    });

    test('update only when the release is newer', () {
      final r = clientReleaseFrom(clientLatest(version: '1.0.7'))!;
      expect(UpdateCheck(const SemVer(1, 0, 6), r).hasUpdate, isTrue);
      expect(UpdateCheck(const SemVer(1, 0, 7), r).hasUpdate, isFalse);
      expect(UpdateCheck(const SemVer(1, 0, 8), r).hasUpdate, isFalse);
      expect(const UpdateCheck(SemVer(1, 0, 6), null).hasUpdate, isFalse);
    });
  });

  group('installed variant → asset (no fallback)', () {
    test('variant from the installed APK lib folders', () {
      expect(variantFromLibAbis(['arm64-v8a']), ApkVariant.arm64);
      expect(variantFromLibAbis(['armeabi-v7a']), ApkVariant.armv7);
      expect(variantFromLibAbis(['arm64-v8a', 'armeabi-v7a']), ApkVariant.universal);
      expect(variantFromLibAbis(['armeabi-v7a', 'arm64-v8a', 'x86_64']), ApkVariant.universal);
      expect(variantFromLibAbis([]), isNull);
      expect(variantFromLibAbis(['x86_64']), isNull);
    });

    test('asset names', () {
      expect(apkAssetName(ApkVariant.arm64), 'veilon-client-arm64-v8a.apk');
      expect(apkAssetName(ApkVariant.armv7), 'veilon-client-armeabi-v7a.apk');
      expect(apkAssetName(ApkVariant.universal), 'veilon-client-universal.apk');
    });

    test('each variant gets exactly its own asset', () {
      final r = clientReleaseFrom(clientLatest())!;
      expect(selectApkAsset(r, ApkVariant.arm64).name, 'veilon-client-arm64-v8a.apk');
      expect(selectApkAsset(r, ApkVariant.armv7).name, 'veilon-client-armeabi-v7a.apk');
      expect(selectApkAsset(r, ApkVariant.universal).name, 'veilon-client-universal.apk');
    });

    for (final (installed, missing) in [
      (ApkVariant.arm64, 'arm64-v8a'),
      (ApkVariant.armv7, 'armeabi-v7a'),
      (ApkVariant.universal, 'universal'),
    ]) {
      test('missing $missing asset is an error, never another variant', () {
        final others = ['arm64-v8a', 'armeabi-v7a', 'universal'].where((v) => v != missing).toList();
        final r = clientReleaseFrom(clientLatest(variants: others))!;
        expect(
            () => selectApkAsset(r, installed),
            throwsA(isA<UpdateError>()
                .having((e) => e.code, 'code', 'assetMissing')
                .having((e) => e.detail, 'detail', 'veilon-client-$missing.apk')));
      });
    }

    test('no checksums.txt → no install', () {
      final r = clientReleaseFrom(clientLatest(checksums: false))!;
      expect(() => selectApkAsset(r, ApkVariant.arm64), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'noChecksum')));
    });

    test('old versioned asset names are not taken for the unversioned ones', () {
      final rel = clientLatest(variants: const []);
      (rel['assets'] as List).add(_asset('veilon-client-v1.0.7-arm64-v8a.apk'));
      final r = clientReleaseFrom(rel)!;
      expect(() => selectApkAsset(r, ApkVariant.arm64), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'assetMissing')));
    });
  });

  group('checksums', () {
    test('sha256sum format, text and binary mode', () {
      final sums = parseChecksums('$_sha64  veilon-client-arm64-v8a.apk\n'
          '${_sha64.toUpperCase().replaceRange(0, 1, 'C')} *veilon-client-armeabi-v7a.apk\r\n'
          '\n# comment\nnot a line\n'
          '${'a' * 64}  ./veilon-client-universal.apk\n');
      expect(sums['veilon-client-arm64-v8a.apk'], _sha64);
      expect(sums['veilon-client-armeabi-v7a.apk'], 'c${_sha64.substring(1)}');
      expect(sums['veilon-client-universal.apk'], 'a' * 64);
      expect(sums.length, 3);
    });

    test('expected hash and verification', () {
      final sums = parseChecksums('$_sha64  veilon-client-arm64-v8a.apk\n');
      expect(expectedSha256(sums, 'veilon-client-arm64-v8a.apk'), _sha64);
      expect(() => expectedSha256(sums, 'veilon-client-universal.apk'),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'noChecksum')));
      verifySha256(_sha64, _sha64.toUpperCase());
      expect(() => verifySha256(_sha64, 'f' * 64), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'checksumMismatch')));
      expect(() => verifySha256(_sha64, ''), throwsA(isA<UpdateError>()));
    });
  });

  test('release notes snippet picks the language section', () {
    final b = _body('1.0.7');
    expect(notesSnippet(b, 'en'), contains('In-app update check'));
    expect(notesSnippet(b, 'en'), isNot(contains('فارسی')));
    expect(notesSnippet(b, 'fa'), contains('نسخه 1.0.7'));
    expect(notesSnippet('x' * 2000, 'en').length, lessThanOrEqualTo(601));
  });

  test('endpoint is the fixed client-latest release, never releases/latest', () {
    expect(updateReleaseUrl, 'https://api.github.com/repos/veilonhq/veilon/releases/tags/client-latest');
    expect(updateReleaseUrl.contains('/releases/latest'), isFalse);
  });

  test('no ZWNJ in the update strings', () {
    for (final lang in ['fa', 'en']) {
      final s = S(lang);
      for (final k in ['settings.update', 'info.update', 'update.latest', 'update.checking', 'update.err.network', 'update.err.assetMissing']) {
        expect(s.t(k).contains('\u200c'), isFalse, reason: '$lang $k');
        expect(s.t(k), isNot(k));
      }
    }
    expect(S('fa').t('update.latest'), 'شما آخرین نسخه را دارید');
    expect(S('en').t('update.latest'), "You're on the latest version");
  });

  group('settings row', () {
    setUpAll(loadAppFonts);
    setUp(() {
      calls.clear();
      installNativeMocks();
    });

    Future<AppState> boot(String lang) async {
      SharedPreferences.setMockInitialValues({
        'flutter.profiles': jsonEncode([vlessReality()]),
        'flutter.settings': jsonEncode({'language': lang}),
        'flutter.selected': 'p-vless',
      });
      final s = AppState();
      await s.init();
      return s;
    }

    for (final lang in ['fa', 'en']) {
      testWidgets('row + info sheet ($lang)', (t) async {
        t.view.physicalSize = const Size(360 * 3, 780 * 3);
        t.view.devicePixelRatio = 3;
        addTearDown(t.view.reset);
        final state = await boot(lang);
        await t.pumpWidget(VeilonApp(state: state));
        await t.pump(const Duration(milliseconds: 500));
        Shell.goTo(t.element(find.byType(TabRail)), 3);
        await t.pump(const Duration(milliseconds: 600));
        final row = find.byKey(const ValueKey('settings-update'));
        await t.scrollUntilVisible(row, 200, scrollable: find.descendant(of: find.byType(SettingsPage), matching: find.byType(Scrollable)).first);
        await t.pump(const Duration(milliseconds: 300));
        expect(find.descendant(of: row, matching: find.textContaining(S(lang).t('settings.update'), findRichText: true)), findsWidgets);
        await t.tap(find.byKey(const ValueKey('info-info.update')));
        await t.pump();
        await t.pump(const Duration(milliseconds: 500));
        expect(find.byKey(const ValueKey('setting-info-sheet')), findsOneWidget);
        expect(find.text(S(lang).t('info.update')), findsOneWidget);
        await t.pump(const Duration(seconds: 3));
        state.dispose();
      });
    }

    Future<AppState> openRow(WidgetTester t, String lang) async {
      t.view.physicalSize = const Size(360 * 3, 780 * 3);
      t.view.devicePixelRatio = 3;
      final state = await boot(lang);
      state.device['appVersion'] = '1.0.7';
      await t.pumpWidget(VeilonApp(state: state));
      await t.pump(const Duration(milliseconds: 500));
      Shell.goTo(t.element(find.byType(TabRail)), 3);
      await t.pump(const Duration(milliseconds: 600));
      final row = find.byKey(const ValueKey('settings-update'));
      await t.scrollUntilVisible(row, 200, scrollable: find.descendant(of: find.byType(SettingsPage), matching: find.byType(Scrollable)).first);
      await t.pump(const Duration(milliseconds: 300));
      // The leading icon (the row centre can be the title's (i) button in RTL).
      await t.tap(find.descendant(of: row, matching: find.byIcon(Icons.system_update_rounded)));
      for (var i = 0; i < 10; i++) {
        await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
        await t.pump(const Duration(milliseconds: 100));
      }
      return state;
    }

    for (final lang in ['fa', 'en']) {
      testWidgets('same version → "latest" message ($lang)', (t) async {
        addTearDown(t.view.reset);
        updateClientFactory = () => _FakeClient(clientLatest(version: '1.0.7'));
        addTearDown(() => updateClientFactory = UpdateClient.new);
        final state = await openRow(t, lang);
        expect(find.text(S(lang).t('update.latest')), findsOneWidget);
        expect(find.byKey(const ValueKey('update-dialog')), findsNothing);
        await t.pump(const Duration(seconds: 6));
        state.dispose();
      });
    }

    testWidgets('newer version → dialog; unknown installed variant → clear error, nothing downloaded', (t) async {
      addTearDown(t.view.reset);
      final fake = _FakeClient(clientLatest(version: '1.0.8'));
      updateClientFactory = () => fake;
      addTearDown(() => updateClientFactory = UpdateClient.new);
      fakeAbis = const ['x86_64'];
      addTearDown(() => fakeAbis = const []);
      final state = await openRow(t, 'en');
      expect(find.byKey(const ValueKey('update-dialog')), findsOneWidget);
      expect(find.text(S('en').f('update.available', {'v': '1.0.8'})), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('update-go')));
      for (var i = 0; i < 10; i++) {
        await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
        await t.pump(const Duration(milliseconds: 100));
      }
      expect(find.text(S('en').t('update.err.noVariant')), findsOneWidget);
      expect(fake.downloads, isEmpty);
      await t.pump(const Duration(seconds: 6));
      state.dispose();
    });

    testWidgets('arm64 install but release lacks the arm64 APK → error, no other variant downloaded', (t) async {
      addTearDown(t.view.reset);
      final fake = _FakeClient(clientLatest(version: '1.0.8', variants: const ['armeabi-v7a', 'universal']));
      updateClientFactory = () => fake;
      addTearDown(() => updateClientFactory = UpdateClient.new);
      fakeAbis = const ['arm64-v8a'];
      addTearDown(() => fakeAbis = const []);
      final state = await openRow(t, 'fa');
      await t.tap(find.byKey(const ValueKey('update-go')));
      for (var i = 0; i < 10; i++) {
        await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
        await t.pump(const Duration(milliseconds: 100));
      }
      expect(find.textContaining('veilon-client-arm64-v8a.apk'), findsOneWidget);
      expect(fake.downloads, isEmpty);
      await t.pump(const Duration(seconds: 6));
      state.dispose();
    });

    testWidgets('network error → short message', (t) async {
      addTearDown(t.view.reset);
      updateClientFactory = () => _FakeClient(null, error: const UpdateError('network', 'Failed host lookup'));
      addTearDown(() => updateClientFactory = UpdateClient.new);
      final state = await openRow(t, 'fa');
      expect(find.text(S('fa').t('update.err.network')), findsOneWidget);
      await t.pump(const Duration(seconds: 6));
      state.dispose();
    });
  });
}

class _FakeClient extends UpdateClient {
  _FakeClient(this.release, {this.error});
  final Map<String, Object?>? release;
  final UpdateError? error;
  final downloads = <String>[];

  @override
  Future<UpdateCheck> check(String currentVersion) async {
    if (error != null) throw error!;
    final r = clientReleaseFrom(release);
    if (r == null) throw const UpdateError('noRelease');
    return UpdateCheck(SemVer.tryParse(currentVersion) ?? const SemVer(0, 0, 0), r);
  }

  @override
  Future<String> fetchChecksums(ClientRelease r) async =>
      '${'a' * 64}  veilon-client-arm64-v8a.apk\n${'b' * 64}  veilon-client-armeabi-v7a.apk\n${'c' * 64}  veilon-client-universal.apk\n';

  @override
  Future<void> download(ReleaseAsset a, String path, {Progress? onProgress, bool Function()? cancelled}) async {
    downloads.add(a.name);
  }
}
