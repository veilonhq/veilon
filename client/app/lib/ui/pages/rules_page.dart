import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import '../widgets/sub_scaffold.dart';

class RulesPage extends StatefulWidget {
  const RulesPage({super.key});
  @override
  State<RulesPage> createState() => _RulesPageState();
}

class _RulesPageState extends State<RulesPage> {
  late final TextEditingController _proxy, _direct, _block;
  int _tab = 0;

  @override
  void initState() {
    super.initState();
    final st = AppScope.read(context).settings;
    _proxy = TextEditingController(text: st.proxyRules.join('\n'));
    _direct = TextEditingController(text: st.directRules.join('\n'));
    _block = TextEditingController(text: st.blockRules.join('\n'));
  }

  @override
  void dispose() {
    _proxy.dispose();
    _direct.dispose();
    _block.dispose();
    super.dispose();
  }

  List<String> _lines(TextEditingController c) =>
      c.text.split('\n').map((e) => e.trim()).where((e) => e.isNotEmpty).toList();

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final app = AppScope.of(context);
    final ctrls = [_proxy, _direct, _block];
    final colors = [Palette.violetFg, Palette.lime, Palette.danger];
    return SubScaffold(
      title: s.t('rules.title'),
      actions: [
        GlassIconButton(
          icon: Icons.check_rounded,
          color: Palette.lime,
          onTap: () {
            final st = app.settings;
            st.proxyRules = _lines(_proxy);
            st.directRules = _lines(_direct);
            st.blockRules = _lines(_block);
            app.saveSettings();
            Navigator.of(context).pop();
          },
        ),
      ],
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: Gap.page),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Row(children: [
            for (var i = 0; i < 3; i++) ...[
              Expanded(
                child: Pressable(
                  onTap: () => setState(() => _tab = i),
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 220),
                    height: 42,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: _tab == i ? colors[i].withValues(alpha: 0.18) : Palette.glass,
                      borderRadius: BorderRadius.circular(Radii.m),
                      border: Border.all(color: _tab == i ? colors[i] : Palette.line),
                    ),
                    child: Text(s.t(['rules.proxy', 'rules.direct', 'rules.block'][i]),
                        style: TextStyle(fontWeight: FontWeight.w800, color: _tab == i ? colors[i] : Palette.text2)),
                  ),
                ),
              ),
              if (i < 2) const SizedBox(width: Gap.s),
            ],
          ]),
          const SizedBox(height: Gap.m),
          Text(s.t('rules.hint'), style: TextStyle(color: Palette.muted, fontSize: 12, height: 1.6)),
          const SizedBox(height: Gap.m),
          Expanded(
            child: TextField(
              controller: ctrls[_tab],
              maxLines: null,
              expands: true,
              textAlignVertical: TextAlignVertical.top,
              textDirection: TextDirection.ltr,
              style: const TextStyle(fontSize: 13.5, height: 1.6),
              decoration: const InputDecoration(hintText: 'domain:example.com'),
            ),
          ),
          const SizedBox(height: Gap.l),
        ]),
      ),
    );
  }
}
