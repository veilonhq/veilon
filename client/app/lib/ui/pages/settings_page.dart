import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/app_state.dart';
import '../../core/contact.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import 'about_page.dart';
import 'logs_page.dart';
import 'per_app_page.dart';
import 'rules_page.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({super.key});

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final st = app.settings;
    final s = S.of(context);
    void save() => app.saveSettings();
    Future<void> push(Widget w) => Navigator.of(context).push(MaterialPageRoute(builder: (_) => w));

    return ListView(
      physics: pagePhysics,
      padding: EdgeInsets.fromLTRB(Gap.page, 0, Gap.page, Gap.l + MediaQuery.viewPaddingOf(context).bottom),
      children: [
        if (app.needsReconnect)
          Padding(
            padding: const EdgeInsets.only(top: Gap.l),
            child: Pressable(
              onTap: () async {
                await app.disconnect();
                await Future.delayed(const Duration(milliseconds: 700));
                await app.connect();
              },
              child: Glass(
                tint: Palette.warn,
                borderColor: Palette.warn.withValues(alpha: 0.4),
                padding: const EdgeInsets.all(Gap.m),
                child: Row(children: [
                  const Icon(Icons.restart_alt_rounded, color: Palette.warn),
                  const SizedBox(width: Gap.m),
                  Expanded(child: Text(s.t('settings.reconnectNeeded'), style: const TextStyle(fontWeight: FontWeight.w700))),
                ]),
              ),
            ),
          ),
        SectionTitle(s.t('settings.general')),
        _Group(children: [
          _Tile(
            icon: Icons.translate_rounded,
            title: s.t('settings.language'), info: 'info.language',
            trailing: _Segmented(
              values: const ['fa', 'en'],
              labels: const ['فارسی', 'English'],
              value: st.language,
              onChanged: (v) => app.setLanguage(v),
            ),
          ),
        ]),
        SectionTitle(s.t('settings.connection')),
        _Group(children: [
          _Switch(
              icon: Icons.shield_moon_outlined,
              title: s.t('settings.killSwitch'), info: 'info.killSwitch',
              subtitle: s.t('settings.killSwitchHint'),
              value: st.killSwitch,
              onChanged: (v) {
                st.killSwitch = v;
                save();
              }),
          _Tile(
            icon: Icons.lock_outline_rounded,
            title: s.t('settings.alwaysOn'), info: 'info.alwaysOn',
            subtitle: s.t('settings.alwaysOnHint'),
            onTap: () => Native.instance.openVpnSettings(),
            chevron: true,
          ),
          _Switch(
              icon: Icons.autorenew_rounded,
              title: s.t('settings.reconnect'), info: 'info.reconnect',
              subtitle: s.t('settings.reconnectHint'),
              value: st.autoReconnect,
              onChanged: (v) {
                st.autoReconnect = v;
                save();
              }),
          _Switch(
              icon: Icons.power_settings_new_rounded,
              title: s.t('settings.boot'), info: 'info.boot',
              value: st.autoConnect,
              onChanged: (v) {
                st.autoConnect = v;
                save();
              }),
          _Switch(
              icon: Icons.auto_awesome_rounded,
              title: s.t('settings.autoFastest'), info: 'info.autoFastest',
              subtitle: s.t('settings.autoFastestHint'),
              value: st.autoSelectFastest,
              onChanged: (v) {
                st.autoSelectFastest = v;
                save();
              }),
        ]),
        SectionTitle(s.t('settings.routing')),
        _Group(children: [
          _Switch(
              icon: Icons.flag_outlined,
              title: s.t('settings.bypassIran'), info: 'info.bypassIran',
              subtitle: s.t('settings.bypassIranHint'),
              value: st.bypassIran,
              onChanged: (v) {
                st.bypassIran = v;
                save();
              }),
          _Switch(
              icon: Icons.router_outlined,
              title: s.t('settings.bypassLan'), info: 'info.bypassLan',
              value: st.bypassLan,
              onChanged: (v) {
                st.bypassLan = v;
                save();
              }),
          _Switch(
              icon: Icons.block_rounded,
              title: s.t('settings.blockAds'), info: 'info.blockAds',
              value: st.blockAds,
              onChanged: (v) {
                st.blockAds = v;
                save();
              }),
          _Tile(
            icon: Icons.apps_rounded,
            title: s.t('settings.perApp'), info: 'info.perApp',
            subtitle: s.t('perapp.${st.perAppMode}'),
            chevron: true,
            onTap: () => push(const PerAppPage()),
          ),
          _Tile(
            icon: Icons.alt_route_rounded,
            title: s.t('settings.domainRules'), info: 'info.domainRules',
            subtitle: s.num('${st.proxyRules.length} · ${st.directRules.length} · ${st.blockRules.length}'),
            chevron: true,
            onTap: () => push(const RulesPage()),
          ),
        ]),
        SectionTitle(s.t('settings.dns')),
        _Group(children: [
          _Tile(
            icon: Icons.dns_outlined,
            title: s.t('settings.remoteDns'), info: 'info.remoteDns',
            subtitle: st.remoteDns,
            ltrSubtitle: true,
            onTap: () => _editText(context, s.t('settings.remoteDns'), st.remoteDns, (v) {
              st.remoteDns = v;
              save();
            }, presets: const ['https://1.1.1.1/dns-query', 'https://8.8.8.8/dns-query', 'tcp://1.1.1.1', 'tls://8.8.8.8', '1.1.1.1']),
          ),
          _Tile(
            icon: Icons.dns_rounded,
            title: s.t('settings.directDns'), info: 'info.directDns',
            subtitle: st.directDns,
            ltrSubtitle: true,
            onTap: () => _editText(context, s.t('settings.directDns'), st.directDns, (v) {
              st.directDns = v;
              save();
            }, presets: const ['223.5.5.5', '8.8.8.8', '1.1.1.1', '78.157.42.100', '178.22.122.100', '217.218.127.127']),
          ),
        ]),
        SectionTitle(s.t('settings.advanced')),
        _Group(children: [
          _Tile(
            icon: Icons.straighten_rounded,
            title: s.t('settings.mtu'), info: 'info.mtu',
            subtitle: s.num('${st.mtu}'),
            onTap: () => _editText(context, s.t('settings.mtu'), '${st.mtu}', (v) {
              final n = int.tryParse(v);
              if (n != null && n >= 1280 && n <= 65535) {
                st.mtu = n;
                save();
              }
            }, number: true, presets: const ['1280', '1400', '1500', '9000']),
          ),
          _Switch(
              icon: Icons.speed_outlined,
              title: s.t('settings.disableQuic'), info: 'info.disableQuic',
              subtitle: s.t('settings.disableQuicHint'),
              value: st.disableQuic,
              onChanged: (v) {
                st.disableQuic = v;
                save();
              }),
          _Switch(
              icon: Icons.six_ft_apart_rounded,
              title: s.t('settings.ipv6'), info: 'info.ipv6',
              value: st.ipv6,
              onChanged: (v) {
                st.ipv6 = v;
                save();
              }),
          _ChoiceTile(
            icon: Icons.memory_rounded,
            title: s.t('settings.core'), info: 'info.core',
            subtitle: s.t('settings.coreHint'),
            values: const ['auto', 'sing-box', 'xray'],
            labels: [s.t('core.auto'), 'sing-box', 'Xray'],
            value: st.core,
            onChanged: (v) {
              st.core = v;
              save();
            },
          ),
          _Tile(
            icon: Icons.timer_outlined,
            title: s.t('settings.pingUrl'), info: 'info.pingUrl',
            subtitle: st.pingUrl,
            ltrSubtitle: true,
            onTap: () => _editText(context, s.t('settings.pingUrl'), st.pingUrl, (v) {
              st.pingUrl = v;
              save();
            }, presets: const ['https://www.gstatic.com/generate_204', 'https://cp.cloudflare.com/', 'http://www.google.com/generate_204']),
          ),
          _Switch(
              icon: Icons.handshake_outlined,
              title: s.t('settings.fullHandshakePing'), info: 'info.fullHandshakePing',
              subtitle: s.t('settings.fullHandshakePingHint'),
              value: st.fullHandshakePing,
              onChanged: (v) {
                st.fullHandshakePing = v;
                save();
              }),
          _Tile(
            icon: Icons.bug_report_outlined,
            title: s.t('settings.logLevel'), info: 'info.logLevel',
            trailing: _Segmented(
              values: const ['error', 'warn', 'info', 'debug'],
              labels: const ['E', 'W', 'I', 'D'],
              value: st.logLevel,
              onChanged: (v) {
                st.logLevel = v;
                save();
              },
            ),
          ),
        ]),
        SectionTitle(s.t('settings.tools')),
        _Group(children: [
          _Tile(icon: Icons.receipt_long_outlined, title: s.t('settings.logs'), info: 'info.logs', chevron: true, onTap: () => push(const LogsPage())),
          _Tile(
            icon: Icons.fingerprint_rounded,
            title: s.t('settings.hwid'), info: 'info.hwid',
            subtitle: '${app.device['hwid'] ?? ''}',
            ltrSubtitle: true,
            onTap: () {
              Clipboard.setData(ClipboardData(text: '${app.device['hwid'] ?? ''}'));
              showToast(context, s.t('menu.copied'));
            },
          ),
          _Tile(icon: Icons.info_outline_rounded, title: s.t('settings.about'), chevron: true, onTap: () => push(const AboutPage())),
        ]),
        if (ContactLink.uri != null) ...[
          const SizedBox(height: Gap.m),
          _Group(children: [
            _Tile(
              key: const ValueKey('settings-contact'),
              icon: Icons.send_rounded,
              title: s.t('settings.contact'),
              subtitle: '@${ContactLink.uri!.pathSegments.last}',
              ltrSubtitle: true,
              trailing: const Icon(Icons.open_in_new_rounded, size: 18, color: Palette.muted),
              onTap: () async {
                if (!await ContactLink.open() && context.mounted) showToast(context, s.t('settings.contactFailed'));
              },
            ),
          ]),
        ],
      ],
    );
  }
}

Future<void> _editText(BuildContext context, String title, String value, ValueChanged<String> onSave,
    {bool number = false, List<String> presets = const []}) async {
  final s = S.of(context);
  final c = TextEditingController(text: value);
  final r = await showDialog<String>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
        TextField(
          controller: c,
          autofocus: true,
          textDirection: TextDirection.ltr,
          keyboardType: number ? TextInputType.number : TextInputType.url,
        ),
        if (presets.isNotEmpty) ...[
          const SizedBox(height: Gap.m),
          Wrap(spacing: Gap.s, runSpacing: Gap.s, children: [
            for (final p in presets)
              ActionChip(
                label: Text(p, textDirection: TextDirection.ltr, style: const TextStyle(fontSize: 12)),
                onPressed: () => c.text = p,
                backgroundColor: Palette.glass,
                side: const BorderSide(color: Palette.line),
              ),
          ]),
        ],
      ]),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx), child: Text(s.t('common.cancel'))),
        FilledButton(onPressed: () => Navigator.pop(ctx, c.text.trim()), child: Text(s.t('common.save'))),
      ],
    ),
  );
  if (r != null && r.isNotEmpty) onSave(r);
}

class _Group extends StatelessWidget {
  const _Group({required this.children});
  final List<Widget> children;
  @override
  Widget build(BuildContext context) {
    return Glass(
      padding: const EdgeInsets.symmetric(vertical: Gap.xs),
      radius: Radii.l,
      child: Column(children: [
        for (var i = 0; i < children.length; i++) ...[
          children[i],
          if (i < children.length - 1) const Divider(height: 1, indent: 58, color: Palette.line),
        ],
      ]),
    );
  }
}

class _Tile extends StatelessWidget {
  const _Tile({
    super.key,
    required this.icon,
    required this.title,
    this.subtitle,
    this.trailing,
    this.onTap,
    this.chevron = false,
    this.ltrSubtitle = false,
    this.info,
  });
  final IconData icon;
  final String title;
  /// strings key of the explanation the (i) button next to the title shows.
  final String? info;
  final String? subtitle;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool chevron;
  final bool ltrSubtitle;

  @override
  Widget build(BuildContext context) {
    final rtl = Directionality.of(context) == TextDirection.rtl;
    return InkWell(
      onTap: onTap == null
          ? null
          : () {
              HapticFeedback.selectionClick();
              onTap!();
            },
      borderRadius: BorderRadius.circular(Radii.m),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: Gap.m, vertical: Gap.m),
        child: Row(children: [
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(color: Palette.glassHi, borderRadius: BorderRadius.circular(11)),
            child: Icon(icon, size: 18, color: Palette.text2),
          ),
          const SizedBox(width: Gap.m),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              _TitleLine(title: title, info: info),
              if (subtitle != null && subtitle!.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: Text(subtitle!,
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      textDirection: ltrSubtitle ? TextDirection.ltr : null,
                      style: const TextStyle(color: Palette.muted, fontSize: 12, height: 1.4)),
                ),
            ]),
          ),
          if (trailing != null) ...[
            const SizedBox(width: Gap.s),
            // Not Flexible: a Flexible next to the Expanded title would take
            // half the row and leave the switch floating mid-row.
            ConstrainedBox(
              constraints: BoxConstraints(maxWidth: MediaQuery.sizeOf(context).width * 0.42),
              child: FittedBox(fit: BoxFit.scaleDown, alignment: AlignmentDirectional.centerEnd, child: trailing!),
            ),
          ],
          if (chevron) Icon(rtl ? Icons.chevron_left_rounded : Icons.chevron_right_rounded, color: Palette.muted),
        ]),
      ),
    );
  }
}

class _Switch extends StatelessWidget {
  const _Switch({required this.icon, required this.title, this.info, this.subtitle, required this.value, required this.onChanged});
  final IconData icon;
  final String title;
  final String? info;
  final String? subtitle;
  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    return _Tile(
      icon: icon,
      title: title,
      info: info,
      subtitle: subtitle,
      onTap: () => onChanged(!value),
      trailing: Switch(value: value, onChanged: (v) {
        HapticFeedback.selectionClick();
        onChanged(v);
      }),
    );
  }
}

/// A setting whose segmented choice sits under its title (room for long
/// labels and a hint on narrow screens).
class _ChoiceTile extends StatelessWidget {
  const _ChoiceTile({
    required this.icon,
    required this.title,
    this.info,
    this.subtitle,
    required this.values,
    required this.labels,
    required this.value,
    required this.onChanged,
  });
  final IconData icon;
  final String title;
  final String? info;
  final String? subtitle;
  final List<String> values;
  final List<String> labels;
  final String value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: Gap.m, vertical: Gap.m),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Container(
          width: 34,
          height: 34,
          decoration: BoxDecoration(color: Palette.glassHi, borderRadius: BorderRadius.circular(11)),
          child: Icon(icon, size: 18, color: Palette.text2),
        ),
        const SizedBox(width: Gap.m),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            _TitleLine(title: title, info: info),
            if (subtitle != null && subtitle!.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: Text(subtitle!, style: const TextStyle(color: Palette.muted, fontSize: 12, height: 1.4)),
              ),
            const SizedBox(height: Gap.s),
            FittedBox(
              fit: BoxFit.scaleDown,
              alignment: AlignmentDirectional.centerStart,
              child: _Segmented(values: values, labels: labels, value: value, onChanged: onChanged),
            ),
          ]),
        ),
      ]),
    );
  }
}

class _Segmented extends StatelessWidget {
  const _Segmented({required this.values, required this.labels, required this.value, required this.onChanged});
  final List<String> values;
  final List<String> labels;
  final String value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(color: Palette.glass, borderRadius: BorderRadius.circular(99), border: Border.all(color: Palette.line)),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        for (var i = 0; i < values.length; i++)
          GestureDetector(
            onTap: () {
              HapticFeedback.selectionClick();
              onChanged(values[i]);
            },
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 220),
              curve: Curves.easeOutCubic,
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
              decoration: BoxDecoration(
                color: values[i] == value ? Palette.lime : Colors.transparent,
                borderRadius: BorderRadius.circular(99),
              ),
              child: Text(labels[i],
                  style: TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w700,
                      color: values[i] == value ? Palette.limeInk : Palette.text2)),
            ),
          ),
      ]),
    );
  }
}

/// Setting title with an optional (i) button right after it; the button
/// opens a short explanation of the setting (fa / en).
class _TitleLine extends StatelessWidget {
  const _TitleLine({required this.title, this.info});
  final String title;
  final String? info;

  @override
  Widget build(BuildContext context) {
    const style = TextStyle(fontWeight: FontWeight.w600, fontSize: 14.5);
    if (info == null) return Text(title, style: style);
    return Text.rich(
      TextSpan(children: [
        TextSpan(text: title),
        WidgetSpan(
          alignment: PlaceholderAlignment.middle,
          child: _InfoButton(title: title, info: info!),
        ),
      ]),
      style: style,
    );
  }
}

class _InfoButton extends StatelessWidget {
  const _InfoButton({required this.title, required this.info});
  final String title;
  final String info;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    return Semantics(
      button: true,
      label: s.t('info.tooltip'),
      child: GestureDetector(
        key: ValueKey('info-$info'),
        behavior: HitTestBehavior.opaque,
        onTap: () {
          HapticFeedback.selectionClick();
          showSettingInfo(context, title, s.t(info));
        },
        child: Padding(
          // 18 dp glyph; the padding gives a ~32 dp touch target.
          padding: const EdgeInsetsDirectional.fromSTEB(6, 6, 8, 6),
          child: Icon(Icons.info_outline_rounded, size: 18, color: Palette.violetFg.withValues(alpha: 0.9)),
        ),
      ),
    );
  }
}

/// The explanation sheet of one setting.
Future<void> showSettingInfo(BuildContext context, String title, String text) {
  final s = S.of(context);
  return showModalBottomSheet<void>(
    context: context,
    backgroundColor: Colors.transparent,
    isScrollControlled: true,
    builder: (ctx) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(Gap.page, 0, Gap.page, Gap.m),
        child: Container(
          key: const ValueKey('setting-info-sheet'),
          padding: const EdgeInsets.fromLTRB(Gap.l, Gap.l, Gap.l, Gap.m),
          decoration: BoxDecoration(
            color: Palette.bg2,
            borderRadius: BorderRadius.circular(Radii.l),
            border: Border.all(color: Palette.line2),
            boxShadow: const [BoxShadow(color: Color(0x99000000), blurRadius: 30, offset: Offset(0, 10))],
          ),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Container(
                width: 30,
                height: 30,
                decoration: BoxDecoration(color: Palette.violet.withValues(alpha: 0.18), borderRadius: BorderRadius.circular(10)),
                child: const Icon(Icons.info_outline_rounded, size: 18, color: Palette.violet),
              ),
              const SizedBox(width: Gap.m),
              Expanded(child: Text(title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16))),
            ]),
            const SizedBox(height: Gap.m),
            Text(text, style: const TextStyle(color: Palette.text2, fontSize: 13.5, height: 1.6)),
            const SizedBox(height: Gap.m),
            Align(
              alignment: AlignmentDirectional.centerEnd,
              child: TextButton(onPressed: () => Navigator.pop(ctx), child: Text(s.t('common.ok'))),
            ),
          ]),
        ),
      ),
    ),
  );
}
