import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import '../widgets/sub_scaffold.dart';

class PerAppPage extends StatefulWidget {
  const PerAppPage({super.key});
  @override
  State<PerAppPage> createState() => _PerAppPageState();
}

class _PerAppPageState extends State<PerAppPage> {
  List<Map<String, dynamic>> _apps = [];
  bool _loading = true;
  bool _system = false;
  String _q = '';
  final _icons = <String, Uint8List?>{};

  @override
  void initState() {
    super.initState();
    Native.instance.installedApps().then((a) {
      if (!mounted) return;
      a.sort((x, y) => '${x['label']}'.toLowerCase().compareTo('${y['label']}'.toLowerCase()));
      setState(() {
        _apps = a;
        _loading = false;
      });
    });
  }

  Future<Uint8List?> _icon(String pkg) async {
    if (_icons.containsKey(pkg)) return _icons[pkg];
    final b = await Native.instance.appIcon(pkg);
    _icons[pkg] = b;
    return b;
  }

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final s = S.of(context);
    final st = app.settings;
    final selected = st.perAppPackages.toSet();
    final own = app.device['package'];
    final iconPx = (36 * MediaQuery.devicePixelRatioOf(context)).round();
    final list = _apps.where((a) {
      if (a['package'] == own) return false;
      if (!_system && a['system'] == true && !selected.contains(a['package'])) return false;
      if (_q.isEmpty) return true;
      return '${a['label']} ${a['package']}'.toLowerCase().contains(_q.toLowerCase());
    }).toList()
      ..sort((x, y) {
        final a = selected.contains(x['package']) ? 0 : 1, b = selected.contains(y['package']) ? 0 : 1;
        return a.compareTo(b);
      });
    return SubScaffold(
      title: s.t('perapp.title'),
      child: Column(children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Gap.page),
          child: _ModeBar(
            value: st.perAppMode,
            labels: {for (final m in _modes) m: s.t('perapp.$m')},
            onChanged: (v) {
              st.perAppMode = v;
              app.saveSettings();
            },
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(Gap.page, Gap.m, Gap.page, Gap.xs),
          child: TextField(
            onChanged: (v) => setState(() => _q = v),
            decoration: InputDecoration(hintText: s.t('perapp.search'), prefixIcon: const Icon(Icons.search_rounded, color: Palette.muted)),
          ),
        ),
        SwitchListTile(
          contentPadding: const EdgeInsets.symmetric(horizontal: Gap.page),
          title: Text(s.t('perapp.system'), style: const TextStyle(fontSize: 14)),
          value: _system,
          onChanged: (v) => setState(() => _system = v),
        ),
        Expanded(
          child: _loading
              ? const Center(child: CircularProgressIndicator())
              // Full opacity in every mode (no dimming when the mode is "All apps").
              : ListView.builder(
                    padding: const EdgeInsets.fromLTRB(Gap.page, 0, Gap.page, Gap.xxl),
                    itemCount: list.length,
                    itemBuilder: (context, i) {
                      final a = list[i];
                      final pkg = '${a['package']}';
                      final on = selected.contains(pkg);
                      return Padding(
                        padding: const EdgeInsets.only(bottom: Gap.xs),
                        child: Pressable(
                          onTap: () {
                            if (on) {
                              st.perAppPackages.remove(pkg);
                            } else {
                              st.perAppPackages.add(pkg);
                            }
                            app.saveSettings();
                          },
                          child: AnimatedContainer(
                            duration: const Duration(milliseconds: 200),
                            padding: const EdgeInsets.all(Gap.s + 2),
                            decoration: BoxDecoration(
                              color: on ? Palette.lime.withValues(alpha: 0.1) : Palette.glass,
                              borderRadius: BorderRadius.circular(Radii.m),
                              border: Border.all(color: on ? Palette.lime.withValues(alpha: 0.5) : Palette.line),
                            ),
                            child: Row(children: [
                              SizedBox(
                                width: 36,
                                height: 36,
                                // 1.0.7: a loaded icon is used straight from the cache
                                // (no new Future per build) and decoded at its shown size.
                                child: _icons.containsKey(pkg)
                                    ? _iconImage(_icons[pkg], iconPx)
                                    : FutureBuilder<Uint8List?>(
                                        future: _icon(pkg),
                                        builder: (_, snap) => _iconImage(snap.data, iconPx),
                                      ),
                              ),
                              const SizedBox(width: Gap.m),
                              Expanded(
                                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                  Text('${a['label']}', maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w600)),
                                  Text(pkg,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      textDirection: TextDirection.ltr,
                                      style: const TextStyle(color: Palette.muted, fontSize: 11)),
                                ]),
                              ),
                              Icon(on ? Icons.check_circle_rounded : Icons.circle_outlined, color: on ? Palette.lime : Palette.muted),
                            ]),
                          ),
                        ),
                      );
                    },
                  ),
        ),
      ]),
    );
  }
}

Widget _iconImage(Uint8List? bytes, int px) => bytes == null
    ? const Icon(Icons.android_rounded, color: Palette.muted)
    : ClipRRect(
        borderRadius: BorderRadius.circular(9),
        child: Image.memory(bytes, gaplessPlayback: true, cacheWidth: px, filterQuality: FilterQuality.medium),
      );

const _modes = ['off', 'include', 'exclude'];

/// Three-way mode switch. Each label gets an equal third and shrinks to fit
/// on one line (320 dp phones, long Persian labels) instead of wrapping.
class _ModeBar extends StatelessWidget {
  const _ModeBar({required this.value, required this.labels, required this.onChanged});
  final String value;
  final Map<String, String> labels;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 44,
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(
        color: Palette.glass,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: Palette.line),
      ),
      child: Row(children: [
        for (final m in _modes)
          Expanded(
            child: Pressable(
              key: ValueKey('perapp-mode-$m'),
              onTap: () {
                if (m != value) onChanged(m);
              },
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 200),
                curve: Curves.easeOutCubic,
                alignment: Alignment.center,
                padding: const EdgeInsets.symmetric(horizontal: 6),
                decoration: BoxDecoration(
                  color: m == value ? Palette.lime : Colors.transparent,
                  borderRadius: BorderRadius.circular(99),
                ),
                child: FittedBox(
                  fit: BoxFit.scaleDown,
                  child: Text(labels[m]!,
                      maxLines: 1,
                      softWrap: false,
                      style: TextStyle(
                          color: m == value ? Palette.limeInk : Palette.text2, fontWeight: FontWeight.w700, fontSize: 13)),
                ),
              ),
            ),
          ),
      ]),
    );
  }
}
