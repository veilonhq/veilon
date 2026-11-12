import 'package:url_launcher/url_launcher.dart';

/// Telegram username behind Settings › "Contact us" (opens `https://t.me/<name>`).
/// The single place to change it; empty hides the row.
const String kContactTelegramUsername = 'lxhosein';

/// Opens a URL; replaced in tests.
typedef ExternalUrlOpener = Future<bool> Function(Uri uri, LaunchMode mode);

class ContactLink {
  ContactLink._();

  /// Defaults to [kContactTelegramUsername]; tests may override it.
  static String username = kContactTelegramUsername;

  static ExternalUrlOpener opener = (uri, mode) => launchUrl(uri, mode: mode);

  /// `https://t.me/<username>`, or null when no username is set (row hidden).
  static Uri? get uri {
    final u = username.trim().replaceFirst(RegExp(r'^@'), '');
    if (u.isEmpty) return null;
    return Uri.https('t.me', '/$u');
  }

  /// Opens the link outside the app: Telegram if it handles t.me, else the browser.
  static Future<bool> open() async {
    final u = uri;
    if (u == null) return false;
    try {
      return await opener(u, LaunchMode.externalApplication);
    } catch (_) {
      return false;
    }
  }
}
