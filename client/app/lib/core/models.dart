import 'dart:convert';

/// Top-level groups on the Servers page; profiles of different groups never
/// mix in one list.
enum ProfileCategory {
  /// V2Ray family and everything else carried by Xray / sing-box (VLESS,
  /// VMess, Trojan, Shadowsocks, Hysteria2, TUIC, HTTP, SOCKS, MASQUE, ...).
  xray,

  /// AmneziaWG / WireGuard.
  amnezia;

  static ProfileCategory? parse(String? v) => ProfileCategory.values.where((c) => c.name == v).firstOrNull;
}

/// A server profile. The JSON schema is owned by the Go core
/// (core/veiloncore/profile.go); the app keeps it as a map and reads the
/// fields it displays.
class Profile {
  Profile(this.json);
  factory Profile.fromJson(Map<String, dynamic> j) => Profile(Map<String, dynamic>.from(j));

  final Map<String, dynamic> json;

  String get id => json['id'] as String? ?? '';
  String get name => (json['name'] as String?)?.trim().isNotEmpty == true ? json['name'] as String : server;
  set name(String v) => json['name'] = v;
  String get type => json['type'] as String? ?? '';
  String get server => json['server'] as String? ?? '';
  int get port => (json['port'] as num?)?.toInt() ?? 0;
  String get network => json['network'] as String? ?? '';
  String get security => json['security'] as String? ?? '';
  String get link => json['link'] as String? ?? '';
  String? get subId => json['subId'] as String?;
  set subId(String? v) => v == null ? json.remove('subId') : json['subId'] = v;

  bool get isMTProto => type == 'mtproto';
  bool get isWireGuard => type == 'wireguard' || type == 'amneziawg';

  /// V2Ray-family profiles honour the core choice (Settings.Core / "core").
  bool get isV2 => const {'vless', 'vmess', 'trojan', 'shadowsocks'}.contains(type);

  /// Types removed in 1.0.4 (SlipNet family: SSH / StunTLS, DNSTT,
  /// NoizDNS, VayDNS, Slipstream, Soar; the NaiveProxy stub).
  /// Stored profiles of these types are dropped on load. Generic SOCKS5 is
  /// a standard Xray / sing-box protocol and is not removed.
  static const removedTypes = {'ssh', 'dnstt', 'noizdns', 'vaydns', 'slipstream', 'soar', 'naive'};
  bool get isRemoved => removedTypes.contains(type) || link.toLowerCase().startsWith('slipnet://');

  ProfileCategory get category => isWireGuard ? ProfileCategory.amnezia : ProfileCategory.xray;

  /// Only Xray / sing-box profiles get a latency (URL) test. AmneziaWG /
  /// WireGuard profiles are never pinged; MTProto opens in Telegram.
  bool get pingable => category == ProfileCategory.xray && !isMTProto;

  /// Row subtitle, always "protocol · transport · security", e.g.
  /// "VLESS · TCP · Reality", "VLESS · WS · TLS", "VMess · TCP · None".
  /// A missing transport (or type=raw) is TCP; no security is "None".
  /// QUIC-based protocols (Hysteria2, TUIC, MASQUE) are "QUIC · TLS";
  /// WireGuard / AmneziaWG are "UDP · Noise" (WireGuard's handshake).
  String get label {
    final String transport;
    final String sec;
    switch (type) {
      case 'hysteria2' || 'tuic' || 'masque':
        transport = 'QUIC';
        sec = 'TLS';
      case 'wireguard' || 'amneziawg':
        transport = 'UDP';
        sec = 'Noise';
      default:
        transport = transportName(network.isEmpty ? 'tcp' : network);
        sec = switch (security) {
          'reality' => 'Reality',
          'tls' => 'TLS',
          _ => 'None',
        };
    }
    return '${protocolName(type)} · $transport · $sec';
  }

  String get tgLink {
    final secret = json['secret'] ?? '';
    return 'tg://proxy?server=${Uri.encodeComponent(server)}&port=$port&secret=${Uri.encodeComponent('$secret')}';
  }

  static String protocolName(String t) => switch (t) {
    'vless' => 'VLESS',
    'vmess' => 'VMess',
    'trojan' => 'Trojan',
    'shadowsocks' => 'Shadowsocks',
    'hysteria2' => 'Hysteria2',
    'tuic' => 'TUIC',
    'wireguard' => 'WireGuard',
    'amneziawg' => 'AmneziaWG',
    'http' => 'HTTP',
    'socks' => 'SOCKS',
    'mtproto' => 'MTProto',
    'masque' => 'MASQUE',
    _ => t.toUpperCase(),
  };

  static String transportName(String n) => switch (n) {
    'tcp' || 'raw' => 'TCP',
    'kcp' || 'mkcp' => 'mKCP',
    'splithttp' => 'XHTTP',
    'ws' => 'WS',
    'grpc' => 'gRPC',
    'httpupgrade' => 'HTTPUpgrade',
    'xhttp' => 'XHTTP',
    'h2' => 'H2',
    'quic' => 'QUIC',
    _ => n.toUpperCase(),
  };

  String encode() => jsonEncode(json);
}

class UserInfo {
  const UserInfo({this.upload = 0, this.download = 0, this.total = 0, this.expire = 0, this.present = false});
  factory UserInfo.fromJson(Map<String, dynamic>? j) => j == null
      ? const UserInfo()
      : UserInfo(
          upload: (j['upload'] as num?)?.toInt() ?? 0,
          download: (j['download'] as num?)?.toInt() ?? 0,
          total: (j['total'] as num?)?.toInt() ?? 0,
          expire: (j['expire'] as num?)?.toInt() ?? 0,
          present: j['present'] as bool? ?? false,
        );

  final int upload;
  final int download;
  final int total; // 0 = unlimited
  final int expire; // unix seconds, 0 = never
  final bool present;

  int get used => upload + download;
  double get usedFraction => total <= 0 ? 0 : (used / total).clamp(0.0, 1.0);

  /// Whole days left until expiry (null = no expiry). Negative = expired.
  int? daysLeft(DateTime now) {
    if (expire <= 0) return null;
    final exp = DateTime.fromMillisecondsSinceEpoch(expire * 1000);
    final diff = exp.difference(now);
    if (diff.isNegative) return -1;
    return (diff.inHours / 24).ceil();
  }

  Map<String, dynamic> toJson() => {'upload': upload, 'download': download, 'total': total, 'expire': expire, 'present': present};
}

class Subscription {
  Subscription({
    required this.id,
    required this.url,
    this.name = '',
    this.title = '',
    this.userInfo = const UserInfo(),
    this.updatedAt = 0,
    this.updateHours = 0,
    this.autoUpdate = true,
    this.lastError = '',
    this.refused = '',
    this.supportUrl = '',
  });

  factory Subscription.fromJson(Map<String, dynamic> j) => Subscription(
    id: j['id'] as String,
    url: j['url'] as String,
    name: j['name'] as String? ?? '',
    title: j['title'] as String? ?? '',
    userInfo: UserInfo.fromJson(j['userInfo'] as Map<String, dynamic>?),
    updatedAt: (j['updatedAt'] as num?)?.toInt() ?? 0,
    updateHours: (j['updateHours'] as num?)?.toInt() ?? 0,
    autoUpdate: j['autoUpdate'] as bool? ?? true,
    lastError: j['lastError'] as String? ?? '',
    refused: j['refused'] as String? ?? '',
    supportUrl: j['supportUrl'] as String? ?? '',
  );

  final String id;
  String url;
  String name;
  String title;
  UserInfo userInfo;
  int updatedAt; // unix ms
  int updateHours;
  bool autoUpdate;
  String lastError;
  String refused;
  String supportUrl;

  String get displayName {
    if (name.isNotEmpty) return name;
    if (title.isNotEmpty) return title;
    return Uri.tryParse(url)?.host ?? url;
  }

  /// Update interval: the panel's profile-update-interval, else [fallbackHours].
  Duration interval(int fallbackHours) => Duration(hours: updateHours > 0 ? updateHours : fallbackHours);

  bool isDue(DateTime now, int fallbackHours) =>
      autoUpdate && now.millisecondsSinceEpoch - updatedAt >= interval(fallbackHours).inMilliseconds;

  Map<String, dynamic> toJson() => {
    'id': id,
    'url': url,
    'name': name,
    'title': title,
    'userInfo': userInfo.toJson(),
    'updatedAt': updatedAt,
    'updateHours': updateHours,
    'autoUpdate': autoUpdate,
    'lastError': lastError,
    'refused': refused,
    'supportUrl': supportUrl,
  };
}

enum ConnState { disconnected, connecting, connected, disconnecting, error }

ConnState connStateFrom(String? s) => switch (s) {
  'connecting' => ConnState.connecting,
  'connected' => ConnState.connected,
  'disconnecting' => ConnState.disconnecting,
  'error' => ConnState.error,
  _ => ConnState.disconnected,
};

class AppSettings {
  AppSettings();

  String language = 'fa';
  bool killSwitch = false;
  bool autoConnect = false;
  bool autoReconnect = true;
  bool autoUpdateSubs = true;
  int autoUpdateHours = 12;
  bool autoSelectFastest = false;
  int mtu = 9000;
  String remoteDns = 'https://1.1.1.1/dns-query';
  String directDns = '223.5.5.5';
  bool disableQuic = false;
  bool bypassIran = true;
  bool bypassLan = true;
  bool blockAds = false;
  bool ipv6 = false;
  String perAppMode = 'off';
  List<String> perAppPackages = [];
  List<String> proxyRules = [];
  List<String> directRules = [];
  List<String> blockRules = [];
  // The TUN stack is always gVisor (1.0.4): stored 'stack' /
  // 'experimentalStack' values are ignored and never sent to the core.

  /// Engine for V2Ray-family profiles: auto, sing-box or xray (Settings.Core).
  String core = 'auto';
  String logLevel = 'warn';
  String pingUrl = 'http://www.google.com/generate_204';

  /// Latency test opens a new proxy connection per sample (handshakes
  /// included). Off: median of 3 round trips over one open connection.
  bool fullHandshakePing = false;

  factory AppSettings.fromJson(Map<String, dynamic> j) {
    final s = AppSettings();
    s.language = j['language'] as String? ?? s.language;
    s.killSwitch = j['killSwitch'] as bool? ?? s.killSwitch;
    s.autoConnect = j['autoConnect'] as bool? ?? s.autoConnect;
    s.autoReconnect = j['autoReconnect'] as bool? ?? s.autoReconnect;
    s.autoUpdateSubs = j['autoUpdateSubs'] as bool? ?? s.autoUpdateSubs;
    s.autoUpdateHours = (j['autoUpdateHours'] as num?)?.toInt() ?? s.autoUpdateHours;
    s.autoSelectFastest = j['autoSelectFastest'] as bool? ?? s.autoSelectFastest;
    s.mtu = (j['mtu'] as num?)?.toInt() ?? s.mtu;
    s.remoteDns = j['remoteDns'] as String? ?? s.remoteDns;
    s.directDns = j['directDns'] as String? ?? s.directDns;
    s.disableQuic = j['disableQuic'] as bool? ?? s.disableQuic;
    s.bypassIran = j['bypassIran'] as bool? ?? s.bypassIran;
    s.bypassLan = j['bypassLan'] as bool? ?? s.bypassLan;
    s.blockAds = j['blockAds'] as bool? ?? s.blockAds;
    // 'batterySaver' (until 1.0.2) is ignored: the option was removed and a
    // stored true must not keep upgraded users in the old saver mode.
    s.ipv6 = j['ipv6'] as bool? ?? s.ipv6;
    s.perAppMode = j['perAppMode'] as String? ?? s.perAppMode;
    s.perAppPackages = _strList(j['perAppPackages']);
    s.proxyRules = _strList(j['proxyRules']);
    s.directRules = _strList(j['directRules']);
    s.blockRules = _strList(j['blockRules']);
    // 'stack' / 'experimentalStack' (≤1.0.3) and 'dnsResolvers' (DNS-tunnel
    // resolvers from the removed scanner) are ignored.
    s.core = j['core'] as String? ?? s.core;
    s.logLevel = j['logLevel'] as String? ?? s.logLevel;
    s.pingUrl = j['pingUrl'] as String? ?? s.pingUrl;
    s.fullHandshakePing = j['fullHandshakePing'] as bool? ?? s.fullHandshakePing;
    return s;
  }

  static List<String> _strList(Object? v) => v is List ? v.map((e) => '$e').toList() : <String>[];

  Map<String, dynamic> toJson() => {
    'language': language,
    'killSwitch': killSwitch,
    'autoConnect': autoConnect,
    'autoReconnect': autoReconnect,
    'autoUpdateSubs': autoUpdateSubs,
    'autoUpdateHours': autoUpdateHours,
    'autoSelectFastest': autoSelectFastest,
    'mtu': mtu,
    'remoteDns': remoteDns,
    'directDns': directDns,
    'disableQuic': disableQuic,
    'bypassIran': bypassIran,
    'bypassLan': bypassLan,
    'blockAds': blockAds,
    'ipv6': ipv6,
    'perAppMode': perAppMode,
    'perAppPackages': perAppPackages,
    'proxyRules': proxyRules,
    'directRules': directRules,
    'blockRules': blockRules,
    'core': core,
    'logLevel': logLevel,
    'pingUrl': pingUrl,
    'fullHandshakePing': fullHandshakePing,
  };

  /// The settings JSON consumed by the Go core (Settings in settings.go).
  Map<String, dynamic> toCoreJson({required String assetDir, required String appPackage}) => {
    'mtu': mtu,
    'remoteDns': remoteDns,
    'directDns': directDns,
    'disableQuic': disableQuic,
    'bypassIran': bypassIran,
    'bypassLan': bypassLan,
    'blockAds': blockAds,
    'ipv6': ipv6,
    'proxyRules': proxyRules,
    'directRules': directRules,
    'blockRules': blockRules,
    'perAppMode': perAppMode,
    'perAppPackages': perAppPackages,
    'appPackage': appPackage,
    'logLevel': logLevel,
    'assetDir': assetDir,
    'core': core,
    'fullHandshakePing': fullHandshakePing,
  };

  /// Settings that require a reconnect to take effect.
  String coreFingerprint() => jsonEncode([
    mtu,
    remoteDns,
    directDns,
    disableQuic,
    bypassIran,
    bypassLan,
    blockAds,
    ipv6,
    perAppMode,
    perAppPackages,
    proxyRules,
    directRules,
    blockRules,
    logLevel,
    core,
  ]);
}
