import 'dart:convert';

/// Field schema for the profile editor. Mirrors the Profile JSON owned by the
/// Go core (core/veiloncore/profile.go, documented in core/API.md). Paths are
/// dotted (`wg.awg.jc`, `dns.domain`, `ssh.host`). Keys the schema does not
/// know are preserved untouched (and are editable in the raw-JSON section).

enum FKind { text, secret, multiline, integer, decimal, toggle, choice, list, intList, maskList }

typedef Vals = Map<String, String>;

class FieldSpec {
  const FieldSpec(
    this.path,
    this.en,
    this.fa, {
    this.kind = FKind.text,
    this.choices = const [],
    this.hintEn = '',
    this.hintFa = '',
    this.required = false,
    this.keepEmpty = false,
    this.check,
    this.min,
    this.max,
    this.visible,
    this.choiceLabels = const {},
    this.maskTypes = const [],
    this.emptyEn = '',
    this.emptyFa = '',
  });

  final String path;
  final String en;
  final String fa;
  final FKind kind;
  final List<String> choices;
  final String hintEn;
  final String hintFa;
  final bool required;

  /// Core field without `omitempty`: written as "" / [] instead of removed.
  final bool keepEmpty;

  /// Extra format check on a non-empty value; returns an error key or null.
  final String? Function(String v)? check;
  final num? min;
  final num? max;
  final bool Function(Vals v)? visible;

  /// Display names for choice values (value -> label).
  final Map<String, String> choiceLabels;

  /// Suggested `type` values for a [FKind.maskList].
  final List<String> maskTypes;

  /// Label of the empty choice ("" = generic "Default").
  final String emptyEn;
  final String emptyFa;
  String emptyLabel(bool fa) => fa ? emptyFa : emptyEn;

  String label(bool fa) => fa ? this.fa : en;
  String hint(bool fa) => fa ? hintFa : hintEn;
  bool shown(Vals v) => visible?.call(v) ?? true;
}

class FieldGroup {
  const FieldGroup(this.id, this.en, this.fa, this.fields, {this.visible});
  final String id;
  final String en;
  final String fa;
  final List<FieldSpec> fields;
  final bool Function(Vals v)? visible;
  String label(bool fa) => fa ? this.fa : en;
  bool shown(Vals v) => visible?.call(v) ?? true;
}

const profileTypes = [
  'vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'amneziawg', 'http', 'socks', 'mtproto', 'masque',
];

/// Types offered by "Add manually": every Xray / sing-box protocol.
/// AmneziaWG is not created by hand: it is added only by importing its
/// .conf file or pasting the config text.
const manualTypes = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'http', 'socks', 'mtproto', 'masque'];

/// Types whose engine the user may choose (profile "core" / Settings.Core).
const v2CoreTypes = {'vless', 'vmess', 'trojan', 'shadowsocks'};

// ------------------------------------------------------------ validators
// Each returns an error key (see editorErrors) or null.

final _uuidRe = RegExp(r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$');
final _hostRe = RegExp(r'^[A-Za-z0-9_.\-]+$');
final _ipv6Re = RegExp(r'^\[?[0-9A-Fa-f:.]+(%[A-Za-z0-9]+)?\]?$');
final _b64Key = RegExp(r'^[A-Za-z0-9+/]{43}=$');
final _b64urlKey = RegExp(r'^[A-Za-z0-9_\-]{43}=?$');
final _hex = RegExp(r'^[0-9a-fA-F]*$');

String? checkHost(String v) {
  if (v.contains('://') || v.contains(' ') || v.contains('/')) return 'host';
  if (_hostRe.hasMatch(v) || (v.contains(':') && _ipv6Re.hasMatch(v))) return null;
  return 'host';
}

String? checkHostPort(String v) {
  final i = v.lastIndexOf(':');
  if (i <= 0) return 'hostPort';
  final port = int.tryParse(v.substring(i + 1));
  if (port == null || port < 1 || port > 65535) return 'hostPort';
  var h = v.substring(0, i);
  if (h.startsWith('[') && h.endsWith(']')) h = h.substring(1, h.length - 1);
  return checkHost(h) == null ? null : 'hostPort';
}

/// Host override: `host`, `host:port`, `[v6]` or `[v6]:port` (core/API.md).
String? checkOverride(String v) {
  if (v.startsWith('[')) {
    final end = v.indexOf(']');
    if (end < 0) return 'override';
    final rest = v.substring(end + 1);
    if (rest.isNotEmpty && (!rest.startsWith(':') || _badPort(rest.substring(1)))) return 'override';
    return _ipv6Re.hasMatch(v.substring(1, end)) ? null : 'override';
  }
  final colons = ':'.allMatches(v).length;
  if (colons == 1) {
    final i = v.indexOf(':');
    if (_badPort(v.substring(i + 1))) return 'override';
    return checkHost(v.substring(0, i)) == null ? null : 'override';
  }
  return checkHost(v) == null ? null : 'override';
}

bool _badPort(String s) {
  final n = int.tryParse(s);
  return n == null || n < 1 || n > 65535;
}

String? checkUuid(String v) => _uuidRe.hasMatch(v) ? null : 'uuid';

/// Xray accepts a UUID or any 1–30 char string (mapped to a UUIDv5).
String? checkXrayId(String v) => _uuidRe.hasMatch(v) || (v.length <= 30 && !v.contains(' ')) ? null : 'uuid';

String? checkWgKey(String v) => _b64Key.hasMatch(v) ? null : 'wgKey';

String? checkRealityKey(String v) => _b64urlKey.hasMatch(v) ? null : 'realityKey';

String? checkShortId(String v) => _hex.hasMatch(v) && v.length <= 16 && v.length.isEven ? null : 'shortId';

String? checkHex(String v) => _hex.hasMatch(v) && v.length.isEven ? null : 'hex';

String? checkCidr(String v) {
  final parts = v.split('/');
  if (parts.length != 2) return 'cidr';
  final bits = int.tryParse(parts[1]);
  final ip = parts[0];
  final v6 = ip.contains(':');
  if (bits == null || bits < 0 || bits > (v6 ? 128 : 32)) return 'cidr';
  if (v6) return _ipv6Re.hasMatch(ip) ? null : 'cidr';
  final o = ip.split('.');
  if (o.length != 4 || o.any((x) => (int.tryParse(x) ?? -1) < 0 || int.parse(x) > 255)) return 'cidr';
  return null;
}

String? checkPorts(String v) {
  for (final part in v.split(',')) {
    final r = part.trim().split(RegExp('[-:]'));
    if (r.isEmpty || r.length > 2) return 'ports';
    for (final x in r) {
      final n = int.tryParse(x.trim());
      if (n == null || n < 1 || n > 65535) return 'ports';
    }
  }
  return null;
}

String? checkUrl(String v) {
  final u = Uri.tryParse(v);
  return u != null && (u.scheme == 'https' || u.scheme == 'http') && u.host.isNotEmpty ? null : 'url';
}

String? checkPath(String v) => v.startsWith('/') ? null : 'path';

String? checkJsonObject(String v) {
  try {
    return jsonDecode(v) is Map ? null : 'json';
  } catch (_) {
    return 'json';
  }
}

/// Splits a list field: one item per line or comma.
List<String> splitList(String v) =>
    v.split(RegExp(r'[\n,]')).map((e) => e.trim()).where((e) => e.isNotEmpty).toList();

const editorErrors = <String, (String, String)>{
  'required': ('Required', 'الزامی است'),
  'int': ('Must be a whole number', 'باید عدد صحیح باشد'),
  'number': ('Must be a number', 'باید عدد باشد'),
  'range': ('Out of range ({min}–{max})', 'خارج از محدوده ({min} تا {max})'),
  'host': ('Invalid host (IP or domain, no scheme)', 'آدرس نامعتبر (IP یا دامنه، بدون پروتکل)'),
  'override': ('Use host, host:port, [IPv6] or [IPv6]:port', 'به شکل host یا host:port یا [IPv6]:port وارد کنید'),
  'hostPort': ('Use host:port', 'به شکل host:port وارد کنید'),
  'uuid': ('Invalid UUID', 'UUID نامعتبر'),
  'wgKey': ('Must be a 44-char base64 key', 'کلید باید base64 و ۴۴ نویسه باشد'),
  'realityKey': ('Must be a 43-char base64url key', 'کلید باید base64url و ۴۳ نویسه باشد'),
  'shortId': ('Hex, even length, max 16', 'هگز با طول زوج، حداکثر ۱۶'),
  'hex': ('Hex with even length', 'هگز با طول زوج'),
  'cidr': ('Use CIDR, e.g. 10.0.0.2/32', 'به شکل CIDR، مثلاً 10.0.0.2/32'),
  'ports': ('Ports or ranges, e.g. 443,20000-30000', 'پورت یا بازه، مثلاً 443,20000-30000'),
  'url': ('Invalid URL', 'نشانی نامعتبر'),
  'path': ('Path must start with /', 'مسیر باید با / شروع شود'),
  'json': ('Must be a JSON object', 'باید یک شیء JSON باشد'),
  'choice': ('Pick one of the options', 'یکی از گزینه ها را انتخاب کنید'),
  'maskType': ('Every mask needs a type', 'هر ماسک باید نوع داشته باشد'),
  'range2': ('Use a number or range, e.g. 100-1000', 'عدد یا بازه وارد کنید، مثلاً 100-1000'),
};

String? Function(String) eachItem(String? Function(String) f) => (v) {
      for (final x in splitList(v)) {
        final e = f(x);
        if (e != null) return e;
      }
      return null;
    };

/// Validates one field value. Returns (errorKey, min, max) or null when valid.
(String, num?, num?)? validateField(FieldSpec f, String raw) {
  final v = raw.trim();
  if (v.isEmpty) return f.required ? ('required', null, null) : null;
  switch (f.kind) {
    case FKind.integer:
      final n = int.tryParse(v);
      if (n == null) return ('int', null, null);
      if ((f.min != null && n < f.min!) || (f.max != null && n > f.max!)) return ('range', f.min, f.max);
    case FKind.decimal:
      final n = double.tryParse(v);
      if (n == null) return ('number', null, null);
      if ((f.min != null && n < f.min!) || (f.max != null && n > f.max!)) return ('range', f.min, f.max);
    case FKind.intList:
      for (final x in splitList(v)) {
        final n = int.tryParse(x);
        if (n == null) return ('int', null, null);
        if ((f.min != null && n < f.min!) || (f.max != null && n > f.max!)) return ('range', f.min, f.max);
      }
    case FKind.choice:
      if (f.choices.isNotEmpty && !f.choices.contains(v)) return ('choice', null, null);
    case FKind.maskList:
      final items = decodeMaskVals(v);
      if (items == null) return ('json', null, null);
      for (final m in items) {
        if ((m['type'] ?? '').trim().isEmpty) return ('maskType', null, null);
        final st = (m['settings'] ?? '').trim();
        if (st.isNotEmpty && checkJsonObject(st) != null) return ('json', null, null);
      }
    default:
      break;
  }
  final e = f.check?.call(v);
  return e == null ? null : (e, f.min, f.max);
}

String errorText((String, num?, num?) e, bool fa) {
  final (key, mn, mx) = e;
  final pair = editorErrors[key] ?? (key, key);
  return (fa ? pair.$2 : pair.$1).replaceAll('{min}', '${mn ?? ''}').replaceAll('{max}', '${mx ?? ''}');
}

// ------------------------------------------------------------ groups

bool _is(Vals v, String k, Set<String> any) => any.contains(v[k] ?? '');

// Transports (`network`). Values follow the Xray names the core accepts
// ("tcp" is RAW; "kcp" is mKCP). XDRIVE is VLESS-only in Xray.
const _networks = ['', 'tcp', 'kcp', 'ws', 'grpc', 'httpupgrade', 'xhttp', 'xdrive', 'h2', 'quic'];
const _networksNoXdrive = ['', 'tcp', 'kcp', 'ws', 'grpc', 'httpupgrade', 'xhttp', 'h2', 'quic'];
const networkLabels = {
  'tcp': 'RAW (TCP)',
  'raw': 'RAW (TCP)',
  'kcp': 'mKCP',
  'mkcp': 'mKCP',
  'ws': 'WebSocket',
  'grpc': 'gRPC',
  'httpupgrade': 'HTTPUpgrade',
  'xhttp': 'XHTTP',
  'splithttp': 'XHTTP',
  'xdrive': 'XDRIVE',
  'h2': 'HTTP/2 (legacy)',
  'quic': 'QUIC (legacy)',
};
const rawHeaderTypes = ['', 'none', 'http'];
const kcpHeaderTypes = ['', 'none', 'srtp', 'utp', 'wechat-video', 'dtls', 'wireguard', 'dns'];
const grpcModes = ['', 'gun', 'multi'];
const xhttpModes = ['', 'auto', 'packet-up', 'stream-up', 'stream-one'];
const tcpMaskTypes = ['header-custom', 'fragment', 'sudoku', 'filter'];
const udpMaskTypes = ['header-custom', 'mkcp-legacy', 'noise', 'salamander', 'sudoku', 'xdns', 'xicmp', 'realm', 'udphop', 'filter'];

/// Profile keys of the Xray transport extras (core/veiloncore/profile.go).
/// `xdrive`, `finalMask`, `sockopt` and `mux` are *raw JSON strings* in the
/// profile; the editor expands them to objects while editing and encodes
/// them back on save (see [jsonStringKeys]).
abstract final class TKeys {
  static const seed = 'seed';
  static const kcp = 'kcp'; // KCPConfig object: mtu, tti, uplinkCapacity, downlinkCapacity, cwndMultiplier, maxSendingWindow
  static const xdrive = 'xdrive'; // xdriveSettings (raw JSON string)
  static const tcpMasks = 'finalMask.tcp'; // finalMask raw JSON {"tcp":[{type,settings}],"udp":[…],"quicParams":{…}}
  static const udpMasks = 'finalMask.udp';
  static const sockopt = 'sockopt'; // raw JSON string
  static const mux = 'mux'; // raw JSON string {"enabled":true,"concurrency":8,…}
  static const authority = 'authority'; // gRPC
}

/// Profile keys whose value is a JSON object serialized into a string.
const jsonStringKeys = {'xdrive', 'finalMask', 'sockopt', 'mux'};

const _fps = ['', 'chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random', 'randomized'];
const _ssMethods = [
  '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305', //
  'aes-128-gcm', 'aes-192-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305', 'xchacha20-ietf-poly1305', 'none',
];

FieldGroup _general(String type) => FieldGroup('general', 'General', 'عمومی', [
      const FieldSpec('name', 'Name', 'نام'),
      const FieldSpec('type', 'Protocol', 'پروتکل', kind: FKind.choice, choices: profileTypes, required: true),
      FieldSpec('server', 'Server address', 'آدرس سرور',
          required: true, check: checkHost, hintEn: 'Domain or IP', hintFa: 'دامنه یا IP'),
      FieldSpec('port', 'Port', 'پورت',
          kind: FKind.integer, min: 1, max: 65535, required: true),
      const FieldSpec('addressOverride', 'Host override', 'آدرس جایگزین (Host override)',
          check: checkOverride,
          hintEn: 'host or host:port to dial instead; SNI / Host header keep the original name',
          hintFa: 'host یا host:port برای اتصال به جای سرور؛ SNI و هدر Host همان نام اصلی می ماند'),
      if (v2CoreTypes.contains(type))
        const FieldSpec('core', 'Core', 'هسته',
            kind: FKind.choice,
            choices: ['', 'sing-box', 'xray'],
            choiceLabels: {'sing-box': 'sing-box', 'xray': 'Xray'},
            emptyEn: 'Auto (app setting)',
            emptyFa: 'خودکار (تنظیم برنامه)',
            hintEn: 'Overrides the core chosen in Settings for this server',
            hintFa: 'برای این سرور بر هسته انتخاب شده در تنظیمات اولویت دارد'),
    ]);

FieldGroup _transport(String type) => FieldGroup('transport', 'Transport', 'ترنسپورت', [
      FieldSpec('network', 'Transport', 'نوع ترنسپورت',
          kind: FKind.choice, choices: type == 'vless' ? _networks : _networksNoXdrive, choiceLabels: networkLabels),
      // RAW
      const FieldSpec('headerType', 'RAW header', 'هدر RAW',
          kind: FKind.choice, choices: rawHeaderTypes, visible: _raw,
          hintEn: 'http = HTTP/1.1 request camouflage', hintFa: 'http = استتار به شکل درخواست HTTP/1.1'),
      const FieldSpec('host', 'HTTP header Host', 'Host در هدر HTTP',
          visible: _rawHttp, hintEn: 'Comma separated', hintFa: 'با کاما جدا کنید'),
      const FieldSpec('path', 'HTTP header path', 'مسیر هدر HTTP', check: checkPath, visible: _rawHttp),
      // mKCP header (seed / tuning in the mKCP group)
      const FieldSpec('headerType', 'mKCP header', 'هدر mKCP',
          kind: FKind.choice, choices: kcpHeaderTypes, visible: _kcp),
      // WebSocket / HTTPUpgrade / XHTTP / legacy h2
      const FieldSpec('path', 'Path', 'مسیر (Path)', check: checkPath, visible: _pathNet),
      const FieldSpec('host', 'Host', 'Host', visible: _pathNet),
      // gRPC
      const FieldSpec('serviceName', 'Service name', 'نام سرویس', visible: _grpc),
      const FieldSpec(TKeys.authority, 'Authority', 'Authority', visible: _grpc),
      const FieldSpec('mode', 'gRPC mode', 'حالت gRPC', kind: FKind.choice, choices: grpcModes, visible: _grpc),
      // XHTTP
      const FieldSpec('mode', 'XHTTP mode', 'حالت XHTTP', kind: FKind.choice, choices: xhttpModes, visible: _xhttp),
      const FieldSpec('xPaddingBytes', 'Padding bytes', 'بایت های padding',
          visible: _xhttp, check: _range, hintEn: 'e.g. 100-1000', hintFa: 'مثلاً 100-1000'),
      const FieldSpec('xhttpExtra', 'XHTTP extra (JSON)', 'تنظیمات اضافه XHTTP (JSON)',
          kind: FKind.multiline, check: checkJsonObject, visible: _xhttp,
          hintEn: 'xmux, headers, padding, downloadSettings…', hintFa: 'xmux، هدرها، padding، downloadSettings و…'),
      // legacy QUIC
      const FieldSpec('headerType', 'QUIC header', 'هدر QUIC',
          kind: FKind.choice, choices: kcpHeaderTypes, visible: _quic),
    ]);

const _kcpGroup = FieldGroup('kcp', 'mKCP', 'mKCP', [
  FieldSpec(TKeys.seed, 'Seed', 'Seed', kind: FKind.secret, hintEn: 'Obfuscation password', hintFa: 'رمز مبهم سازی'),
  FieldSpec('${TKeys.kcp}.mtu', 'MTU', 'MTU', kind: FKind.integer, min: 576, max: 1460),
  FieldSpec('${TKeys.kcp}.tti', 'TTI (ms)', 'TTI (میلی ثانیه)', kind: FKind.integer, min: 10, max: 100),
  FieldSpec('${TKeys.kcp}.uplinkCapacity', 'Uplink capacity (MB/s)', 'ظرفیت آپلود (MB/s)', kind: FKind.integer, min: 0, max: 10000),
  FieldSpec('${TKeys.kcp}.downlinkCapacity', 'Downlink capacity (MB/s)', 'ظرفیت دانلود (MB/s)', kind: FKind.integer, min: 0, max: 10000),
  FieldSpec('${TKeys.kcp}.cwndMultiplier', 'Congestion-window multiplier', 'ضریب پنجره ازدحام', kind: FKind.integer, min: 1, max: 100),
  FieldSpec('${TKeys.kcp}.maxSendingWindow', 'Max sending window', 'حداکثر پنجره ارسال', kind: FKind.integer, min: 0, max: 1 << 30),
  FieldSpec(TKeys.udpMasks, 'UDP masks', 'ماسک های UDP', kind: FKind.maskList, maskTypes: udpMaskTypes,
      hintEn: 'FinalMask UDP layers, innermost first', hintFa: 'لایه های UDP در FinalMask، داخلی ترین اول'),
], visible: _kcp);

bool _xdriveS3(Vals v) => (v['${TKeys.xdrive}.service'] ?? '') == 's3';
bool _xdriveTemplate(Vals v) => (v['${TKeys.xdrive}.service'] ?? '') == 'template';

const _xdriveGroup = FieldGroup('xdrive', 'XDRIVE', 'XDRIVE', [
  FieldSpec('${TKeys.xdrive}.service', 'Storage service', 'سرویس ذخیره سازی', required: true,
      hintEn: 'e.g. gdrive, onedrive, yandex, s3, ftp, template', hintFa: 'مثلاً gdrive، onedrive، yandex، s3، ftp، template'),
  FieldSpec('${TKeys.xdrive}.remoteFolder', 'Remote folder', 'پوشه راه دور', required: true),
  FieldSpec('${TKeys.xdrive}.secrets', 'Secrets / tokens', 'رمزها / توکن ها',
      kind: FKind.list, hintEn: 'One per line', hintFa: 'هر خط یکی'),
  FieldSpec('${TKeys.xdrive}.s3.endpoint', 'S3 endpoint', 'آدرس S3', check: checkUrl, visible: _xdriveS3),
  FieldSpec('${TKeys.xdrive}.s3.region', 'S3 region', 'منطقه S3', visible: _xdriveS3),
  FieldSpec('${TKeys.xdrive}.s3.bucket', 'S3 bucket', 'باکت S3', visible: _xdriveS3),
  FieldSpec('${TKeys.xdrive}.s3.pathStyle', 'S3 path-style URLs', 'آدرس path-style در S3', kind: FKind.toggle, visible: _xdriveS3),
  FieldSpec('${TKeys.xdrive}.template', 'Template (JSON)', 'قالب (JSON)',
      kind: FKind.multiline, check: checkJsonObject, visible: _xdriveTemplate,
      hintEn: 'auth / put / get / list / delete operations', hintFa: 'عملیات auth / put / get / list / delete'),
  FieldSpec('${TKeys.xdrive}.segmentBytes', 'Segment size (bytes)', 'اندازه قطعه (بایت)', kind: FKind.integer, min: 0, max: 1 << 30),
  FieldSpec('${TKeys.xdrive}.flushIntervalMs', 'Flush interval (ms)', 'فاصله flush (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
  FieldSpec('${TKeys.xdrive}.pollIntervalMs', 'Poll interval (ms)', 'فاصله poll (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
  FieldSpec('${TKeys.xdrive}.maxPollIntervalMs', 'Max poll interval (ms)', 'حداکثر فاصله poll (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
  FieldSpec('${TKeys.xdrive}.sessionTtlSeconds', 'Session TTL (s)', 'عمر نشست (ثانیه)', kind: FKind.integer, min: 0, max: 86400),
  FieldSpec('${TKeys.xdrive}.concurrency', 'Concurrency', 'همزمانی', kind: FKind.integer, min: 0, max: 256),
  FieldSpec('${TKeys.xdrive}.eagerWindowMs', 'Eager window (ms)', 'پنجره eager (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
  FieldSpec('${TKeys.xdrive}.holeTimeoutMs', 'Hole timeout (ms)', 'مهلت hole (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
], visible: _xdrive);

const _masksGroup = FieldGroup('masks', 'TCP masks (FinalMask)', 'ماسک های TCP (FinalMask)', [
  FieldSpec(TKeys.tcpMasks, 'TCP masks', 'ماسک های TCP', kind: FKind.maskList, maskTypes: tcpMaskTypes,
      hintEn: 'Applied after TLS/REALITY; first item is the innermost layer',
      hintFa: 'بعد از TLS/REALITY اعمال می شود؛ اولین مورد داخلی ترین لایه است'),
], visible: _notKcp);

const _sockoptGroup = FieldGroup('sockopt', 'Socket options (sockopt)', 'گزینه های سوکت (sockopt)', [
  FieldSpec('${TKeys.sockopt}.domainStrategy', 'Domain strategy', 'استراتژی دامنه', kind: FKind.choice,
      choices: ['', 'AsIs', 'UseIP', 'UseIPv4', 'UseIPv6', 'UseIPv4v6', 'UseIPv6v4', 'ForceIP', 'ForceIPv4', 'ForceIPv6', 'ForceIPv4v6', 'ForceIPv6v4']),
  FieldSpec('${TKeys.sockopt}.tcpFastOpen', 'TCP Fast Open', 'TCP Fast Open', kind: FKind.toggle),
  FieldSpec('${TKeys.sockopt}.tcpNoDelay', 'TCP no-delay', 'TCP no-delay', kind: FKind.toggle),
  FieldSpec('${TKeys.sockopt}.tcpMptcp', 'Multipath TCP', 'TCP چندمسیره (MPTCP)', kind: FKind.toggle),
  FieldSpec('${TKeys.sockopt}.tcpCongestion', 'TCP congestion', 'ازدحام TCP', kind: FKind.choice, choices: ['', 'bbr', 'cubic', 'reno']),
  FieldSpec('${TKeys.sockopt}.tcpKeepAliveIdle', 'Keepalive idle (s)', 'بیکاری keepalive (ثانیه)', kind: FKind.integer, min: -1, max: 86400),
  FieldSpec('${TKeys.sockopt}.tcpKeepAliveInterval', 'Keepalive interval (s)', 'فاصله keepalive (ثانیه)', kind: FKind.integer, min: -1, max: 86400),
  FieldSpec('${TKeys.sockopt}.tcpUserTimeout', 'TCP user timeout (ms)', 'مهلت کاربر TCP (میلی ثانیه)', kind: FKind.integer, min: 0, max: 600000),
  FieldSpec('${TKeys.sockopt}.tcpMaxSeg', 'TCP max segment', 'حداکثر سگمنت TCP', kind: FKind.integer, min: 0, max: 65535),
  FieldSpec('${TKeys.sockopt}.tcpWindowClamp', 'TCP window clamp', 'محدودیت پنجره TCP', kind: FKind.integer, min: 0, max: 1 << 30),
  FieldSpec('${TKeys.sockopt}.mark', 'SO_MARK', 'SO_MARK', kind: FKind.integer, min: 0, max: 0x7fffffff),
  FieldSpec('${TKeys.sockopt}.interface', 'Bind interface', 'اینترفیس', hintEn: 'e.g. wlan0', hintFa: 'مثلاً wlan0'),
  FieldSpec('${TKeys.sockopt}.dialerProxy', 'Dialer proxy (outbound tag)', 'پروکسی واسط (تگ outbound)'),
]);

String? _range(String v) => RegExp(r'^\d+(-\d+)?$').hasMatch(v) ? null : 'range2';

String _net(Vals v) => v['network'] ?? '';
bool _raw(Vals v) => const {'', 'tcp', 'raw'}.contains(_net(v));
bool _rawHttp(Vals v) => _raw(v) && v['headerType'] == 'http';
bool _kcp(Vals v) => const {'kcp', 'mkcp'}.contains(_net(v));
bool _notKcp(Vals v) => !_kcp(v);
bool _quic(Vals v) => _net(v) == 'quic';
bool _xdrive(Vals v) => _net(v) == 'xdrive';
bool _pathNet(Vals v) => const {'ws', 'httpupgrade', 'xhttp', 'splithttp', 'h2'}.contains(_net(v));
bool _grpc(Vals v) => _net(v) == 'grpc';
bool _xhttp(Vals v) => const {'xhttp', 'splithttp'}.contains(_net(v));

/// Clears per-transport values that are invalid for the newly chosen one.
void onNetworkChanged(Vals v) {
  final h = v['headerType'] ?? '';
  final ok = _raw(v) ? rawHeaderTypes.contains(h) : (_kcp(v) || _quic(v)) ? kcpHeaderTypes.contains(h) : true;
  if (!ok) v['headerType'] = '';
  final m = v['mode'] ?? '';
  if (_grpc(v) && !grpcModes.contains(m)) v['mode'] = '';
  if (_xhttp(v) && !xhttpModes.contains(m)) v['mode'] = '';
}

const _muxGroup = FieldGroup('mux', 'Mux', 'Mux (چندگانه سازی)', [
  FieldSpec('${TKeys.mux}.enabled', 'Enable mux', 'فعال سازی Mux', kind: FKind.toggle,
      hintEn: 'Always on for XDRIVE', hintFa: 'برای XDRIVE همیشه فعال است'),
  FieldSpec('${TKeys.mux}.concurrency', 'Concurrency', 'همزمانی', kind: FKind.integer, min: -1, max: 1024),
  FieldSpec('${TKeys.mux}.xudpConcurrency', 'XUDP concurrency', 'همزمانی XUDP', kind: FKind.integer, min: -1, max: 1024),
  FieldSpec('${TKeys.mux}.xudpProxyUDP443', 'XUDP UDP/443', 'XUDP برای UDP/443', kind: FKind.choice, choices: ['', 'reject', 'allow', 'skip']),
]);

List<FieldGroup> _xrayStream(String type) =>
    [_transport(type), _kcpGroup, _xdriveGroup, _masksGroup, _sockoptGroup, _muxGroup];

bool _tlsOn(Vals v) => _is(v, 'security', {'tls', 'reality'});
bool _reality(Vals v) => _is(v, 'security', {'reality'});
bool _tlsOnly(Vals v) => _is(v, 'security', {'tls'});

FieldGroup _tls({required bool selectable, bool reality = false}) => FieldGroup('tls', 'TLS', 'TLS', [
      if (selectable)
        FieldSpec('security', 'Security', 'امنیت',
            kind: FKind.choice, choices: reality ? const ['none', 'tls', 'reality'] : const ['none', 'tls']),
      FieldSpec('sni', 'SNI (server name)', 'SNI (نام سرور)', check: checkHost, visible: selectable ? _tlsOn : null),
      FieldSpec('alpn', 'ALPN', 'ALPN',
          kind: FKind.list, hintEn: 'Comma separated, e.g. h2,http/1.1', hintFa: 'با کاما جدا کنید، مثلاً h2,http/1.1',
          visible: selectable ? _tlsOnly : null),
      FieldSpec('fp', 'uTLS fingerprint', 'اثر انگشت uTLS',
          kind: FKind.choice, choices: _fps, visible: selectable ? _tlsOn : null),
      FieldSpec('insecure', 'Allow insecure certificate', 'پذیرش گواهی ناامن',
          kind: FKind.toggle, visible: selectable ? _tlsOnly : null),
      FieldSpec('pinSHA256', 'Certificate pin (SHA-256)', 'پین گواهی (SHA-256)',
          check: (v) => checkHex(v.replaceAll(':', '')), visible: selectable ? _tlsOnly : null),
      FieldSpec('pinSPKI', 'Public-key pin (SPKI)', 'پین کلید عمومی (SPKI)', visible: selectable ? _tlsOnly : null),
      FieldSpec('ech', 'ECH config list', 'پیکربندی ECH', visible: selectable ? _tlsOnly : null,
          hintEn: 'base64 ECHConfigList, or a DNS name to fetch it', hintFa: 'ECHConfigList به base64 یا نام DNS'),
      FieldSpec('vcn', 'Verify certificate by name', 'تأیید گواهی با نام', visible: selectable ? _tlsOnly : null),
      FieldSpec('cipherSuites', 'Cipher suites', 'مجموعه رمزها', visible: selectable ? _tlsOnly : null,
          hintEn: 'Colon separated', hintFa: 'با دونقطه جدا کنید'),
      if (reality) ...[
        const FieldSpec('pbk', 'Reality public key', 'کلید عمومی Reality',
            check: checkRealityKey, visible: _reality),
        const FieldSpec('sid', 'Reality short ID', 'شناسه کوتاه Reality', check: checkShortId, visible: _reality),
        const FieldSpec('spx', 'Reality spiderX', 'spiderX در Reality', visible: _reality),
        const FieldSpec('pqv', 'ML-DSA-65 verify key', 'کلید تأیید ML-DSA-65', visible: _reality),
      ],
    ]);

const _awg = FieldGroup('awg', 'AmneziaWG obfuscation', 'مبهم سازی AmneziaWG', [
  FieldSpec('wg.awg.jc', 'Jc (junk packets)', 'Jc (بسته های اضافی)', kind: FKind.integer, min: 0, max: 128),
  FieldSpec('wg.awg.jmin', 'Jmin', 'Jmin', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.jmax', 'Jmax', 'Jmax', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.s1', 'S1', 'S1', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.s2', 'S2', 'S2', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.s3', 'S3', 'S3', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.s4', 'S4', 'S4', kind: FKind.integer, min: 0, max: 1280),
  FieldSpec('wg.awg.h1', 'H1', 'H1'),
  FieldSpec('wg.awg.h2', 'H2', 'H2'),
  FieldSpec('wg.awg.h3', 'H3', 'H3'),
  FieldSpec('wg.awg.h4', 'H4', 'H4'),
  FieldSpec('wg.awg.i1', 'I1', 'I1', kind: FKind.multiline),
  FieldSpec('wg.awg.i2', 'I2', 'I2', kind: FKind.multiline),
  FieldSpec('wg.awg.i3', 'I3', 'I3', kind: FKind.multiline),
  FieldSpec('wg.awg.i4', 'I4', 'I4', kind: FKind.multiline),
  FieldSpec('wg.awg.i5', 'I5', 'I5', kind: FKind.multiline),
]);

const _wg = FieldGroup('wg', 'WireGuard', 'وایرگارد', [
  FieldSpec('wg.privateKey', 'Private key', 'کلید خصوصی',
      kind: FKind.secret, required: true, keepEmpty: true, check: checkWgKey),
  FieldSpec('wg.peerPublicKey', 'Peer public key', 'کلید عمومی سرور',
      required: true, keepEmpty: true, check: checkWgKey),
  FieldSpec('wg.presharedKey', 'Preshared key', 'کلید مشترک (PSK)', kind: FKind.secret, check: checkWgKey),
  FieldSpec('wg.address', 'Interface addresses', 'آدرس های اینترفیس',
      kind: FKind.list, required: true, keepEmpty: true, check: _cidrs,
      hintEn: 'CIDR, one per line', hintFa: 'CIDR، هر خط یکی'),
  FieldSpec('wg.allowedIPs', 'Allowed IPs', 'IPهای مجاز',
      kind: FKind.list, check: _cidrs, hintEn: 'Empty = all traffic', hintFa: 'خالی = همه ترافیک'),
  FieldSpec('wg.dns', 'DNS servers', 'سرورهای DNS', kind: FKind.list),
  FieldSpec('wg.mtu', 'MTU', 'MTU', kind: FKind.integer, min: 576, max: 65535),
  FieldSpec('wg.keepalive', 'Persistent keepalive (s)', 'Keepalive دائمی (ثانیه)', kind: FKind.integer, min: 0, max: 65535),
  FieldSpec('wg.reserved', 'Reserved bytes', 'بایت های رزرو',
      kind: FKind.intList, min: 0, max: 255, hintEn: 'e.g. 1,2,3', hintFa: 'مثلاً 1,2,3'),
]);

String? _cidrs(String v) => eachItem(checkCidr)(v);
FieldGroup _auth(String id, List<FieldSpec> f) => FieldGroup(id, 'Authentication', 'احراز هویت', f);

const _pass = FieldSpec('password', 'Password', 'رمز عبور', kind: FKind.secret);
const _passReq = FieldSpec('password', 'Password', 'رمز عبور', kind: FKind.secret, required: true);
const _user = FieldSpec('username', 'Username', 'نام کاربری');

/// Editor groups for a profile type, in display order.
List<FieldGroup> groupsFor(String type) {
  final g = <FieldGroup>[_general(type)];
  switch (type) {
    case 'vless':
      g.add(_auth('auth', [
        const FieldSpec('uuid', 'UUID', 'UUID', required: true, check: checkXrayId),
        const FieldSpec('flow', 'Flow', 'Flow', kind: FKind.choice, choices: ['', 'xtls-rprx-vision', 'xtls-rprx-vision-udp443']),
        const FieldSpec('encryption', 'Encryption', 'رمزنگاری', hintEn: 'none (or VLESS encryption string)', hintFa: 'none (یا رشته رمزنگاری VLESS)'),
      ]));
      g..addAll(_xrayStream(type))..add(_tls(selectable: true, reality: true));
    case 'vmess':
      g.add(_auth('auth', [
        const FieldSpec('uuid', 'UUID', 'UUID', required: true, check: checkXrayId),
        const FieldSpec('vmessSecurity', 'Cipher', 'رمزنگاری',
            kind: FKind.choice, choices: ['', 'auto', 'aes-128-gcm', 'chacha20-poly1305', 'none', 'zero']),
        const FieldSpec('alterId', 'Alter ID', 'Alter ID', kind: FKind.integer, min: 0, max: 65535),
      ]));
      g..addAll(_xrayStream(type))..add(_tls(selectable: true));
    case 'trojan':
      g.add(_auth('auth', [_passReq]));
      g..addAll(_xrayStream(type))..add(_tls(selectable: true, reality: true));
    case 'shadowsocks':
      g.add(_auth('auth', [
        const FieldSpec('method', 'Method', 'روش رمزنگاری', kind: FKind.choice, choices: _ssMethods, required: true),
        _passReq,
        const FieldSpec('plugin', 'Plugin', 'افزونه', hintEn: 'e.g. obfs-local, v2ray-plugin', hintFa: 'مثلاً obfs-local, v2ray-plugin'),
        const FieldSpec('pluginOpts', 'Plugin options', 'گزینه های افزونه'),
      ]));
      g.addAll(_xrayStream(type));
    case 'hysteria2':
      g.add(_auth('auth', [
        _passReq,
        const FieldSpec('obfsPassword', 'Obfuscation password (salamander)', 'رمز مبهم سازی (salamander)', kind: FKind.secret),
        const FieldSpec('ports', 'Port hopping', 'پرش پورت (Port hopping)', check: checkPorts,
            hintEn: 'e.g. 443,20000-30000', hintFa: 'مثلاً 443,20000-30000'),
        const FieldSpec('upMbps', 'Upload (Mbps)', 'آپلود (Mbps)', kind: FKind.integer, min: 0, max: 100000),
        const FieldSpec('downMbps', 'Download (Mbps)', 'دانلود (Mbps)', kind: FKind.integer, min: 0, max: 100000),
      ]));
      g.add(_tls(selectable: false));
    case 'tuic':
      g.add(_auth('auth', [
        const FieldSpec('uuid', 'UUID', 'UUID', required: true, check: checkUuid),
        _passReq,
        const FieldSpec('congestion', 'Congestion control', 'کنترل ازدحام', kind: FKind.choice, choices: ['', 'cubic', 'new_reno', 'bbr']),
        const FieldSpec('udpRelayMode', 'UDP relay mode', 'حالت انتقال UDP', kind: FKind.choice, choices: ['', 'native', 'quic']),
      ]));
      g.add(_tls(selectable: false));
    case 'wireguard':
      g.add(_wg);
    case 'amneziawg':
      g..add(_wg)..add(_awg);
    case 'http':
    case 'socks':
      // SOCKS5: plain runs on sing-box, SOCKS over TLS on Xray.
      g.add(_auth('auth', [_user, _pass]));
      g.add(_tls(selectable: true));
    case 'mtproto':
      g.add(_auth('auth', [const FieldSpec('secret', 'Secret', 'Secret', required: true)]));
    case 'masque':
      g.add(_auth('auth', [_user, _pass, const FieldSpec('masquePath', 'MASQUE path', 'مسیر MASQUE', check: checkPath)]));
      g.add(_tls(selectable: false));
  }
  return g;
}

// ------------------------------------------------------------ json <-> values

Object? getPath(Map<String, dynamic> j, String path) {
  Object? cur = j;
  for (final k in path.split('.')) {
    if (cur is! Map) return null;
    cur = cur[k];
  }
  return cur;
}

void setPath(Map<String, dynamic> j, String path, Object? value) {
  final keys = path.split('.');
  var cur = j;
  for (final k in keys.take(keys.length - 1)) {
    final next = cur[k];
    if (next is Map<String, dynamic>) {
      cur = next;
    } else if (next is Map) {
      cur = cur[k] = Map<String, dynamic>.from(next);
    } else {
      if (value == null) return; // nothing to remove
      cur = cur[k] = <String, dynamic>{};
    }
  }
  if (value == null) {
    cur.remove(keys.last);
  } else {
    cur[keys.last] = value;
  }
}

String fieldToString(FieldSpec f, Object? v) {
  if (v == null) return f.kind == FKind.toggle ? 'false' : '';
  switch (f.kind) {
    case FKind.maskList:
      if (v is! List || v.isEmpty) return '';
      return encodeMaskVals([
        for (final m in v)
          if (m is Map)
            {
              'type': '${m['type'] ?? ''}',
              'settings': m['settings'] is Map && (m['settings'] as Map).isNotEmpty
                  ? const JsonEncoder.withIndent('  ').convert(m['settings'])
                  : '',
            }
      ]);
    case FKind.list:
    case FKind.intList:
      return v is List ? v.map((e) => '$e').join('\n') : '$v';
    case FKind.toggle:
      return v == true ? 'true' : 'false';
    case FKind.decimal:
      return v is num && v == v.roundToDouble() ? v.toInt().toString() : '$v';
    default:
      return '$v';
  }
}

Object? fieldFromString(FieldSpec f, String raw) {
  final v = raw.trim();
  switch (f.kind) {
    case FKind.maskList:
      final items = decodeMaskVals(v) ?? const [];
      final out = <Map<String, dynamic>>[];
      for (final m in items) {
        final t = (m['type'] ?? '').trim();
        if (t.isEmpty) continue;
        final st = (m['settings'] ?? '').trim();
        Object? settings;
        try {
          settings = st.isEmpty ? null : jsonDecode(st);
        } catch (_) {}
        out.add({'type': t, if (settings is Map && settings.isNotEmpty) 'settings': settings});
      }
      return out.isEmpty ? null : out;
    case FKind.toggle:
      return v == 'true' ? true : null;
    case FKind.integer:
      return v.isEmpty ? null : int.tryParse(v);
    case FKind.decimal:
      return v.isEmpty ? null : double.tryParse(v);
    case FKind.list:
      final l = splitList(v);
      return l.isEmpty ? (f.keepEmpty ? <String>[] : null) : l;
    case FKind.intList:
      final l = splitList(v).map(int.tryParse).whereType<int>().toList();
      return l.isEmpty ? null : l;
    case FKind.multiline:
    case FKind.secret:
      // Keep internal whitespace (PEM keys, payloads), trim the ends only.
      return raw.trim().isEmpty ? (f.keepEmpty ? '' : null) : raw.trim();
    default:
      return v.isEmpty ? (f.keepEmpty ? '' : null) : v;
  }
}

Map<String, dynamic> deepCopy(Map<String, dynamic> j) => jsonDecode(jsonEncode(j)) as Map<String, dynamic>;

/// Copy of [j] with the raw-JSON string keys decoded to objects. Keys whose
/// string is not a JSON object are listed in the returned set and left as is.
(Map<String, dynamic>, Set<String>) expandJsonStrings(Map<String, dynamic> j) {
  final c = deepCopy(j);
  final opaque = <String>{};
  for (final k in jsonStringKeys) {
    final v = c[k];
    if (v is String && v.trim().isNotEmpty) {
      try {
        final d = jsonDecode(v);
        if (d is Map) {
          c[k] = Map<String, dynamic>.from(d);
          continue;
        }
      } catch (_) {}
      opaque.add(k);
    }
  }
  return (c, opaque);
}

/// Inverse of [expandJsonStrings]: objects back to (compact) JSON strings.
Map<String, dynamic> collapseJsonStrings(Map<String, dynamic> j) {
  for (final k in jsonStringKeys) {
    final v = j[k];
    if (v is Map) {
      if (v.isEmpty) {
        j.remove(k);
      } else {
        j[k] = jsonEncode(v);
      }
    }
  }
  return j;
}

bool _underOpaque(String path, Set<String> opaque) => opaque.contains(path.split('.').first);

/// All field values of [type] read from [json].
Vals valuesOf(Map<String, dynamic> json0, String type) {
  final (json, _) = expandJsonStrings(json0);
  final v = <String, String>{};
  for (final g in groupsFor(type)) {
    for (final f in g.fields) {
      v[f.path] = fieldToString(f, getPath(json, f.path));
    }
  }
  v['type'] = type;
  return v;
}

/// Validates all visible fields. Returns path -> error key tuple.
Map<String, (String, num?, num?)> validateAll(Vals vals) {
  final errs = <String, (String, num?, num?)>{};
  final type = vals['type'] ?? '';
  for (final g in groupsFor(type)) {
    if (!g.shown(vals)) continue;
    for (final f in g.fields) {
      if (!f.shown(vals)) continue;
      final e = validateField(f, vals[f.path] ?? '');
      if (e != null) errs[f.path] = e;
    }
  }
  return errs;
}

/// Applies [vals] onto a copy of [original]; keys outside the schema are kept.
Map<String, dynamic> applyValues(Map<String, dynamic> original, Vals vals) {
  final (j, opaque) = expandJsonStrings(original);
  final type = vals['type'] ?? (original['type'] as String? ?? '');
  for (final g in groupsFor(type)) {
    for (final f in g.fields) {
      if (!vals.containsKey(f.path)) continue;
      // A raw-JSON key that is not valid JSON is only editable as raw JSON.
      if (_underOpaque(f.path, opaque)) continue;
      setPath(j, f.path, fieldFromString(f, vals[f.path]!));
    }
  }
  j['type'] = type;
  if (original['id'] != null) j['id'] = original['id'];
  return collapseJsonStrings(j);
}

/// Fields that define the connection (a change invalidates the share link).
bool connectionChanged(Map<String, dynamic> a, Map<String, dynamic> b) {
  Map<String, dynamic> strip(Map<String, dynamic> m) {
    final (c, _) = expandJsonStrings(m);
    for (final k in const ['name', 'link', 'userRenamed', 'userEdited', 'subId', 'addressOverride', 'core']) {
      c.remove(k);
    }
    return c;
  }

  return jsonEncode(_sorted(strip(a))) != jsonEncode(_sorted(strip(b)));
}

Object? _sorted(Object? v) {
  if (v is Map) {
    final keys = v.keys.map((e) => '$e').toList()..sort();
    return {for (final k in keys) k: _sorted(v[k])};
  }
  if (v is List) return v.map(_sorted).toList();
  return v;
}


/// Editor representation of a mask list: JSON array of {type, settings}
/// where settings is the raw (possibly invalid) JSON text being edited.
String encodeMaskVals(List<Map<String, String>> items) => items.isEmpty ? '' : jsonEncode(items);

List<Map<String, String>>? decodeMaskVals(String v) {
  if (v.trim().isEmpty) return [];
  try {
    final l = jsonDecode(v);
    if (l is! List) return null;
    return [
      for (final m in l)
        if (m is Map) {'type': '${m['type'] ?? ''}', 'settings': '${m['settings'] ?? ''}'} else <String, String>{}
    ];
  } catch (_) {
    return null;
  }
}
