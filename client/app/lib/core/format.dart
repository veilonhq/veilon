import 'dart:math';

/// Byte formatting helpers (Latin digits; the UI layer may localise digits).
String fmtBytes(num bytes, {int digits = 1}) {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  final i = min(units.length - 1, (log(bytes) / log(1024)).floor());
  final v = bytes / pow(1024, i);
  final d = i == 0 ? 0 : (v >= 100 ? 0 : digits);
  return '${v.toStringAsFixed(d)} ${units[i]}';
}

/// Splits a formatted size into value and unit, e.g. ("12.4", "GB").
(String, String) splitBytes(num bytes, {int digits = 1}) {
  final s = fmtBytes(bytes, digits: digits);
  final i = s.lastIndexOf(' ');
  return (s.substring(0, i), s.substring(i + 1));
}

String fmtSpeed(num bytesPerSec) => '${fmtBytes(bytesPerSec)}/s';

(String, String) splitSpeed(num bytesPerSec) {
  final (v, u) = splitBytes(bytesPerSec);
  return (v, '$u/s');
}

String fmtDuration(Duration d) {
  final h = d.inHours;
  final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
  final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
  return h > 0 ? '${h.toString().padLeft(2, '0')}:$m:$s' : '$m:$s';
}
