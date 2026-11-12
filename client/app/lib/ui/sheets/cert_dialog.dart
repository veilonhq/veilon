import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../theme/palette.dart';

/// "AB:CD:…" groups of the SHA-256 hex, two lines of 16 bytes.
String formatFingerprint(String hex) {
  final h = hex.toUpperCase();
  final pairs = [for (var i = 0; i + 1 < h.length; i += 2) h.substring(i, i + 2)];
  if (pairs.length <= 16) return pairs.join(':');
  return '${pairs.take(16).join(':')}\n${pairs.skip(16).join(':')}';
}

/// 1.0.8 (audit H1): asks whether to trust a literal-IP subscription
/// server's unverifiable certificate. Nothing was sent to the server yet
/// (the core only read its certificate). Returns true to pin it.
Future<bool> showCertDialog(BuildContext context, String server, String fingerprint, bool changed) async {
  final s = S.of(context);
  final ok = await showDialog<bool>(
    context: context,
    barrierColor: Palette.barrier,
    builder: (ctx) => AlertDialog(
      key: const ValueKey('cert-dialog'),
      icon: Icon(changed ? Icons.gpp_maybe_rounded : Icons.verified_user_outlined, color: changed ? Palette.danger : Palette.warn),
      title: Text(s.t('cert.title'), textAlign: TextAlign.center),
      content: SingleChildScrollView(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          if (changed) ...[
            Text(s.t('cert.changed'), style: TextStyle(color: Palette.danger, fontWeight: FontWeight.w700, height: 1.5)),
            const SizedBox(height: Gap.m),
          ],
          Text(s.t('cert.body').replaceAll('{server}', '\u2066$server\u2069'), style: TextStyle(color: Palette.text2, height: 1.6)),
          const SizedBox(height: Gap.m),
          Text(s.t('cert.fingerprint'), style: TextStyle(color: Palette.muted, fontSize: 12, fontWeight: FontWeight.w700)),
          const SizedBox(height: Gap.xs),
          Container(
            padding: const EdgeInsets.all(Gap.s),
            decoration: BoxDecoration(
              color: Palette.glass,
              borderRadius: BorderRadius.circular(Radii.s),
              border: Border.all(color: Palette.line),
            ),
            child: SelectableText(
              formatFingerprint(fingerprint),
              key: const ValueKey('cert-fingerprint'),
              textDirection: TextDirection.ltr,
              style: Fonts.metric(size: 11, weight: FontWeight.w600, color: Palette.text, height: 1.5),
            ),
          ),
        ]),
      ),
      actions: [
        TextButton(
          key: const ValueKey('cert-cancel'),
          onPressed: () => Navigator.pop(ctx, false),
          child: Text(s.t('common.cancel')),
        ),
        FilledButton(
          key: const ValueKey('cert-trust'),
          style: FilledButton.styleFrom(
            backgroundColor: changed ? Palette.danger : Palette.lime,
            foregroundColor: changed ? Colors.white : (Palette.isLight ? Colors.white : Palette.limeInk),
          ),
          onPressed: () => Navigator.pop(ctx, true),
          child: Text(s.t('cert.trust')),
        ),
      ],
    ),
  );
  return ok == true;
}
