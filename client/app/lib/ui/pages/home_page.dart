import 'dart:math';

import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../core/foreground_timer.dart';
import '../../core/format.dart';
import '../../core/models.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../shell.dart';
import '../sheets/add_sheet.dart';
import '../widgets/error_details.dart';
import '../widgets/glass.dart';
import '../widgets/portal.dart';
import '../widgets/proto_badge.dart';
import '../widgets/speed_wave.dart';
import '../widgets/usage_card.dart';

class HomePage extends StatelessWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final s = S.of(context);
    final p = app.selected;
    final sub = app.subOf(p) ?? (app.subs.isNotEmpty ? app.subs.first : null);
    final mq = MediaQuery.of(context);
    final w = mq.size.width;
    // The hero shrinks on shorter screens so the page fits without
    // scrolling on common phones (1.0.2 was 24-71 px too tall at 360x780 /
    // 393x851 dp, so the page scrolled and jiggled). Fixed per screen size,
    // never per state, so nothing below it moves when connecting.
    final hero = min(w * 0.72, (mq.size.height - mq.padding.vertical) * 0.31).clamp(190.0, 320.0);
    return ListView(
      padding: EdgeInsets.fromLTRB(Gap.page, Gap.s, Gap.page, Gap.l + mq.viewPadding.bottom),
      physics: pagePhysics,
      children: [
        const SizedBox(height: Gap.l),
        // 1.0.7: the animated portal is its own layer (its frames and the
        // press scale never repaint the page; scrolling only moves it).
        Center(
          child: RepaintBoundary(
            child: Portal(
            state: app.state,
            size: hero,
            onTap: () async {
              if (p == null) {
                showAddSheet(context);
                return;
              }
              final err = await app.toggle();
              if (err != null && context.mounted) {
                showToast(context, friendlyError(s, err), error: true);
              }
            },
          ),
          ),
        ),
        const SizedBox(height: Gap.m),
        _StatusLine(app: app),
        const SizedBox(height: Gap.xl),
        if (p != null) _ServerCapsule(profile: p, ping: app.pings[p.id], testing: app.testing.contains(p.id))
        else _EmptyCapsule(onTap: () => showAddSheet(context)),
        const SizedBox(height: Gap.l),
        // Live numbers: rebuilt per stats tick on their own (1.0.7).
        RepaintBoundary(child: StatsBuilder(builder: (context) => _LiveCard(app: app))),
        if (sub != null && sub.userInfo.present) ...[
          const SizedBox(height: Gap.l),
          UsageCard(info: sub.userInfo, title: sub.displayName),
        ],
      ],
    );
  }
}

class _StatusLine extends StatefulWidget {
  const _StatusLine({required this.app});
  final AppState app;
  @override
  State<_StatusLine> createState() => _StatusLineState();
}

class _StatusLineState extends State<_StatusLine> {
  ForegroundTimer? _timer;

  @override
  void initState() {
    super.initState();
    _timer = ForegroundTimer(const Duration(seconds: 1), () {
      if (mounted && TickerMode.valuesOf(context).enabled && widget.app.state == ConnState.connected) setState(() {});
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final app = widget.app;
    final st = app.state;
    final title = s.t('state.${st.name}');
    final color = switch (st) {
      ConnState.connected => Palette.lime,
      ConnState.error => Palette.danger,
      ConnState.connecting || ConnState.disconnecting => Palette.violetFg,
      _ => Palette.text2,
    };
    String sub;
    final failed = st == ConnState.error && app.lastError.isNotEmpty;
    if (st == ConnState.connected && app.connectedSince != null) {
      sub = s.metric(fmtDuration(DateTime.now().difference(app.connectedSince!)));
    } else if (st == ConnState.error && app.blocking) {
      sub = s.t('hero.blocking');
    } else if (failed) {
      sub = friendlyError(s, app.lastError);
    } else if (app.selected == null) {
      sub = s.t('hero.noServer');
    } else {
      sub = s.t(app.isOn ? 'hero.tapDisconnect' : 'hero.tapConnect');
    }
    final subStyle = st == ConnState.connected
        ? Fonts.metric(size: 16, weight: FontWeight.w600, color: Palette.muted).copyWith(letterSpacing: 1.5)
        : TextStyle(color: failed ? Palette.text2 : Palette.muted, fontSize: 13, height: 1.5);
    return Column(children: [
      AnimatedSwitcher(
        duration: const Duration(milliseconds: 300),
        switchInCurve: Curves.easeOutCubic,
        switchOutCurve: Curves.easeInCubic,
        layoutBuilder: (cur, prev) => Stack(alignment: Alignment.center, children: [...prev, ?cur]),
        transitionBuilder: (c, a) => FadeTransition(
            opacity: a, child: SlideTransition(position: Tween(begin: const Offset(0, 0.25), end: Offset.zero).animate(a), child: c)),
        child: Text(title,
            key: ValueKey(st),
            // 1.0.8: no blurred text shadow (a glyph blur pass on every
            // frame of the home page while connected).
            style: TextStyle(fontSize: 24, height: 1.3, fontWeight: FontWeight.w900, color: color, letterSpacing: -0.3)),
      ),
      const SizedBox(height: Gap.xs),
      // Fixed height: switching between one- and two-line texts never
      // shifts the cards below.
      SizedBox(
        height: 42,
        child: AnimatedSwitcher(
          duration: const Duration(milliseconds: 250),
          switchInCurve: Curves.easeOutCubic,
          switchOutCurve: Curves.easeInCubic,
          layoutBuilder: (cur, prev) => Stack(alignment: Alignment.topCenter, children: [...prev, ?cur]),
          child: failed
              ? GestureDetector(
                  key: ValueKey('err$sub'),
                  behavior: HitTestBehavior.opaque,
                  onTap: () => showErrorDetails(context, app.lastError, title: s.t('state.error')),
                  child: Column(mainAxisSize: MainAxisSize.min, children: [
                    Text(sub, maxLines: 1, overflow: TextOverflow.ellipsis, textAlign: TextAlign.center, style: subStyle),
                    Row(mainAxisSize: MainAxisSize.min, children: [
                      Icon(Icons.info_outline_rounded, size: 13, color: Palette.danger),
                      const SizedBox(width: Gap.xs),
                      Text(s.t('err.details'),
                          style: TextStyle(color: Palette.danger, fontSize: 12, fontWeight: FontWeight.w700, height: 1.4)),
                    ]),
                  ]),
                )
              : Text(sub,
                  key: ValueKey(st == ConnState.connected ? st : sub),
                  maxLines: 2,
                  textAlign: TextAlign.center,
                  overflow: TextOverflow.ellipsis,
                  style: subStyle),
        ),
      ),
    ]);
  }
}

class _ServerCapsule extends StatelessWidget {
  const _ServerCapsule({required this.profile, required this.ping, required this.testing});
  final Profile profile;
  final int? ping;
  final bool testing;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.read(context);
    final core = AppScope.of(context).coreOf(profile);
    final rtl = Directionality.of(context) == TextDirection.rtl;
    // Narrow phones (≤ 340 dp): a smaller badge and no chevron, so the name
    // has room (1.0.8).
    final narrow = MediaQuery.sizeOf(context).width <= 340;
    return Pressable(
      onTap: () => Shell.goTo(context, 1),
      child: Glass(
        radius: Radii.xl,
        padding: const EdgeInsetsDirectional.fromSTEB(Gap.m, Gap.m, Gap.m, Gap.m),
        child: Row(crossAxisAlignment: CrossAxisAlignment.center, children: [
          ProtoBadge(type: profile.type, size: narrow ? 38 : 46, active: app.state == ConnState.connected),
          const SizedBox(width: Gap.m),
          Expanded(
            child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
              // 1.0.8: up to two lines (long Persian names at 320 dp).
              Text(profile.name,
                  key: const ValueKey('home-server-name'),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  softWrap: true,
                  style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15.5, height: 1.3)),
              const SizedBox(height: 2),
              // Narrow: the ping chip sits on the label line, so the name
              // gets the full width for its two lines.
              if (narrow)
                Row(children: [
                  Expanded(child: _label(profile, core)),
                  const SizedBox(width: Gap.s),
                  PingChip(profile: profile, ping: ping, testing: testing),
                ])
              else
                _label(profile, core),
            ]),
          ),
          if (!narrow) ...[
            const SizedBox(width: Gap.s),
            PingChip(profile: profile, ping: ping, testing: testing),
            const SizedBox(width: Gap.xs),
            Icon(rtl ? Icons.chevron_left_rounded : Icons.chevron_right_rounded, color: Palette.muted, size: 22),
          ],
        ]),
      ),
    );
  }

  Widget _label(Profile profile, (String, String)? core) => LayoutBuilder(builder: (context, c) {
                final label = Text(profile.label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textDirection: TextDirection.ltr,
                    style: TextStyle(color: Palette.muted, fontSize: 12, height: 1.3));
                final hasCore = core != null && core.$1.isNotEmpty;
                if (!hasCore) return Row(children: [Flexible(child: label)]);
                final tag = CoreTag(core: core.$1, error: core.$2.isNotEmpty);
                // Very narrow: the engine tag alone (it shrinks with an ellipsis).
                if (c.maxWidth < 120) return Row(children: [Flexible(child: tag)]);
                return Row(children: [tag, const SizedBox(width: Gap.s), Flexible(child: label)]);
              });
}

/// Small label naming the engine ("sing-box", "Xray", "AmneziaWG"…).
class CoreTag extends StatelessWidget {
  const CoreTag({super.key, required this.core, this.error = false});
  final String core;
  final bool error;

  static String label(String core) => switch (core) {
        'sing-box' => 'sing-box',
        'xray' => 'Xray',
        'amneziawg' => 'AmneziaWG',
        'dns-tunnel' => 'DNS',
        'ssh' => 'SSH',
        _ => core,
      };

  @override
  Widget build(BuildContext context) {
    final c = error ? Palette.danger : Palette.violetFg;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(
        color: c.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: c.withValues(alpha: 0.3)),
      ),
      child: Text(label(core),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          softWrap: false,
          textDirection: TextDirection.ltr,
          style: TextStyle(color: c, fontSize: 10.5, fontWeight: FontWeight.w700, height: 1.3, fontFamily: Fonts.display,
              fontFamilyFallback: Fonts.fallback)),
    );
  }
}

/// Delay chip: tap to test; when the last test failed, tap (or hold any
/// time) to see why.
class PingChip extends StatelessWidget {
  const PingChip({super.key, required this.profile, required this.ping, required this.testing});
  final Profile profile;
  final int? ping;
  final bool testing;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final app = AppScope.read(context);
    if (profile.isMTProto) {
      return Pill(text: 'TG', color: Palette.info);
    }
    if (!profile.pingable) {
      // AmneziaWG / WireGuard profiles are never pinged: a
      // neutral dash, no tap-to-ping, no error state.
      return Container(
        key: const ValueKey('ping-none'),
        constraints: const BoxConstraints(minWidth: 56, minHeight: 30),
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: Palette.glass,
          borderRadius: BorderRadius.circular(99),
          border: Border.all(color: Palette.line),
        ),
        child: Center(
            widthFactor: 1,
            child: Text('—', style: Fonts.metric(size: 13, weight: FontWeight.w700, color: Palette.muted, height: 1.2))),
      );
    }
    final color = Palette.pingColor(ping);
    final err = app.pingErrors[profile.id];
    Widget child;
    if (testing) {
      child = const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2));
    } else if (ping == null) {
      child = Icon(Icons.speed_rounded, size: 16, color: Palette.muted);
    } else if (ping! < 0) {
      child = Row(mainAxisSize: MainAxisSize.min, children: [
        Text(s.t('servers.timeout'), style: TextStyle(color: color, fontSize: 11.5, fontWeight: FontWeight.w700, height: 1.2)),
        if (err != null) ...[const SizedBox(width: 3), Icon(Icons.info_outline_rounded, size: 12, color: color)],
      ]);
    } else {
      child = Row(
          mainAxisSize: MainAxisSize.min,
          textDirection: TextDirection.ltr,
          crossAxisAlignment: CrossAxisAlignment.baseline,
          textBaseline: TextBaseline.alphabetic,
          children: [
        Text(s.metric('$ping'), style: Fonts.metric(size: 13, weight: FontWeight.w800, color: color, height: 1.2)),
        const SizedBox(width: 2),
        Text('ms', style: Fonts.metric(size: 10, weight: FontWeight.w600, color: color.withValues(alpha: 0.8), height: 1.2)),
      ]);
    }
    void details() => showErrorDetails(context, err ?? '', title: s.t('ping.failed'), onRetry: () => app.ping(profile));
    // 1.0.7: a Semantics hint instead of a manual-trigger Tooltip (never
    // shown; it built an overlay portal and gesture plumbing into every
    // server row).
    return Semantics(
      tooltip: s.t('ping.hint'),
      child: Pressable(
        scale: 0.9,
        onTap: testing ? null : (ping != null && ping! < 0 && err != null ? details : () => app.ping(profile)),
        onLongPress: err != null && !testing ? details : null,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 250),
          curve: Curves.easeOutCubic,
          constraints: const BoxConstraints(minWidth: 56, minHeight: 30),
          padding: const EdgeInsets.symmetric(horizontal: 10),
          decoration: BoxDecoration(
            color: color.withValues(alpha: 0.12),
            borderRadius: BorderRadius.circular(99),
            border: Border.all(color: color.withValues(alpha: 0.3)),
          ),
          child: Center(widthFactor: 1, child: child),
        ),
      ),
    );
  }
}

class _EmptyCapsule extends StatelessWidget {
  const _EmptyCapsule({required this.onTap});
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    return Pressable(
      onTap: onTap,
      child: Glass(
        radius: Radii.xl,
        borderColor: Palette.lime.withValues(alpha: 0.35),
        child: Row(children: [
          Container(
            width: 46,
            height: 46,
            decoration: BoxDecoration(color: Palette.lime.withValues(alpha: 0.14), borderRadius: BorderRadius.circular(15)),
            child: Icon(Icons.add_rounded, color: Palette.lime),
          ),
          const SizedBox(width: Gap.m),
          Expanded(child: Text(s.t('home.addFirst'), style: const TextStyle(fontWeight: FontWeight.w700))),
        ]),
      ),
    );
  }
}

class _LiveCard extends StatelessWidget {
  const _LiveCard({required this.app});
  final AppState app;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final active = app.state == ConnState.connected;
    return Glass(
      padding: const EdgeInsets.fromLTRB(Gap.l, Gap.l, Gap.l, Gap.l),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        if (active && app.serverUnreachable) ...[
          Row(key: const ValueKey('server-unreachable'), children: [
            Icon(Icons.error_outline_rounded, color: Palette.danger, size: 18),
            const SizedBox(width: Gap.s),
            Expanded(
                child: Text(s.t('home.serverUnreachable'),
                    style: TextStyle(color: Palette.danger, fontSize: 12.5, fontWeight: FontWeight.w700, height: 1.3))),
          ]),
          const SizedBox(height: Gap.m),
        ],
        Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Expanded(child: _Speed(label: s.t('home.download'), value: app.downSpeed, color: Palette.lime, icon: Icons.south_rounded)),
          const SizedBox(width: Gap.m),
          Expanded(
              child: _Speed(label: s.t('home.upload'), value: app.upSpeed, color: Palette.violet, icon: Icons.north_rounded, end: true)),
        ]),
        const SizedBox(height: Gap.m),
        RepaintBoundary(child: SpeedWave(samples: app.history, height: 110, active: active)),
        const SizedBox(height: Gap.m),
        Divider(height: 1, color: Palette.line),
        const SizedBox(height: Gap.m),
        Row(crossAxisAlignment: CrossAxisAlignment.center, children: [
          Text(s.t('home.session'), style: TextStyle(color: Palette.muted, fontSize: 12, height: 1.3)),
          const Spacer(),
          _SessionValue(icon: Icons.south_rounded, color: Palette.lime, text: s.metric(fmtBytes(app.sessionDown))),
          const SizedBox(width: Gap.l),
          _SessionValue(icon: Icons.north_rounded, color: Palette.violet, text: s.metric(fmtBytes(app.sessionUp))),
        ]),
      ]),
    );
  }
}

class _SessionValue extends StatelessWidget {
  const _SessionValue({required this.icon, required this.color, required this.text});
  final IconData icon;
  final Color color;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Row(mainAxisSize: MainAxisSize.min, textDirection: TextDirection.ltr, children: [
      Icon(icon, size: 13, color: color),
      const SizedBox(width: 3),
      Text(text, textDirection: TextDirection.ltr, style: Fonts.metric(size: 12.5, weight: FontWeight.w600, color: Palette.text2, height: 1.3)),
    ]);
  }
}

class _Speed extends StatelessWidget {
  const _Speed({required this.label, required this.value, required this.color, required this.icon, this.end = false});
  final String label;
  final double value;
  final Color color;
  final IconData icon;
  final bool end;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final (v, u) = splitSpeed(value);
    return Column(crossAxisAlignment: end ? CrossAxisAlignment.end : CrossAxisAlignment.start, children: [
      Row(mainAxisSize: MainAxisSize.min, children: [
        Icon(icon, size: 14, color: color),
        const SizedBox(width: Gap.xs),
        Text(label, style: TextStyle(color: Palette.muted, fontSize: 12, height: 1.3)),
      ]),
      const SizedBox(height: Gap.xs),
      // Number and unit always read left-to-right ("0 B/s", "12.4 MB/s").
      Row(
        mainAxisSize: MainAxisSize.min,
        textDirection: TextDirection.ltr,
        crossAxisAlignment: CrossAxisAlignment.baseline,
        textBaseline: TextBaseline.alphabetic,
        children: [
          // The current value as is: no count-up tween from 0.
          Text(s.metric(v),
              textDirection: TextDirection.ltr, style: Fonts.metric(size: 26, weight: FontWeight.w700, color: color, height: 1.15)),
          const SizedBox(width: Gap.xs),
          Text(u, textDirection: TextDirection.ltr, style: Fonts.metric(size: 12, weight: FontWeight.w600, color: Palette.text2, height: 1.15)),
        ],
      ),
    ]);
  }
}
