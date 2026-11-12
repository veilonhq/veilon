import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/app_state.dart';
import '../../core/format.dart';
import '../../core/models.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../sheets/add_sheet.dart';
import '../widgets/error_details.dart';
import '../widgets/glass.dart';
import '../widgets/proto_badge.dart';
import 'home_page.dart' show PingChip;
import 'profile_editor_page.dart';

class ServersPage extends StatefulWidget {
  const ServersPage({super.key});
  @override
  State<ServersPage> createState() => _ServersPageState();
}

/// One collapsible group on the Servers page: the configs added by hand or
/// imported singly ([sub] == null), or one subscription and its configs.
class ServerGroup {
  ServerGroup(this.id, this.sub, this.profiles);
  final String id; // AppState.manualGroup or the subscription id
  final Subscription? sub;
  final List<Profile> profiles;

  /// Manual configs first (only when there are any), then every
  /// subscription in the order it was added (even while still empty).
  static List<ServerGroup> of(AppState app) {
    final manual = app.profiles.where((p) => p.subId == null || app.subs.every((x) => x.id != p.subId)).toList();
    return [
      if (manual.isNotEmpty) ServerGroup(AppState.manualGroup, null, manual),
      for (final x in app.subs) ServerGroup(x.id, x, app.profiles.where((p) => p.subId == x.id).toList()),
    ];
  }
}

class _ServersPageState extends State<ServersPage> with AutomaticKeepAliveClientMixin {
  /// Groups sorted by delay (per group, not persisted).
  final Set<String> _sorted = {};

  /// Groups whose "test all" is running.
  final Set<String> _pinging = {};

  @override
  bool get wantKeepAlive => true;

  List<Profile> _ordered(AppState app, ServerGroup g) {
    final list = [...g.profiles];
    if (_sorted.contains(g.id)) {
      int key(Profile p) {
        final v = app.pings[p.id];
        if (v == null) return 1 << 29;
        if (v < 0) return 1 << 30;
        return v;
      }

      list.sort((a, b) => key(a).compareTo(key(b)));
    }
    return list;
  }

  Future<void> _pingGroup(AppState app, String id, List<Profile> list) async {
    setState(() => _pinging.add(id));
    await app.pingAll(only: list);
    if (!mounted) return;
    setState(() => _pinging.remove(id));
    HapticFeedback.lightImpact();
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    final app = AppScope.of(context);
    final s = S.of(context);
    final groups = ServerGroup.of(app);
    final pingable = app.profiles.where((p) => p.pingable).toList();
    return Stack(
      children: [
        CustomScrollView(
          physics: pagePhysics,
          slivers: [
            SliverPadding(
              padding: const EdgeInsets.fromLTRB(Gap.page, Gap.m, Gap.page, Gap.s),
              sliver: SliverToBoxAdapter(
                child: Row(
                  children: [
                    GlassIconButton(
                      key: const ValueKey('view-mode'),
                      icon: app.serversCompact ? Icons.view_agenda_outlined : Icons.format_list_bulleted_rounded,
                      color: app.serversCompact ? Palette.lime : Palette.text,
                      tooltip: s.t(app.serversCompact ? 'servers.viewCards' : 'servers.viewCompact'),
                      onTap: () {
                        HapticFeedback.selectionClick();
                        app.setServersCompact(!app.serversCompact);
                      },
                    ),
                    const SizedBox(width: Gap.s),
                    // Latency tests and "fastest" only exist for Xray / sing-box profiles.
                    // Pinned to the far end so it lines up with the cards' end edge.
                    if (pingable.isNotEmpty)
                      Expanded(
                        child: Align(
                          alignment: AlignmentDirectional.centerEnd,
                          child: FittedBox(
                          fit: BoxFit.scaleDown,
                          alignment: AlignmentDirectional.centerEnd,
                          child: Pressable(
                            key: const ValueKey('fastest'),
                            onTap: () {
                              final best = app.fastest(among: pingable);
                              if (best != null) {
                                app.select(best.id);
                                HapticFeedback.mediumImpact();
                              } else {
                                _pingGroup(app, '*', pingable).then((_) {
                                  final b = app.fastest(among: pingable);
                                  if (b != null) app.select(b.id);
                                });
                              }
                            },
                            child: Pill(
                              text: s.t('servers.fastest'),
                              color: Palette.lime,
                              filled: true,
                              icon: Icons.auto_awesome_rounded,
                            ),
                          ),
                        ),
                        ),
                      ),
                  ],
                ),
              ),
            ),
            if (groups.isEmpty)
              SliverFillRemaining(hasScrollBody: false, child: _Empty(onAdd: () => showAddSheet(context)))
            else
              for (final g in groups) ...[
                SliverPadding(
                  padding: const EdgeInsets.fromLTRB(Gap.page, Gap.s, Gap.page, Gap.s),
                  sliver: SliverToBoxAdapter(
                    child: GroupHeader(
                      key: ValueKey('group-${g.id}'),
                      group: g,
                      collapsed: app.isCollapsed(g.id),
                      sorted: _sorted.contains(g.id),
                      pinging: _pinging.contains(g.id),
                      onToggle: () {
                        HapticFeedback.selectionClick();
                        app.toggleGroup(g.id);
                      },
                      onSort: () => setState(() => _sorted.contains(g.id) ? _sorted.remove(g.id) : _sorted.add(g.id)),
                      onPing: () => _pingGroup(app, g.id, g.profiles.where((p) => p.pingable).toList()),
                    ),
                  ),
                ),
                if (!app.isCollapsed(g.id))
                  Builder(
                    builder: (context) {
                      final list = _ordered(app, g);
                      return SliverPadding(
                        padding: const EdgeInsetsDirectional.fromSTEB(Gap.page + Gap.s, 0, Gap.page, Gap.s),
                        sliver: SliverList.separated(
                          itemCount: list.length,
                          separatorBuilder: (_, _) => SizedBox(height: app.serversCompact ? Gap.xs : Gap.s),
                          itemBuilder: (context, i) =>
                              _ServerTile(key: ValueKey(list[i].id), profile: list[i], index: i, compact: app.serversCompact),
                        ),
                      );
                    },
                  ),
              ],
            const SliverToBoxAdapter(child: SizedBox(height: 120)),
          ],
        ),
        PositionedDirectional(
          end: Gap.page,
          bottom: Gap.xl + MediaQuery.paddingOf(context).bottom,
          // Hidden while the add sheet is open (nothing may show through it).
          child: ValueListenableBuilder<bool>(
            valueListenable: addSheetOpen,
            builder: (context, open, child) => Visibility(visible: !open, child: child!),
            child: Pressable(
              onTap: () => showAddSheet(context),
              scale: 0.9,
              child: Container(
                width: 60,
                height: 60,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: LinearGradient(colors: [const Color(0xFFDDFF80), Palette.limeFill]),
                  border: Palette.isLight ? Border.all(color: Palette.lime.withValues(alpha: 0.35)) : null,
                  boxShadow: [BoxShadow(color: Palette.limeFill.withValues(alpha: Palette.isLight ? 0.6 : 0.45), blurRadius: 26, spreadRadius: -4)],
                ),
                child: Icon(Icons.add_rounded, color: Palette.limeInk, size: 30),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

String _two(int n) => n.toString().padLeft(2, '0');

/// yyyy/MM/dd (local time).
String fmtDate(DateTime d) => '${d.year}/${_two(d.month)}/${_two(d.day)}';

/// yyyy/MM/dd HH:mm (local time).
String fmtDateTime(DateTime d) => '${fmtDate(d)} ${_two(d.hour)}:${_two(d.minute)}';

/// Header of a [ServerGroup]: name and count, collapse chevron, group
/// "test all" and sort-by-delay; for a subscription also the usage bar
/// (upload + download of total, from subscription-userinfo), the expiry
/// date, the last update time, refresh and the edit menu.
class GroupHeader extends StatelessWidget {
  const GroupHeader({
    super.key,
    required this.group,
    required this.collapsed,
    required this.sorted,
    required this.pinging,
    required this.onToggle,
    required this.onSort,
    required this.onPing,
  });
  final ServerGroup group;
  final bool collapsed;
  final bool sorted;
  final bool pinging;
  final VoidCallback onToggle;
  final VoidCallback onSort;
  final VoidCallback onPing;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final s = S.of(context);
    final sub = group.sub;
    final id = group.id;
    final ui = sub?.userInfo;
    final canPing = group.profiles.any((p) => p.pingable);
    final name = sub == null ? s.t('servers.myConfigs') : sub.displayName;
    final err = sub?.lastError ?? '';
    final meta = <String>[
      s.f('servers.count', {'n': group.profiles.length}),
      if (ui != null && ui.present && ui.expire > 0)
        '${s.t('servers.expires')} ${fmtDate(DateTime.fromMillisecondsSinceEpoch(ui.expire * 1000))}'
            '${switch (ui.daysLeft(DateTime.now())) {
              null => '',
              final d when d < 0 => ' (${s.t('home.expired')})',
              final d => ' (${s.f('home.daysLeft', {'n': d})})',
            }}',
      if (sub != null && sub.updatedAt > 0)
        '${s.t('servers.updatedAt')} ${fmtDateTime(DateTime.fromMillisecondsSinceEpoch(sub.updatedAt))}',
    ];
    final actions = <Widget>[
      if (canPing) ...[
        _MiniButton(
          key: ValueKey('group-sort-$id'),
          icon: Icons.sort_rounded,
          active: sorted,
          tooltip: s.t('servers.sort'),
          onTap: onSort,
        ),
        _MiniButton(
          key: ValueKey('group-ping-$id'),
          icon: Icons.bolt_rounded,
          busy: pinging,
          active: true,
          tooltip: s.t('servers.pingAll'),
          onTap: onPing,
        ),
      ],
      if (sub != null) ...[
        _MiniButton(
          key: ValueKey('group-refresh-$id'),
          icon: Icons.sync_rounded,
          busy: app.busyUpdating,
          tooltip: s.t('servers.update'),
          onTap: () async {
            final m = ScaffoldMessenger.of(context);
            final e = await app.refreshSubscription(sub);
            if (e == null) {
              toastOn(m, s.t('sub.updated'));
            } else if (e == 'timeout') {
              toastOn(m, s.t('sub.timeout'), error: true);
            } else if (e.startsWith('refused:')) {
              toastOn(m, s.f('sub.refused', {'e': e.substring(8)}), error: true);
            } else {
              toastOn(m, s.f('sub.failed', {'e': e}), error: true);
            }
          },
        ),
      ],
    ];
    return Glass(
      radius: Radii.l,
      padding: const EdgeInsets.fromLTRB(Gap.s, Gap.s, Gap.s, Gap.s),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Pressable(
                  key: ValueKey('group-toggle-$id'),
                  onTap: onToggle,
                  child: Padding(
                    padding: const EdgeInsetsDirectional.only(start: Gap.xs, top: 2, bottom: 2),
                    child: Row(
                      children: [
                        AnimatedRotation(
                          turns: collapsed ? (Directionality.of(context) == TextDirection.rtl ? 0.25 : -0.25) : 0,
                          duration: const Duration(milliseconds: 200),
                          child: Icon(Icons.expand_more_rounded, color: Palette.text2, size: 22),
                        ),
                        const SizedBox(width: Gap.xs),
                        Icon(
                          sub == null ? Icons.folder_special_outlined : Icons.cloud_sync_outlined,
                          color: sub == null ? Palette.lime : Palette.violetFg,
                          size: 20,
                        ),
                        const SizedBox(width: Gap.s),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15),
                              ),
                              Text(
                                s.num(meta.join(' · ')),
                                key: ValueKey('group-meta-$id'),
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(color: Palette.muted, fontSize: 11.5, height: 1.35),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              if (sub == null) ...actions,
              if (sub != null) ...[
                PopupMenuButton<String>(
                  key: ValueKey('group-menu-$id'),
                  tooltip: s.t('sub.edit'),
                  icon: Icon(Icons.more_horiz_rounded, size: 20, color: Palette.text2),
                  color: Palette.bg3,
                  padding: EdgeInsets.zero,
                  constraints: const BoxConstraints(minWidth: 180),
                  onSelected: (v) => v == 'delete' ? confirmDeleteSub(context, sub) : _editSub(context, sub),
                  itemBuilder: (_) => [
                    PopupMenuItem(
                      key: ValueKey('group-edit-$id'),
                      value: 'edit',
                      child: Row(
                        children: [
                          Icon(Icons.edit_outlined, size: 18, color: Palette.text2),
                          const SizedBox(width: Gap.s),
                          Text(s.t('sub.edit')),
                        ],
                      ),
                    ),
                    PopupMenuItem(
                      key: ValueKey('group-delete-$id'),
                      value: 'delete',
                      child: Row(
                        children: [
                          Icon(Icons.delete_outline_rounded, size: 18, color: Palette.danger),
                          const SizedBox(width: Gap.s),
                          Text(s.t('sub.delete'), style: TextStyle(color: Palette.danger)),
                        ],
                      ),
                    ),
                  ],
                ),
              ],
            ],
          ),
          // Usage bar (subscriptions) with the group actions at its end.
          if (sub != null)
            Padding(
              padding: const EdgeInsets.only(top: Gap.xs),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Expanded(
                    child: ui != null && ui.present
                        ? Padding(
                            padding: const EdgeInsets.only(bottom: 6),
                            child: UsageBar(key: ValueKey('group-usage-$id'), info: ui),
                          )
                        : const SizedBox.shrink(),
                  ),
                  const SizedBox(width: Gap.s),
                  ...actions,
                ],
              ),
            ),
          if (err.isNotEmpty) ...[
            const SizedBox(height: Gap.xs),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: Gap.xs),
              child: Text(
                err.startsWith('refused:') ? s.f('sub.refused', {'e': err.substring(8)}) : err,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Palette.danger, fontSize: 12),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// "used / total" with a bar: used = upload + download (subscription-userinfo).
/// An unlimited plan (total 0) shows ∞ and no fill.
class UsageBar extends StatelessWidget {
  const UsageBar({super.key, required this.info});
  final UserInfo info;

  /// Shown text, e.g. "1.5 GB / 10.0 GB" (∞ when unlimited).
  static String label(UserInfo i) => '${fmtBytes(i.used)} / ${i.total > 0 ? fmtBytes(i.total) : '∞'}';

  static TextStyle get _usageCaption => TextStyle(color: Palette.text2, fontSize: 11.5, fontWeight: FontWeight.w600);

  static double _captionWidth(BuildContext context, String text) {
    final tp = TextPainter(
      text: TextSpan(text: text, style: DefaultTextStyle.of(context).style.merge(_usageCaption)),
      textDirection: Directionality.of(context),
      textScaler: MediaQuery.textScalerOf(context),
      maxLines: 1,
    )..layout();
    final w = tp.width;
    tp.dispose();
    return w.ceilToDouble() + 1;
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final f = info.usedFraction;
    final color = f >= 0.9 ? Palette.danger : (f >= 0.75 ? Palette.warn : Palette.lime);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: Gap.xs),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Narrow subscription headers (320 dp, three action buttons) leave
          // ~130 dp here: the used / total value scales down so the caption
          // stays whole; only below 55% of the row does the caption give way
          // (ellipsis). Nothing overflows.
          LayoutBuilder(
            builder: (context, c) => Row(
              children: [
                Expanded(
                  child: Text(
                    s.t('servers.usage'),
                    maxLines: 1,
                    softWrap: false,
                    overflow: TextOverflow.ellipsis,
                    style: _usageCaption,
                  ),
                ),
                const SizedBox(width: 6),
                ConstrainedBox(
                  constraints: BoxConstraints(
                    maxWidth: math.max(c.maxWidth * 0.55, c.maxWidth - 6 - _captionWidth(context, s.t('servers.usage'))),
                  ),
                  child: FittedBox(
                    fit: BoxFit.scaleDown,
                    alignment: AlignmentDirectional.centerEnd,
                    child: Text(
                      s.num(label(info)),
                      key: const ValueKey('usage-label'),
                      maxLines: 1,
                      textDirection: TextDirection.ltr,
                      style: Fonts.metric(size: 12, weight: FontWeight.w700, color: Palette.text),
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 6),
          ClipRRect(
            borderRadius: BorderRadius.circular(9),
            child: LinearProgressIndicator(
              key: const ValueKey('usage-bar'),
              value: info.total > 0 ? f : 0,
              minHeight: 6,
              backgroundColor: Palette.glassHi,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

class _MiniButton extends StatelessWidget {
  const _MiniButton({super.key, required this.icon, required this.onTap, this.tooltip, this.active = false, this.busy = false});
  final IconData icon;
  final VoidCallback onTap;
  final String? tooltip;
  final bool active;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final b = SizedBox(
      width: 38,
      height: 38,
      child: busy
          ? Padding(
              padding: EdgeInsets.all(11),
              child: CircularProgressIndicator(strokeWidth: 2, color: Palette.lime),
            )
          : Pressable(
              onTap: onTap,
              child: Icon(icon, size: 20, color: active ? Palette.lime : Palette.text2),
            ),
    );
    return tooltip == null ? b : Tooltip(message: tooltip!, child: b);
  }
}

Future<void> _editSub(BuildContext context, Subscription sub) async {
  final app = AppScope.read(context);
  final s = S.of(context);
  final name = TextEditingController(text: sub.name);
  final url = TextEditingController(text: sub.url);
  final res = await showDialog<String>(
    context: context,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, set) => AlertDialog(
        title: Text(s.t('sub.edit')),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: name,
              decoration: InputDecoration(hintText: s.t('add.name')),
            ),
            const SizedBox(height: Gap.s),
            TextField(
              controller: url,
              textDirection: TextDirection.ltr,
              decoration: InputDecoration(hintText: s.t('add.subHint')),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, 'delete'),
            child: Text(s.t('sub.delete'), style: TextStyle(color: Palette.danger)),
          ),
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(s.t('common.cancel'))),
          FilledButton(onPressed: () => Navigator.pop(ctx, 'save'), child: Text(s.t('common.save'))),
        ],
      ),
    ),
  );
  if (res == 'save') {
    await app.editSubscription(sub, name: name.text.trim(), url: url.text.trim());
  } else if (res == 'delete' && context.mounted) {
    await confirmDeleteSub(context, sub);
  }
}

/// Asks, then removes the subscription and all of its configs.
Future<void> confirmDeleteSub(BuildContext context, Subscription sub) async {
  final app = AppScope.read(context);
  final s = S.of(context);
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(s.t('sub.delete')),
      content: Text(s.t('sub.deleteQ')),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(s.t('common.cancel'))),
        FilledButton(
          key: const ValueKey('sub-delete-confirm'),
          style: FilledButton.styleFrom(backgroundColor: Palette.danger),
          onPressed: () => Navigator.pop(ctx, true),
          child: Text(s.t('common.delete')),
        ),
      ],
    ),
  );
  if (ok == true) await app.removeSubscription(sub);
}

class _ServerTile extends StatelessWidget {
  const _ServerTile({super.key, required this.profile, required this.index, this.compact = false});
  final Profile profile;
  final int index;

  /// Compact row: smaller badge, one line, tighter padding.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final s = S.of(context);
    final sel = app.selected?.id == profile.id;
    // 1.0.8: on narrow phones (≤ 340 dp) a card puts its ping chip on the
    // label line, so a long name gets the full width for its two lines. A
    // compact row does so only when its name would not fit in two lines
    // beside the chip (other rows stay one line).
    final narrow = MediaQuery.sizeOf(context).width <= 340;
    final chip = PingChip(profile: profile, ping: app.pings[profile.id], testing: app.testing.contains(profile.id));
    // 1.0.7: the staggered fade-in is for rows that appear with the list
    // (page open, group expanded). A row that scrolls into view during a
    // scroll shows at once: animating every newly exposed row (an opacity
    // layer per row, rebuilt each frame) made scrolling long lists stutter.
    final scrolling = Scrollable.maybeOf(context)?.position.isScrollingNotifier.value ?? false;
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: scrolling ? 1 : 0, end: 1),
      duration: Duration(milliseconds: 220 + (index.clamp(0, 6) * 20)),
      curve: Curves.easeOutCubic,
      builder: (context, t, child) => Opacity(
        opacity: t,
        child: Transform.translate(offset: Offset(0, 18 * (1 - t)), child: child),
      ),
      child: Pressable(
        onTap: () {
          if (profile.isMTProto) {
            Native.instance.openUrl(profile.tgLink);
            return;
          }
          app.select(profile.id);
        },
        onLongPress: () => _profileMenu(context, profile),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 280),
          curve: Curves.easeOutCubic,
          padding: compact
              ? const EdgeInsets.symmetric(horizontal: Gap.m, vertical: Gap.s)
              : const EdgeInsets.fromLTRB(Gap.m, Gap.m, Gap.m, Gap.m),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(compact ? Radii.s : Radii.m),
            gradient: LinearGradient(
              begin: AlignmentDirectional.centerStart,
              end: AlignmentDirectional.centerEnd,
              colors: sel
                  ? [Palette.lime.withValues(alpha: 0.16), Palette.glass]
                  : [Palette.glass, Palette.glass.withValues(alpha: 0.03)],
            ),
            border: Border.all(color: sel ? Palette.lime.withValues(alpha: 0.6) : Palette.line),
            boxShadow: sel ? [BoxShadow(color: Palette.lime.withValues(alpha: 0.12), blurRadius: 20)] : null,
          ),
          child: LayoutBuilder(builder: (context, box) {
            var chipBelow = narrow && !compact;
            if (narrow && compact) {
              final tp = TextPainter(
                text: TextSpan(
                    text: profile.name,
                    style: DefaultTextStyle.of(context)
                        .style
                        .merge(const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5, height: 1.35))),
                textDirection: Directionality.of(context),
                textScaler: MediaQuery.textScalerOf(context),
                maxLines: 2,
              )..layout(maxWidth: math.max(0.0, box.maxWidth - 28 - Gap.s * 2 - 72));
              chipBelow = tp.didExceedMaxLines;
              tp.dispose();
            }
            return Row(
            children: [
              ProtoBadge(type: profile.type, size: compact ? 28 : 40, active: sel),
              SizedBox(width: compact ? Gap.s : Gap.m),
              Expanded(
                child: Builder(
                  builder: (context) {
                    // 1.0.8: long (Persian) names wrap to a second line
                    // instead of being cut after a few characters on
                    // narrow phones (320 dp); still never more than two.
                    final name = Text(
                      profile.name,
                      key: ValueKey('name-${profile.id}'),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      softWrap: true,
                      style: TextStyle(fontWeight: FontWeight.w700, color: Palette.text, fontSize: compact ? 13.5 : 14.5, height: 1.35),
                    );
                    // Always "protocol · transport · security" (LTR in every locale).
                    final label = Text(
                      profile.isMTProto ? s.t('servers.opensTelegram') : profile.label,
                      key: ValueKey('label-${profile.id}'),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      textDirection: profile.isMTProto ? null : TextDirection.ltr,
                      style: TextStyle(color: Palette.muted, fontSize: compact ? 11 : 12),
                    );
                    if (compact) {
                      // The name gets the room it needs first; the protocol
                      // label takes what is left with an ellipsis (it is
                      // hidden only when less than ~36 dp remain, e.g. a long
                      // name that wraps at 320 dp).
                      return LayoutBuilder(builder: (context, c) {
                        final style = DefaultTextStyle.of(context).style.merge(name.style);
                        final tp = TextPainter(
                          text: TextSpan(text: profile.name, style: style),
                          textDirection: Directionality.of(context),
                          textScaler: MediaQuery.textScalerOf(context),
                          maxLines: 1,
                        )..layout();
                        final nameW = tp.width;
                        tp.dispose();
                        final rest = c.maxWidth - nameW - Gap.s;
                        if (chipBelow) {
                          return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                            name,
                            const SizedBox(height: 2),
                            Row(children: [
                              Expanded(child: label),
                              const SizedBox(width: Gap.s),
                              chip,
                            ]),
                          ]);
                        }
                        if (rest < 36) return name;
                        return Row(
                          children: [
                            SizedBox(width: nameW.ceilToDouble() + 1, child: name),
                            const SizedBox(width: Gap.s),
                            Expanded(child: Align(alignment: AlignmentDirectional.centerEnd, child: label)),
                          ],
                        );
                      });
                    }
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        name,
                        const SizedBox(height: 2),
                        if (chipBelow)
                          Row(children: [Expanded(child: label), const SizedBox(width: Gap.s), chip])
                        else
                          label,
                      ],
                    );
                  },
                ),
              ),
              if (!chipBelow) ...[const SizedBox(width: Gap.s), chip],
            ],
          );
          }),
        ),
      ),
    );
  }
}

Future<void> _profileMenu(BuildContext context, Profile p) async {
  final app = AppScope.read(context);
  final s = S.of(context);
  final action = await showModalBottomSheet<String>(
    context: context,
    builder: (ctx) => SolidSheet(
      radius: Radii.xl,
      padding: const EdgeInsets.fromLTRB(Gap.m, Gap.l, Gap.m, Gap.xl),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.all(Gap.s),
            child: Row(
              children: [
                ProtoBadge(type: p.type, size: 36),
                const SizedBox(width: Gap.m),
                Expanded(
                  child: Text(
                    p.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontWeight: FontWeight.w800),
                  ),
                ),
              ],
            ),
          ),
          if (p.pingable && app.pingErrors[p.id] != null)
            ListTile(
              dense: true,
              leading: Icon(Icons.info_outline_rounded, color: Palette.danger),
              title: Text(
                friendlyError(s, app.pingErrors[p.id]!),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Palette.danger, fontSize: 13),
              ),
              subtitle: Text(s.t('err.details'), style: TextStyle(color: Palette.muted, fontSize: 12)),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.s)),
              onTap: () => Navigator.pop(ctx, 'error'),
            ),
          for (final (key, icon, label) in [
            if (p.pingable) ('test', Icons.speed_rounded, s.t('menu.test')),
            ('edit', Icons.tune_rounded, s.t('menu.edit')),
            ('rename', Icons.edit_rounded, s.t('menu.rename')),
            if (p.link.isNotEmpty) ('copy', Icons.copy_rounded, s.t('menu.copy')),
            ('delete', Icons.delete_outline_rounded, s.t('menu.delete')),
          ])
            ListTile(
              leading: Icon(icon, color: key == 'delete' ? Palette.danger : Palette.text2),
              title: Text(label, style: TextStyle(color: key == 'delete' ? Palette.danger : Palette.text)),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.s)),
              onTap: () => Navigator.pop(ctx, key),
            ),
        ],
      ),
    ),
  );
  if (!context.mounted) return;
  switch (action) {
    case 'error':
      await showErrorDetails(context, app.pingErrors[p.id] ?? '', title: s.t('ping.failed'), onRetry: () => app.ping(p));
    case 'test':
      app.ping(p);
    case 'copy':
      await Clipboard.setData(ClipboardData(text: p.link));
      if (context.mounted) showToast(context, s.t('menu.copied'));
    case 'delete':
      app.deleteProfile(p);
    case 'edit':
      await ProfileEditorPage.open(context, p);
    case 'rename':
      final c = TextEditingController(text: p.name);
      final name = await showDialog<String>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(s.t('menu.rename')),
          content: TextField(controller: c, autofocus: true),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx), child: Text(s.t('common.cancel'))),
            FilledButton(onPressed: () => Navigator.pop(ctx, c.text.trim()), child: Text(s.t('common.save'))),
          ],
        ),
      );
      if (name != null && name.isNotEmpty) app.renameProfile(p, name);
  }
}

class _Empty extends StatelessWidget {
  const _Empty({required this.onAdd});
  final VoidCallback onAdd;
  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    return Padding(
      padding: const EdgeInsets.all(Gap.xxl),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 96,
            height: 96,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              gradient: RadialGradient(colors: [Palette.violet.withValues(alpha: 0.35), Colors.transparent]),
            ),
            child: Icon(Icons.public_rounded, size: 44, color: Palette.violetFg),
          ),
          const SizedBox(height: Gap.l),
          Text(s.t('servers.empty'), style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800)),
          const SizedBox(height: Gap.s),
          Text(
            s.t('servers.emptyHint'),
            textAlign: TextAlign.center,
            style: TextStyle(color: Palette.muted, height: 1.6),
          ),
          const SizedBox(height: Gap.xl),
          SizedBox(
            width: 200,
            child: PrimaryButton(label: s.t('add.title'), icon: Icons.add_rounded, onTap: onAdd),
          ),
        ],
      ),
    );
  }
}
