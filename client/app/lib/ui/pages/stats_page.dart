import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../core/foreground_timer.dart';
import '../../core/format.dart';
import '../../core/models.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import 'home_page.dart' show CoreTag;
import '../widgets/speed_wave.dart';
import '../widgets/usage_card.dart';

class StatsPage extends StatefulWidget {
  const StatsPage({super.key});
  @override
  State<StatsPage> createState() => _StatsPageState();
}

class _StatsPageState extends State<StatsPage> {
  ForegroundTimer? _tick;

  @override
  void initState() {
    super.initState();
    _tick = ForegroundTimer(const Duration(seconds: 1), () {
      // Only while this page is in front (TickerMode) and connected.
      if (mounted && TickerMode.valuesOf(context).enabled && AppScope.read(context).state == ConnState.connected) setState(() {});
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final s = S.of(context);
    final on = app.state == ConnState.connected;
    final p = app.profiles.where((x) => x.id == app.connectedProfileId).firstOrNull ?? app.selected;
    final core = p == null ? null : app.coreOf(p);
    final coreText = core == null ? '' : CoreTag.label(core.$1);
    final dur = on && app.connectedSince != null ? DateTime.now().difference(app.connectedSince!) : Duration.zero;
    return ListView(
      physics: pagePhysics,
      // Bottom: the gesture bar plus a small margin (1.0.2 added 64 px, which
      // made the page a few px too tall to fit and so it scrolled/jiggled).
      padding: EdgeInsets.fromLTRB(Gap.page, Gap.l, Gap.page, Gap.l + MediaQuery.viewPaddingOf(context).bottom),
      children: [
        Glass(
          glow: on ? Palette.lime : null,
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(s.t('stats.duration'), style: const TextStyle(color: Palette.muted, fontSize: 12)),
            const SizedBox(height: Gap.xs),
            Text(s.metric(fmtDuration(dur)),
                textDirection: TextDirection.ltr,
                style: Fonts.metric(size: 44, weight: FontWeight.w300, color: on ? Palette.text : Palette.muted, height: 1.2)
                    .copyWith(letterSpacing: 2)),
            if (!on) Text(s.t('stats.idle'), style: const TextStyle(color: Palette.muted, fontSize: 13)),
            const SizedBox(height: Gap.l),
            RepaintBoundary(child: SpeedWave(samples: app.history, height: 150, active: on)),
          ]),
        ),
        const SizedBox(height: Gap.m),
        GridView.count(
          crossAxisCount: 2,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          mainAxisSpacing: Gap.m,
          crossAxisSpacing: Gap.m,
          childAspectRatio: 1.7,
          children: [
            _Metric(label: s.t('stats.sessionDown'), value: fmtBytes(app.sessionDown), color: Palette.lime, icon: Icons.south_rounded),
            _Metric(label: s.t('stats.sessionUp'), value: fmtBytes(app.sessionUp), color: Palette.violet, icon: Icons.north_rounded),
            _Metric(label: s.t('stats.peakDown'), value: fmtSpeed(app.peakDown), color: Palette.lime, icon: Icons.trending_up_rounded),
            _Metric(label: s.t('stats.peakUp'), value: fmtSpeed(app.peakUp), color: Palette.violet, icon: Icons.trending_up_rounded),
          ],
        ),
        if (p != null) ...[
          const SizedBox(height: Gap.m),
          Glass(
            child: Column(children: [
              _Row(k: s.t('home.server'), v: p.name),
              const Divider(height: Gap.xl, color: Palette.line),
              _Row(k: s.t('stats.protocol'), v: p.label, ltr: true),
              const Divider(height: Gap.xl, color: Palette.line),
              _Row(k: s.t('stats.core'), v: coreText, ltr: true),
            ]),
          ),
        ],
        if (app.subs.any((x) => x.userInfo.present)) ...[
          SectionTitle(s.t('stats.subscriptions')),
          for (final sub in app.subs.where((x) => x.userInfo.present)) ...[
            UsageCard(info: sub.userInfo, title: sub.displayName),
            const SizedBox(height: Gap.m),
          ],
        ],
      ],
    );
  }
}

class _Metric extends StatelessWidget {
  const _Metric({required this.label, required this.value, required this.color, required this.icon});
  final String label, value;
  final Color color;
  final IconData icon;
  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final i = value.lastIndexOf(' ');
    return Glass(
      padding: const EdgeInsets.all(Gap.m + 2),
      radius: Radii.m,
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
        Row(children: [
          Container(
            padding: const EdgeInsets.all(5),
            decoration: BoxDecoration(color: color.withValues(alpha: 0.15), shape: BoxShape.circle),
            // Trend arrows keep their meaning in RTL (no mirroring).
            child: Directionality(textDirection: TextDirection.ltr, child: Icon(icon, size: 14, color: color)),
          ),
          const SizedBox(width: Gap.s),
          Expanded(child: Text(label, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(color: Palette.muted, fontSize: 12))),
        ]),
        Row(
            mainAxisSize: MainAxisSize.min,
            textDirection: TextDirection.ltr,
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Text(s.metric(value.substring(0, i)), style: Fonts.metric(size: 22, weight: FontWeight.w700, height: 1.15)),
              const SizedBox(width: Gap.xs),
              Text(value.substring(i + 1), style: Fonts.metric(size: 12, weight: FontWeight.w600, color: Palette.text2, height: 1.15)),
            ]),
      ]),
    );
  }
}

class _Row extends StatelessWidget {
  const _Row({required this.k, required this.v, this.ltr = false});
  final String k, v;
  final bool ltr;
  @override
  Widget build(BuildContext context) {
    return Row(children: [
      Text(k, style: const TextStyle(color: Palette.muted, fontSize: 13)),
      const SizedBox(width: Gap.l),
      Expanded(
        child: Align(
          alignment: AlignmentDirectional.centerEnd,
          child: Text(v,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            textDirection: ltr ? TextDirection.ltr : null,
            style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
        ),
      ),
    ]);
  }
}
