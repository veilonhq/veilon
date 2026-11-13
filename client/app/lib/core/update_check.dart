// In-app update check (1.0.7): the client release on GitHub.
//
// The newest client is always the release with the fixed tag
// "client-latest" (https://github.com/veilonhq/veilon/releases/tag/client-latest)
// in the panel's repository; each new client version replaces its APKs.
// The panel's own releases ("v0.3.9", …) and "latest" belong to the panel and
// are never read. The release's version is the `Version: X.Y.Z` line of its
// notes (English section first); its assets carry no version:
// veilon-client-<variant>.apk and checksums.txt. The update always installs
// the same variant (arm64-v8a, armeabi-v7a or universal) that is installed
// now; there is no fallback to another variant, and checksums.txt must list
// it.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;

const updateRepo = 'veilonhq/veilon';
const clientReleaseTag = 'client-latest';
const updateReleaseUrl = 'https://api.github.com/repos/$updateRepo/releases/tags/$clientReleaseTag';
const updateReleasePage = 'https://github.com/$updateRepo/releases/tag/$clientReleaseTag';
const checksumsAsset = 'checksums.txt';

/// A semantic version "major.minor.patch" (a "+build" suffix is ignored;
/// any "-pre" suffix makes it a prerelease, which sorts before the release).
class SemVer implements Comparable<SemVer> {
  const SemVer(this.major, this.minor, this.patch, [this.pre = '']);
  final int major, minor, patch;
  final String pre;

  static final _re = RegExp(r'^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$');

  static SemVer? tryParse(String s) {
    final m = _re.firstMatch(s.trim());
    if (m == null) return null;
    return SemVer(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!), m[4] ?? '');
  }

  @override
  int compareTo(SemVer o) {
    for (final d in [major - o.major, minor - o.minor, patch - o.patch]) {
      if (d != 0) return d.sign;
    }
    if (pre == o.pre) return 0;
    if (pre.isEmpty) return 1;
    if (o.pre.isEmpty) return -1;
    return pre.compareTo(o.pre).sign;
  }

  bool operator >(SemVer o) => compareTo(o) > 0;

  @override
  bool operator ==(Object other) => other is SemVer && compareTo(other) == 0;
  @override
  int get hashCode => Object.hash(major, minor, patch, pre);

  @override
  String toString() => '$major.$minor.$patch${pre.isEmpty ? '' : '-$pre'}';
}

final _versionLine = RegExp(r'^\s*[*_`]*\s*Version\s*[*_`]*\s*:\s*[*_`]*\s*v?(\d+\.\d+\.\d+)\s*[*_`]*\s*$', multiLine: true);

/// Version from the release notes: the `Version: X.Y.Z` line, taken from the
/// "## English" section when there is one, else from anywhere in the notes.
SemVer? releaseBodyVersion(String body) {
  final text = body.replaceAll('\r', '');
  final en = RegExp(r'^##\s*English\s*$', multiLine: true, caseSensitive: false).firstMatch(text);
  if (en != null) {
    var part = text.substring(en.end);
    final next = RegExp(r'^##\s', multiLine: true).firstMatch(part);
    if (next != null) part = part.substring(0, next.start);
    final m = _versionLine.firstMatch(part);
    if (m != null) return SemVer.tryParse(m[1]!);
  }
  final m = _versionLine.firstMatch(text);
  return m == null ? null : SemVer.tryParse(m[1]!);
}

class ReleaseAsset {
  const ReleaseAsset(this.name, this.url, this.size);
  final String name;
  final String url;
  final int size;
}

class ClientRelease {
  const ClientRelease({required this.tag, required this.version, required this.name, required this.notes, required this.htmlUrl, required this.assets});
  final String tag;

  /// From the notes' `Version: X.Y.Z` line.
  final SemVer version;
  final String name;
  final String notes;
  final String htmlUrl;
  final List<ReleaseAsset> assets;

  ReleaseAsset? asset(String name) {
    for (final a in assets) {
      if (a.name == name) return a;
    }
    return null;
  }
}

/// The client release from the GitHub "release by tag" answer (one JSON
/// object). Null when it is not the "client-latest" release (panel releases
/// are ignored), is a draft, or its notes have no `Version:` line.
ClientRelease? clientReleaseFrom(Object? r) {
  if (r is! Map) return null;
  if (r['tag_name'] != clientReleaseTag) return null;
  if (r['draft'] == true) return null;
  final body = (r['body'] as String?) ?? '';
  final v = releaseBodyVersion(body);
  if (v == null) return null;
  final assets = <ReleaseAsset>[];
  final list = r['assets'];
  if (list is List) {
    for (final a in list) {
      if (a is! Map) continue;
      final n = a['name'], u = a['browser_download_url'];
      if (n is String && u is String) assets.add(ReleaseAsset(n, u, (a['size'] as num?)?.toInt() ?? 0));
    }
  }
  final name = (r['name'] as String?)?.trim() ?? '';
  return ClientRelease(
    tag: clientReleaseTag,
    version: v,
    name: name.isEmpty ? clientReleaseTag : name,
    notes: body,
    htmlUrl: (r['html_url'] as String?) ?? updateReleasePage,
    assets: assets,
  );
}

/// The APK variant that is installed: the update installs the same one.
enum ApkVariant { arm64, armv7, universal }

extension ApkVariantName on ApkVariant {
  String get abiSuffix => switch (this) {
        ApkVariant.arm64 => 'arm64-v8a',
        ApkVariant.armv7 => 'armeabi-v7a',
        ApkVariant.universal => 'universal',
      };
}

/// Variant from the ABI folders (`lib/<abi>/`) of the installed APK: only
/// arm64-v8a → arm64; only armeabi-v7a → armeabi-v7a; both → universal.
/// Anything else (no native libs, other ABIs) → null (unknown).
ApkVariant? variantFromLibAbis(Iterable<String> abis) {
  final s = abis.toSet();
  final a64 = s.contains('arm64-v8a'), a7 = s.contains('armeabi-v7a');
  if (a64 && a7) return ApkVariant.universal;
  if (a64) return ApkVariant.arm64;
  if (a7) return ApkVariant.armv7;
  return null;
}

/// Release asset name of [variant] (no version in it): veilon-client-arm64-v8a.apk.
String apkAssetName(ApkVariant variant) => 'veilon-client-${variant.abiSuffix}.apk';

class UpdateError implements Exception {
  const UpdateError(this.code, [this.detail = '']);

  /// network | badResponse | rateLimited | noRelease | noVariant | assetMissing |
  /// noChecksum | checksumMismatch | verifyFailed | sizeMismatch | storage |
  /// install | permission | signature | cancelled
  ///
  /// 1.0.9: checksumMismatch carries `<expected 8 hex>/<actual 8 hex>`;
  /// verifyFailed (the file could not be hashed at all) carries the reason.
  final String code;
  final String detail;
  @override
  String toString() => detail.isEmpty ? 'update.$code' : 'update.$code: $detail';
}

/// The APK asset of exactly [variant]. Throws [UpdateError] `assetMissing`
/// when the release does not have it (never another variant instead), and
/// `noChecksum` when the release has no checksums.txt.
ReleaseAsset selectApkAsset(ClientRelease r, ApkVariant variant) {
  final name = apkAssetName(variant);
  final a = r.asset(name);
  if (a == null) throw UpdateError('assetMissing', name);
  if (r.asset(checksumsAsset) == null) throw const UpdateError('noChecksum', checksumsAsset);
  return a;
}

/// Parses a `sha256sum` output (`<64 hex>  <name>` or `<hex> *<name>`)
/// into name → lower-case hex. Other lines are ignored.
Map<String, String> parseChecksums(String text) {
  final out = <String, String>{};
  final re = RegExp(r'^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$');
  for (final line in const LineSplitter().convert(text)) {
    final m = re.firstMatch(line.trim());
    if (m == null) continue;
    var name = m[2]!.trim();
    if (name.startsWith('./')) name = name.substring(2);
    out[name] = m[1]!.toLowerCase();
  }
  return out;
}

/// Expected sha256 of [asset] from the parsed checksums; throws
/// `noChecksum` when the file is not listed.
String expectedSha256(Map<String, String> sums, String asset) {
  final h = sums[asset];
  if (h == null) throw UpdateError('noChecksum', asset);
  return h;
}

/// First 8 hex characters of a hash (for error texts).
String shortHash(String h) {
  final t = h.trim().toLowerCase();
  return t.length <= 8 ? t : t.substring(0, 8);
}

/// Throws unless [actual] equals [expected] (hex, any case). An empty or
/// non-hex [actual] means the file could not be hashed: `verifyFailed`
/// (1.0.8 reported that as a mismatch). A real difference is
/// `checksumMismatch` with detail `<expected 8>/<actual 8>`.
void verifySha256(String expected, String actual) {
  final a = actual.trim().toLowerCase();
  if (!RegExp(r'^[0-9a-f]{64}$').hasMatch(a)) {
    throw UpdateError('verifyFailed', a.isEmpty ? 'no hash' : 'bad hash "${shortHash(a)}"');
  }
  if (expected.trim().toLowerCase() != a) {
    throw UpdateError('checksumMismatch', '${shortHash(expected)}/${shortHash(a)}');
  }
}

class _DigestSink implements Sink<crypto.Digest> {
  crypto.Digest? value;
  @override
  void add(crypto.Digest d) => value = d;
  @override
  void close() {}
}

/// Lower-case hex sha256 of the file at [path], computed in Dart while
/// streaming the file (1.0.9: no native call involved, nothing read whole).
Future<String> sha256OfFile(String path) async {
  final out = _DigestSink();
  final input = crypto.sha256.startChunkedConversion(out);
  await for (final chunk in File(path).openRead()) {
    input.add(chunk);
  }
  input.close();
  return out.value.toString();
}

typedef FileHasher = Future<String> Function(String path);

String _fsReason(Object e) {
  if (e is FileSystemException) {
    final os = e.osError;
    return os == null ? e.message : '${e.message}: ${os.message} (errno ${os.errorCode})';
  }
  return '$e';
}

/// sha256 of the downloaded file: Dart ([dart], default [sha256OfFile])
/// first; [native] only when Dart could not read the file. Throws
/// `verifyFailed` with the reason(s) when no hash could be computed.
Future<String> hashDownloaded(String path, {FileHasher? dart, FileHasher? native}) async {
  String? dartErr;
  try {
    final h = await (dart ?? sha256OfFile)(path);
    if (RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(h.trim())) return h.trim().toLowerCase();
    dartErr = 'dart: empty hash';
  } catch (e) {
    dartErr = 'dart: ${_fsReason(e)}';
  }
  if (native != null) {
    try {
      final h = (await native(path)).trim();
      if (RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(h)) return h.toLowerCase();
      throw UpdateError('verifyFailed', '$dartErr; native: no hash');
    } on UpdateError {
      rethrow;
    } catch (e) {
      throw UpdateError('verifyFailed', '$dartErr; native: $e');
    }
  }
  throw UpdateError('verifyFailed', dartErr);
}

/// Checks the downloaded file at [path]: its length on disk must equal the
/// asset size (when GitHub gave one), then its sha256 must equal [expected].
Future<void> verifyDownloaded(ReleaseAsset a, String path, String expected, {FileHasher? dart, FileHasher? native}) async {
  int len;
  try {
    len = await File(path).length();
  } catch (e) {
    throw UpdateError('verifyFailed', 'cannot read file: ${_fsReason(e)}');
  }
  if (a.size > 0 && len != a.size) throw UpdateError('sizeMismatch', '$len/${a.size}');
  verifySha256(expected, await hashDownloaded(path, dart: dart, native: native));
}

/// Errors after which the file is deleted and downloaded once more.
const retryableUpdateErrors = {'checksumMismatch', 'verifyFailed', 'sizeMismatch'};

Future<void> _deleteQuietly(String path) async {
  try {
    final f = File(path);
    if (await f.exists()) await f.delete();
  } catch (_) {}
}

/// Downloads [a] to [path] and verifies it (size on disk + sha256). On a
/// mismatch, a failed verification or a short file, the file is deleted and
/// downloaded once more ([attempts] in total). A failed file never stays on
/// disk. [onVerify] is called before each verification, [onRetry] before
/// the second download.
Future<void> downloadVerified(
  UpdateClient client,
  ReleaseAsset a,
  String path,
  String expected, {
  Progress? onProgress,
  bool Function()? cancelled,
  FileHasher? dartHash,
  FileHasher? nativeHash,
  void Function()? onVerify,
  void Function(int attempt, UpdateError error)? onRetry,
  int attempts = 2,
}) async {
  for (var i = 1;; i++) {
    try {
      await client.download(a, path, onProgress: onProgress, cancelled: cancelled);
      if (cancelled?.call() == true) throw const UpdateError('cancelled');
      onVerify?.call();
      await verifyDownloaded(a, path, expected, dart: dartHash, native: nativeHash);
      if (cancelled?.call() == true) throw const UpdateError('cancelled');
      return;
    } catch (e) {
      await _deleteQuietly(path);
      if (e is! UpdateError) rethrow;
      if (!retryableUpdateErrors.contains(e.code) || i >= attempts || cancelled?.call() == true) rethrow;
      onRetry?.call(i, e);
    }
  }
}

/// First lines of the release notes for the dialog: the section for
/// [lang] ("## فارسی" / "## English") when the notes have one, markdown
/// markers stripped, at most [maxChars].
String notesSnippet(String body, String lang, {int maxChars = 600}) {
  final lines = const LineSplitter().convert(body.replaceAll('\r', ''));
  final want = lang == 'fa' ? 'فارسی' : 'english';
  var start = -1, end = lines.length;
  for (var i = 0; i < lines.length; i++) {
    final l = lines[i].trim();
    if (l.startsWith('## ')) {
      if (start >= 0) {
        end = i;
        break;
      }
      if (l.substring(3).trim().toLowerCase() == want) start = i + 1;
    }
  }
  final part = start >= 0 ? lines.sublist(start, end) : lines;
  final text = part
      .map((l) => l.replaceAll(RegExp(r'\*\*|`|^#+\s*'), '').trimRight())
      .join('\n')
      .replaceAll(RegExp(r'\n{3,}'), '\n\n')
      .trim();
  return text.length <= maxChars ? text : '${text.substring(0, maxChars).trimRight()}…';
}

/// Result of a check.
class UpdateCheck {
  const UpdateCheck(this.current, this.latest);
  final SemVer current;
  final ClientRelease? latest;
  bool get hasUpdate => latest != null && latest!.version > current;
}

typedef Progress = void Function(int received, int total);

/// 1.0.8 (audit L5): the only hosts the updater talks to, https only. The
/// release API and every asset download (including GitHub's redirects to
/// its asset storage) must stay on these.
const updateHosts = {
  'api.github.com',
  'github.com',
  'objects.githubusercontent.com',
  'release-assets.githubusercontent.com',
};

/// Whether [url] is an https URL on [updateHosts].
bool isAllowedUpdateUrl(Uri url) =>
    url.scheme == 'https' && updateHosts.contains(url.host.toLowerCase()) && url.userInfo.isEmpty;

/// Opens a GET on an allowed URL and follows redirects by hand (at most 5),
/// each hop checked against [updateHosts] (the automatic redirect handling
/// followed any host and scheme).
Future<HttpClientResponse> openAllowed(HttpClient c, String url, void Function(HttpClientRequest req) headers,
    {required Duration timeout}) async {
  var uri = Uri.parse(url);
  for (var hop = 0; hop <= 5; hop++) {
    if (!isAllowedUpdateUrl(uri)) throw UpdateError('badResponse', 'host not allowed: ${uri.host}');
    final req = await c.getUrl(uri).timeout(timeout);
    req.followRedirects = false;
    headers(req);
    final res = await req.close().timeout(timeout);
    if (res.isRedirect || const [301, 302, 303, 307, 308].contains(res.statusCode)) {
      final loc = res.headers.value(HttpHeaders.locationHeader);
      await res.drain<void>().catchError((_) {});
      if (loc == null || loc.isEmpty) throw UpdateError('badResponse', 'HTTP ${res.statusCode}');
      uri = uri.resolve(loc);
      continue;
    }
    return res;
  }
  throw const UpdateError('badResponse', 'too many redirects');
}

/// GitHub client: the releases list (unauthenticated) and asset downloads.
/// The app is excluded from its own VPN, so this works with the VPN on or off.
class UpdateClient {
  UpdateClient({HttpClient Function()? httpFactory}) : _factory = httpFactory ?? HttpClient.new;
  final HttpClient Function() _factory;

  static const _ua = 'Veilon-Android-Updater';

  Future<UpdateCheck> check(String currentVersion) async {
    final cur = SemVer.tryParse(currentVersion) ?? const SemVer(0, 0, 0);
    final body = await _getText(updateReleaseUrl, json: true, timeout: const Duration(seconds: 15));
    Object? data;
    try {
      data = jsonDecode(body);
    } catch (_) {
      throw const UpdateError('badResponse');
    }
    final r = clientReleaseFrom(data);
    if (r == null) throw const UpdateError('noRelease');
    return UpdateCheck(cur, r);
  }

  Future<String> fetchChecksums(ClientRelease r) async {
    final a = r.asset(checksumsAsset);
    if (a == null) throw const UpdateError('noChecksum', checksumsAsset);
    return _getText(a.url, timeout: const Duration(seconds: 30));
  }

  Future<String> _getText(String url, {bool json = false, required Duration timeout}) async {
    final c = _factory()..connectionTimeout = const Duration(seconds: 12);
    try {
      final res = await openAllowed(c, url, (req) {
        req.headers.set(HttpHeaders.userAgentHeader, _ua);
        if (json) {
          req.headers.set(HttpHeaders.acceptHeader, 'application/vnd.github+json');
          req.headers.set('X-GitHub-Api-Version', '2022-11-28');
        }
      }, timeout: timeout);
      final text = await res.transform(utf8.decoder).join().timeout(timeout);
      if (res.statusCode == 403 || res.statusCode == 429) throw const UpdateError('rateLimited');
      if (res.statusCode == 404 && json) throw const UpdateError('noRelease');
      if (res.statusCode != 200) throw UpdateError('badResponse', 'HTTP ${res.statusCode}');
      return text;
    } on UpdateError {
      rethrow;
    } on TimeoutException {
      throw const UpdateError('network', 'timeout');
    } on SocketException catch (e) {
      throw UpdateError('network', e.message);
    } on HandshakeException catch (e) {
      throw UpdateError('network', e.message);
    } on HttpException catch (e) {
      throw UpdateError('network', e.message);
    } finally {
      c.close(force: true);
    }
  }

  /// Downloads [a] to [path] with progress. 1.0.9: the body goes to the
  /// file through `addStream` (backpressure: no unbounded buffering of a
  /// 62 MB APK), and after the file is closed its length on disk must equal
  /// the bytes received and the asset size.
  Future<void> download(ReleaseAsset a, String path, {Progress? onProgress, bool Function()? cancelled}) async {
    final c = _factory()..connectionTimeout = const Duration(seconds: 15);
    final f = File(path);
    IOSink? sink;
    try {
      final res = await openAllowed(c, a.url, (req) {
        req.headers.set(HttpHeaders.userAgentHeader, _ua);
        req.headers.set(HttpHeaders.acceptHeader, 'application/octet-stream');
      }, timeout: const Duration(seconds: 30));
      if (res.statusCode != 200) throw UpdateError('badResponse', 'HTTP ${res.statusCode}');
      final total = res.contentLength > 0 ? res.contentLength : a.size;
      await f.parent.create(recursive: true);
      sink = f.openWrite();
      var got = 0;
      // A stalled transfer (no bytes for 30 s) is a network error.
      final body = res.timeout(const Duration(seconds: 30)).map((chunk) {
        if (cancelled?.call() == true) throw const UpdateError('cancelled');
        got += chunk.length;
        onProgress?.call(got, total);
        return chunk;
      });
      await sink.addStream(body);
      await sink.flush();
      await sink.close();
      sink = null;
      final written = await f.length();
      if (written != got) throw UpdateError('sizeMismatch', 'written $written of $got');
      if (a.size > 0 && written != a.size) throw UpdateError('sizeMismatch', '$written/${a.size}');
    } on UpdateError {
      rethrow;
    } on TimeoutException {
      throw const UpdateError('network', 'timeout');
    } on SocketException catch (e) {
      throw UpdateError('network', e.message);
    } on HandshakeException catch (e) {
      throw UpdateError('network', e.message);
    } on HttpException catch (e) {
      throw UpdateError('network', e.message);
    } on FileSystemException catch (e) {
      throw UpdateError('storage', _fsReason(e));
    } finally {
      try {
        await sink?.close();
      } catch (_) {}
      c.close(force: true);
    }
  }
}
