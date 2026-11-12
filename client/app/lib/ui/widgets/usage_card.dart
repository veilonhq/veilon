import 'dart:math';

import 'package:flutter/material.dart';

import '../../core/format.dart';
import '../../core/models.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import 'glass.dart';

/// Plan usage from the subscription-userinfo header: used of total, a split
/// lime (download) / violet (upload) capsule, and a days-left dial.
class UsageCard extends StatelessWidget {
  const UsageCard({super.key, required this.info, required this.title});
  final UserInfo info;
  final String title;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final (usedV, usedU) = splitBytes(info.used);
    final totalText = info.total > 0 ? fmtBytes(info.total) : s.t('home.unlimited');
    final days = info.daysLeft(DateTime.now());
    final total = info.total > 0 ? info.total.toDouble() : max(1.0, info.used * 1.0);
    final downF = (info.download / total).clamp(0.0, 1.0);
    final upF = (info.upload / total).clamp(0.0, 1.0 - downF);
    String daysText;
    double dial;
    if (days == null) {
      daysText = s.t('home.noExpiry');
      dial = 1;
    } else if (days < 0) {
      daysText = s.t('home.expired');
      dial = 0;
    } else if (days == 0) {
      daysText = s.t('home.expiresToday');
      dial = 0.02;
    } else {
      daysText = s.f('home.daysLeft', {'n': days});
      dial = (days / 30).clamp(0.0, 1.0);
    }
    return Glass(
      padding: const EdgeInsets.all(Gap.l + 2),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Icon(Icons.data_usage_rounded, size: 18, color: Palette.text2),
          const SizedBox(width: Gap.s),
          Expanded(
            child: Text(title.isEmpty ? s.t('home.usage') : title,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Palette.text2, fontWeight: FontWeight.w700, fontSize: 13)),
          ),
          _DaysDial(value: dial, label: daysText, warn: days != null && days <= 3),
        ]),
        const SizedBox(height: Gap.m),
        Row(crossAxisAlignment: CrossAxisAlignment.baseline, textBaseline: TextBaseline.alphabetic, children: [
          Row(
              mainAxisSize: MainAxisSize.min,
              textDirection: TextDirection.ltr,
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: [
                Text(s.metric(usedV), style: Fonts.metric(size: 34, weight: FontWeight.w700, height: 1.15).copyWith(letterSpacing: -1)),
                const SizedBox(width: 6),
                Text(usedU, style: Fonts.metric(size: 14, weight: FontWeight.w600, color: Palette.text2, height: 1.15)),
              ]),
          const SizedBox(width: Gap.s),
          Flexible(
            child: Text.rich(
              TextSpan(children: [
                TextSpan(text: '${s.t('home.of')} '),
                TextSpan(text: s.metric(totalText), style: Fonts.metric(size: 13, weight: FontWeight.w600, color: Palette.muted)),
              ]),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: Palette.muted, fontSize: 13),
            ),
          ),
        ]),
        const SizedBox(height: Gap.m),
        _SplitBar(down: downF, up: upF),
        const SizedBox(height: Gap.m),
        Row(children: [
          _Legend(color: Palette.lime, label: s.t('home.download'), value: s.metric(fmtBytes(info.download))),
          const SizedBox(width: Gap.xl),
          _Legend(color: Palette.violet, label: s.t('home.upload'), value: s.metric(fmtBytes(info.upload))),
        ]),
      ]),
    );
  }
}

class _SplitBar extends StatelessWidget {
  const _SplitBar({required this.down, required this.up});
  final double down, up;

  @override
  Widget build(BuildContext context) {
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0, end: 1),
      duration: const Duration(milliseconds: 1100),
      curve: Curves.easeOutQuart,
      builder: (context, t, _) => LayoutBuilder(builder: (context, c) {
        final w = c.maxWidth;
        return Container(
          height: 12,
          decoration: BoxDecoration(color: Palette.glassHi, borderRadius: BorderRadius.circular(99)),
          child: Stack(children: [
            PositionedDirectional(
              start: 0,
              top: 0,
              bottom: 0,
              width: w * down * t,
              child: Container(
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(99),
                  gradient: LinearGradient(colors: [Color(0xFF9CD92A), Palette.lime]),
                  boxShadow: [BoxShadow(color: Palette.lime.withValues(alpha: 0.45), blurRadius: 10)],
                ),
              ),
            ),
            PositionedDirectional(
              start: w * down * t + (down > 0 && up > 0 ? 3 : 0),
              top: 0,
              bottom: 0,
              width: max(0, w * up * t - (down > 0 && up > 0 ? 3 : 0)),
              child: Container(
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(99),
                  gradient: LinearGradient(colors: [Palette.violet, Palette.violetFg]),
                  boxShadow: [BoxShadow(color: Palette.violet.withValues(alpha: 0.45), blurRadius: 10)],
                ),
              ),
            ),
          ]),
        );
      }),
    );
  }
}

class _Legend extends StatelessWidget {
  const _Legend({required this.color, required this.label, required this.value});
  final Color color;
  final String label, value;
  @override
  Widget build(BuildContext context) {
    return Row(mainAxisSize: MainAxisSize.min, children: [
      Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
      const SizedBox(width: 6),
      Text(label, style: TextStyle(color: Palette.muted, fontSize: 12)),
      const SizedBox(width: 6),
      Text(value, textDirection: TextDirection.ltr, style: Fonts.metric(size: 12, weight: FontWeight.w700, color: Palette.text)),
    ]);
  }
}

class _DaysDial extends StatelessWidget {
  const _DaysDial({required this.value, required this.label, required this.warn});
  final double value;
  final String label;
  final bool warn;
  @override
  Widget build(BuildContext context) {
    final color = warn ? Palette.warn : Palette.lime;
    return Row(mainAxisSize: MainAxisSize.min, children: [
      SizedBox(
        width: 18,
        height: 18,
        child: TweenAnimationBuilder<double>(
          tween: Tween(begin: 0, end: value),
          duration: const Duration(milliseconds: 1000),
          curve: Curves.easeOutCubic,
          builder: (_, v, _) => CircularProgressIndicator(
              value: v, strokeWidth: 2.6, color: color, backgroundColor: Palette.glassHi, strokeCap: StrokeCap.round),
        ),
      ),
      const SizedBox(width: 6),
      Text(label, style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w700)),
    ]);
  }
}
