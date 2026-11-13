// 1.0.9: updater verification fix. The 1.0.8 updater reported any failure of
// the native sha256 call as "checksum mismatch" (the native reply '' / a
// PlatformException became ''), so a phone where the native hash failed could
// never update. Now: Dart hashes the file (native only as a fallback), an
// unhashable file is `verifyFailed` with its reason, a real difference is
// `checksumMismatch` with the first 8 hex of both hashes, the length on disk
// is checked after the sink is closed, the body is written with
// backpressure (addStream), and a bad file is deleted and downloaded once
// more before an error is shown.
//
// The download tests run the real UpdateClient against a local HTTPS server
// (self-signed certificate made with openssl at test time; every connection
// is routed to it, the URLs keep their GitHub hosts so the allowlist applies).
import 'dart:async';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;
import 'package:flutter_test/flutter_test.dart';
import 'package:veilon/core/update_check.dart';
import 'package:veilon/l10n/strings.dart';
import 'package:veilon/ui/sheets/update_flow.dart';

const _assetUrl = 'https://github.com/veilonhq/veilon/releases/download/client-latest/veilon-client-universal.apk';

List<int> _payload(int n, [int seed = 7]) => List<int>.generate(n, (i) => (i * 31 + seed) & 0xff);
String _hex(List<int> b) => crypto.sha256.convert(b).toString();

/// UpdateClient whose download writes whatever [next] returns (no network).
class _ScriptedClient extends UpdateClient {
  _ScriptedClient(this.next);
  final Future<void> Function(String path, int attempt) next;
  int downloads = 0;
  @override
  Future<void> download(ReleaseAsset a, String path, {Progress? onProgress, bool Function()? cancelled}) async {
    downloads++;
    await next(path, downloads);
  }
}

class _Tls {
  _Tls(this.server, this.dir);
  final HttpServer server;
  final Directory dir;
  int requests = 0;

  /// What the server sends for the APK path: (status, body, declared length or null).
  late FutureOr<void> Function(HttpRequest req, int n) handler;

  static Future<_Tls?> start() async {
    final dir = await Directory.systemTemp.createTemp('upd109');
    final key = '${dir.path}/k.pem', crt = '${dir.path}/c.pem';
    ProcessResult r;
    try {
      r = await Process.run('openssl', [
        'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', key, '-out', crt, '-days', '1', '-subj', '/CN=github.com',
      ]);
    } catch (_) {
      return null;
    }
    if (r.exitCode != 0) return null;
    final ctx = SecurityContext()
      ..useCertificateChain(crt)
      ..usePrivateKey(key);
    final s = await HttpServer.bindSecure(InternetAddress.loopbackIPv4, 0, ctx);
    final t = _Tls(s, dir);
    s.listen((req) async {
      t.requests++;
      try {
        await t.handler(req, t.requests);
      } catch (_) {}
    });
    return t;
  }

  UpdateClient client() => UpdateClient(httpFactory: () {
        final c = HttpClient()..badCertificateCallback = (_, _, _) => true;
        c.connectionFactory = (uri, _, _) => SecureSocket.startConnect(InternetAddress.loopbackIPv4, server.port,
            onBadCertificate: (_) => true);
        return c;
      });

  Future<void> close() async {
    await server.close(force: true);
    try {
      dir.deleteSync(recursive: true);
    } catch (_) {}
  }
}

Future<void> _send(HttpRequest req, List<int> body, {int? declared, bool cutHalf = false}) async {
  req.response.statusCode = 200;
  req.response.headers.contentType = ContentType('application', 'octet-stream');
  req.response.contentLength = declared ?? body.length;
  req.response.bufferOutput = false;
  final part = cutHalf ? body.sublist(0, body.length ~/ 2) : body;
  // Many small chunks, like a real transfer.
  for (var i = 0; i < part.length; i += 65536) {
    req.response.add(part.sublist(i, i + 65536 > part.length ? part.length : i + 65536));
    if (i % (1 << 20) == 0) await req.response.flush();
  }
  // cutHalf: the declared length is the full body but only half is sent;
  // closing the response then fails on the server and drops the connection.
  try {
    await req.response.close();
  } catch (_) {}
}

void main() {
  late Directory tmp;
  setUp(() async => tmp = await Directory.systemTemp.createTemp('upd109f'));
  tearDown(() async {
    try {
      await tmp.delete(recursive: true);
    } catch (_) {}
  });

  group('hash and verification', () {
    test('Dart sha256 of a file equals the known hash (streamed, 5 MB)', () async {
      final b = _payload(5 * 1024 * 1024 + 3);
      final f = File('${tmp.path}/a.apk')..writeAsBytesSync(b);
      expect(await sha256OfFile(f.path), _hex(b));
      File('${tmp.path}/abc.apk').writeAsStringSync('abc');
      expect(await sha256OfFile('${tmp.path}/abc.apk'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
    });

    test('empty / non-hex actual = verifyFailed, never checksumMismatch', () {
      final exp = 'a' * 64;
      expect(() => verifySha256(exp, ''), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'verifyFailed')));
      expect(() => verifySha256(exp, 'native: oops'), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'verifyFailed')));
      expect(
          () => verifySha256(exp, '0f4c6865${'0' * 56}'),
          throwsA(isA<UpdateError>()
              .having((e) => e.code, 'code', 'checksumMismatch')
              .having((e) => e.detail, 'detail', 'aaaaaaaa/0f4c6865')));
      verifySha256(exp.toUpperCase(), ' $exp\n');
    });

    test('native is only the fallback; its failure reason is kept', () async {
      final b = _payload(1000);
      final p = (File('${tmp.path}/x.apk')..writeAsBytesSync(b)).path;
      var nativeCalls = 0;
      Future<String> native(String _) async {
        nativeCalls++;
        return _hex(b);
      }

      expect(await hashDownloaded(p, native: native), _hex(b));
      expect(nativeCalls, 0, reason: 'Dart hash first');
      expect(await hashDownloaded(p, dart: (_) async => throw const FileSystemException('read failed'), native: native), _hex(b));
      expect(nativeCalls, 1);
      await expectLater(
          hashDownloaded(p, dart: (_) async => throw const FileSystemException('read failed'), native: (_) async => throw 'sha256File: file is not in the update folder'),
          throwsA(isA<UpdateError>()
              .having((e) => e.code, 'code', 'verifyFailed')
              .having((e) => e.detail, 'detail', allOf(contains('read failed'), contains('not in the update folder')))));
      await expectLater(hashDownloaded('${tmp.path}/missing.apk'),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'verifyFailed')));
    });

    test('length on disk must equal the asset size', () async {
      final b = _payload(4096);
      final p = (File('${tmp.path}/y.apk')..writeAsBytesSync(b)).path;
      await verifyDownloaded(ReleaseAsset('y.apk', _assetUrl, b.length), p, _hex(b));
      await expectLater(verifyDownloaded(ReleaseAsset('y.apk', _assetUrl, b.length + 1), p, _hex(b)),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'sizeMismatch').having((e) => e.detail, 'detail', '4096/4097')));
      await expectLater(verifyDownloaded(ReleaseAsset('y.apk', _assetUrl, 1), '${tmp.path}/none.apk', _hex(b)),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'verifyFailed')));
    });
  });

  group('download + verify with one automatic retry', () {
    final good = _payload(300000);
    final bad = _payload(300000, 9);
    final asset = ReleaseAsset('veilon-client-universal.apk', _assetUrl, good.length);

    test('damaged first download → deleted, downloaded again, passes', () async {
      final p = '${tmp.path}/u.apk';
      final retries = <String>[];
      final c = _ScriptedClient((path, n) async => File(path).writeAsBytesSync(n == 1 ? bad : good));
      await downloadVerified(c, asset, p, _hex(good), onRetry: (i, e) => retries.add('$i ${e.code}'));
      expect(c.downloads, 2);
      expect(retries, ['1 checksumMismatch']);
      expect(await sha256OfFile(p), _hex(good));
    });

    test('hash could not be computed the first time → retried (no fake mismatch)', () async {
      final p = '${tmp.path}/u.apk';
      var dartCalls = 0;
      final codes = <String>[];
      final c = _ScriptedClient((path, n) async => File(path).writeAsBytesSync(good));
      await downloadVerified(c, asset, p, _hex(good),
          dartHash: (f) async => ++dartCalls == 1 ? throw const FileSystemException('busy') : sha256OfFile(f),
          nativeHash: (_) async => throw 'no hash',
          onRetry: (_, e) => codes.add(e.code));
      expect(codes, ['verifyFailed']);
      expect(c.downloads, 2);
    });

    test('still wrong after the retry → checksumMismatch with both 8-hex prefixes, file removed', () async {
      final p = '${tmp.path}/u.apk';
      final c = _ScriptedClient((path, n) async => File(path).writeAsBytesSync(bad));
      await expectLater(
          downloadVerified(c, asset, p, _hex(good)),
          throwsA(isA<UpdateError>()
              .having((e) => e.code, 'code', 'checksumMismatch')
              .having((e) => e.detail, 'detail', '${_hex(good).substring(0, 8)}/${_hex(bad).substring(0, 8)}')));
      expect(c.downloads, 2, reason: 'exactly one re-download');
      expect(File(p).existsSync(), isFalse);
    });

    test('short file → sizeMismatch, retried; network / cancel errors are not retried', () async {
      final p = '${tmp.path}/u.apk';
      final c = _ScriptedClient((path, n) async => File(path).writeAsBytesSync(n == 1 ? good.sublist(0, 1000) : good));
      await downloadVerified(c, asset, p, _hex(good));
      expect(c.downloads, 2);
      final net = _ScriptedClient((path, n) async => throw const UpdateError('network', 'timeout'));
      await expectLater(downloadVerified(net, asset, p, _hex(good)), throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'network')));
      expect(net.downloads, 1);
      var cancel = false;
      final cc = _ScriptedClient((path, n) async {
        File(path).writeAsBytesSync(bad);
        cancel = true;
      });
      await expectLater(downloadVerified(cc, asset, p, _hex(good), cancelled: () => cancel),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'cancelled')));
      expect(cc.downloads, 1);
      expect(File(p).existsSync(), isFalse);
    });

    test('native-only failure no longer blocks the update (the 1.0.8 bug)', () async {
      // 1.0.8: native.sha256File → '' → "checksum mismatch". Now Dart hashes.
      final p = '${tmp.path}/u.apk';
      final c = _ScriptedClient((path, n) async => File(path).writeAsBytesSync(good));
      await downloadVerified(c, asset, p, _hex(good), nativeHash: (_) async => '');
      expect(c.downloads, 1);
    });
  });

  group('real UpdateClient.download over local HTTPS', () {
    _Tls? srv;
    setUpAll(() async => srv = await _Tls.start());
    tearDownAll(() async => srv?.close());

    test('8 MB body through redirect: on-disk length and hash exact, progress monotonic', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      final body = _payload(8 * 1024 * 1024 + 123);
      s.handler = (req, _) async {
        if (req.uri.host != 'objects.githubusercontent.com' && req.headers.host != 'objects.githubusercontent.com') {
          req.response.statusCode = 302;
          req.response.headers.set(HttpHeaders.locationHeader, 'https://objects.githubusercontent.com/blob/1');
          await req.response.close();
          return;
        }
        await _send(req, body);
      };
      final p = '${tmp.path}/dl.apk';
      final seen = <int>[];
      await s.client().download(ReleaseAsset('veilon-client-universal.apk', _assetUrl, body.length), p, onProgress: (g, _) => seen.add(g));
      expect(File(p).lengthSync(), body.length);
      expect(await sha256OfFile(p), _hex(body));
      expect(seen.last, body.length);
      for (var i = 1; i < seen.length; i++) {
        expect(seen[i] > seen[i - 1], isTrue);
      }
    });

    test('connection cut mid-body → network error (not a checksum error)', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      final body = _payload(2 * 1024 * 1024);
      s.handler = (req, _) => _send(req, body, cutHalf: true);
      await expectLater(s.client().download(ReleaseAsset('a.apk', _assetUrl, body.length), '${tmp.path}/cut.apk'),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', anyOf('network', 'sizeMismatch'))));
    }, timeout: const Timeout(Duration(seconds: 90)));

    test('server sends a different size than the release says → sizeMismatch', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      final body = _payload(100000);
      s.handler = (req, _) => _send(req, body);
      await expectLater(s.client().download(ReleaseAsset('a.apk', _assetUrl, body.length + 5), '${tmp.path}/sz.apk'),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'sizeMismatch')));
    });

    test('redirect to a host outside the allowlist is refused', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      s.handler = (req, _) async {
        req.response.statusCode = 302;
        req.response.headers.set(HttpHeaders.locationHeader, 'https://evil.example/x.apk');
        await req.response.close();
      };
      await expectLater(s.client().download(const ReleaseAsset('a.apk', _assetUrl, 10), '${tmp.path}/ev.apk'),
          throwsA(isA<UpdateError>().having((e) => e.detail, 'detail', contains('host not allowed'))));
    });

    test('cancel during the transfer stops it', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      final body = _payload(4 * 1024 * 1024);
      s.handler = (req, _) => _send(req, body);
      var got = 0;
      await expectLater(
          s.client().download(ReleaseAsset('a.apk', _assetUrl, body.length), '${tmp.path}/cx.apk',
              onProgress: (g, _) => got = g, cancelled: () => got > 1024 * 1024),
          throwsA(isA<UpdateError>().having((e) => e.code, 'code', 'cancelled')));
    });

    test('downloadVerified end to end: first transfer corrupt, second good', () async {
      final s = srv;
      if (s == null) return markTestSkipped('openssl not available');
      final good = _payload(1 << 20), bad = _payload(1 << 20, 3);
      var n = 0;
      s.handler = (req, _) => _send(req, ++n == 1 ? bad : good);
      final p = '${tmp.path}/e2e.apk';
      await downloadVerified(s.client(), ReleaseAsset('veilon-client-universal.apk', _assetUrl, good.length), p, _hex(good));
      expect(n, 2);
      expect(await sha256OfFile(p), _hex(good));
    });
  });

  group('error texts', () {
    test('mismatch shows both 8-hex prefixes; verifyFailed / size show the cause; no ZWNJ', () {
      for (final lang in ['fa', 'en']) {
        final s = S(lang);
        final m = updateErrorText(s, const UpdateError('checksumMismatch', '0f4c6865/e3b0c442'));
        expect(m, allOf(contains('0f4c6865'), contains('e3b0c442')), reason: lang);
        final v = updateErrorText(s, const UpdateError('verifyFailed', 'dart: read failed; native: file missing'));
        expect(v, contains('native: file missing'), reason: lang);
        expect(updateErrorText(s, const UpdateError('sizeMismatch', '1000/62015432')), contains('1000/62015432'));
        expect(updateErrorText(s, const UpdateError('storage', 'No space left on device')), contains('No space left'));
        for (final k in ['update.err.checksumMismatch', 'update.err.verifyFailed', 'update.err.sizeMismatch', 'update.err.storage', 'update.retrying']) {
          expect(s.t(k), isNot(k), reason: '$lang $k');
          expect(s.t(k).contains('\u200c'), isFalse, reason: '$lang $k');
        }
        expect(m.contains('{'), isFalse);
        expect(v.contains('{'), isFalse);
      }
      expect(updateErrorText(S('en'), const UpdateError('verifyFailed')), contains('(?)'));
    });
  });
}
