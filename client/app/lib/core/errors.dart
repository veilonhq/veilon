/// Maps a raw core / service error to a localisation key for a short,
/// human-friendly message. The raw text stays available as "details".
String friendlyErrorKey(String raw) {
  final e = raw.trim();
  if (e.isEmpty) return 'err.friendly.generic';
  if (e == 'denied') return 'err.vpnDenied';
  if (e.startsWith('err.') && !e.contains(' ')) return e;
  // A core error key wrapped in context ("start: err.x").
  final key = RegExp(r'err\.[A-Za-z]+').firstMatch(e);
  if (key != null) return key.group(0)!;
  final l = e.toLowerCase();
  bool has(List<String> xs) => xs.any(l.contains);
  if (has(['read-only file system', 'permission denied', 'cache-file', 'cache.db', 'no space left'])) {
    return 'err.friendly.storage';
  }
  if (has(['not supported in this build', 'is not supported', 'opened in telegram'])) return 'err.friendly.unsupported';
  if (has(['handshake', 'no handshake'])) {
    if (has(['tls', 'x509', 'certificate', 'reality'])) return 'err.friendly.tls';
    return 'err.friendly.handshake';
  }
  if (has(['x509', 'certificate', 'tls:', 'remote error'])) return 'err.friendly.tls';
  if (has(['no such host', 'resolve', 'lookup '])) return 'err.friendly.dns';
  if (has(['network is unreachable', 'no route to host', 'no network', 'host is down'])) return 'err.friendly.network';
  if (has(['connection refused', 'connection reset', 'refused'])) return 'err.friendly.refused';
  if (has(['timeout', 'timed out', 'deadline exceeded', 'i/o timeout', 'no response'])) return 'err.friendly.timeout';
  if (has(['config', 'invalid', 'missing', 'parse', 'unsupported', 'unknown'])) return 'err.friendly.config';
  return 'err.friendly.generic';
}
