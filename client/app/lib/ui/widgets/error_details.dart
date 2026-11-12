import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/errors.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import 'glass.dart';

/// Short localized message for a raw core/service error.
String friendlyError(S s, String raw) => s.t(friendlyErrorKey(raw));

/// Bottom sheet with the friendly message and the raw technical text
/// (selectable, copyable). [onRetry] adds a retry button.
Future<void> showErrorDetails(BuildContext context, String raw, {String? title, VoidCallback? onRetry}) {
  final s = S.of(context);
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(Gap.l, 0, Gap.l, Gap.l),
        child: Container(
          decoration: BoxDecoration(
            color: Palette.bg2,
            borderRadius: BorderRadius.circular(Radii.l),
            border: Border.all(color: Palette.line),
          ),
          padding: const EdgeInsets.fromLTRB(Gap.l, Gap.l, Gap.l, Gap.m),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Row(children: [
              const Icon(Icons.error_outline_rounded, color: Palette.danger, size: 22),
              const SizedBox(width: Gap.s),
              Expanded(
                child: Text(title ?? s.t('err.title'),
                    style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16)),
              ),
            ]),
            const SizedBox(height: Gap.m),
            Text(friendlyError(s, raw), style: const TextStyle(color: Palette.text, fontSize: 14, height: 1.6)),
            const SizedBox(height: Gap.l),
            Text(s.t('err.raw'), style: const TextStyle(color: Palette.muted, fontSize: 12)),
            const SizedBox(height: Gap.xs),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 220),
              child: Container(
                padding: const EdgeInsets.all(Gap.m),
                decoration: BoxDecoration(color: Palette.glass, borderRadius: BorderRadius.circular(Radii.s)),
                child: SingleChildScrollView(
                  child: SelectableText(raw,
                      textDirection: TextDirection.ltr,
                      style: const TextStyle(fontFamily: 'monospace', fontSize: 12, color: Palette.text2, height: 1.5)),
                ),
              ),
            ),
            const SizedBox(height: Gap.m),
            Row(children: [
              TextButton.icon(
                icon: const Icon(Icons.copy_rounded, size: 18),
                label: Text(s.t('common.copy')),
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: raw));
                  showToast(ctx, s.t('common.copied'));
                },
              ),
              const Spacer(),
              if (onRetry != null)
                TextButton(
                  onPressed: () {
                    Navigator.of(ctx).pop();
                    onRetry();
                  },
                  child: Text(s.t('common.retry')),
                ),
              TextButton(onPressed: () => Navigator.of(ctx).pop(), child: Text(s.t('common.close'))),
            ]),
          ]),
        ),
      ),
    ),
  );
}
