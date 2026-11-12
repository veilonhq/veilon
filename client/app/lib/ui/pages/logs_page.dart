import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/foreground_timer.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import '../widgets/sub_scaffold.dart';

class LogsPage extends StatefulWidget {
  const LogsPage({super.key});
  @override
  State<LogsPage> createState() => _LogsPageState();
}

class _LogsPageState extends State<LogsPage> {
  final List<Map<String, dynamic>> _lines = [];
  final _scroll = ScrollController();
  ForegroundTimer? _timer;
  int _seq = 0;
  bool _follow = true;

  @override
  void initState() {
    super.initState();
    _poll();
    _timer = ForegroundTimer(const Duration(milliseconds: 1200), _poll);
  }

  Future<void> _poll() async {
    final add = await Native.instance.logs(_seq);
    if (!mounted || add.isEmpty) return;
    setState(() {
      _lines.addAll(add);
      _seq = (add.last['seq'] as num).toInt();
      if (_lines.length > 2000) _lines.removeRange(0, _lines.length - 2000);
    });
    if (_follow) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (_scroll.hasClients) _scroll.animateTo(_scroll.position.maxScrollExtent, duration: const Duration(milliseconds: 250), curve: Curves.easeOut);
      });
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  Color _color(String l) => switch (l) {
        'error' => Palette.danger,
        'warn' => Palette.warn,
        'info' => Palette.info,
        _ => Palette.muted,
      };

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    return SubScaffold(
      title: s.t('logs.title'),
      actions: [
        GlassIconButton(
          icon: Icons.copy_all_rounded,
          tooltip: s.t('logs.copy'),
          onTap: () {
            Clipboard.setData(ClipboardData(text: _lines.map((l) => '[${l['level']}] ${l['msg']}').join('\n')));
            showToast(context, s.t('menu.copied'));
          },
        ),
        const SizedBox(width: Gap.s),
        GlassIconButton(
          icon: Icons.delete_sweep_rounded,
          tooltip: s.t('logs.clear'),
          onTap: () async {
            await Native.instance.clearLogs();
            setState(_lines.clear);
          },
        ),
      ],
      child: _lines.isEmpty
          ? Center(child: Text(s.t('logs.empty'), style: const TextStyle(color: Palette.muted)))
          : NotificationListener<ScrollNotification>(
              onNotification: (n) {
                if (n is UserScrollNotification) {
                  _follow = _scroll.position.pixels >= _scroll.position.maxScrollExtent - 40;
                }
                return false;
              },
              child: Directionality(
                textDirection: TextDirection.ltr,
                // 1.0.7: one SelectionArea over plain Text rows instead of a
                // SelectableText (a full text editor) per row: selection and
                // copy still work (across rows too), scrolling builds far
                // less per new row.
                child: SelectionArea(
                child: ListView.builder(
                  controller: _scroll,
                  padding: const EdgeInsets.fromLTRB(Gap.page, 0, Gap.page, Gap.xxl),
                  itemCount: _lines.length,
                  itemBuilder: (context, i) {
                    final l = _lines[i];
                    final t = DateTime.fromMillisecondsSinceEpoch((l['time'] as num).toInt());
                    final ts = '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}';
                    return Padding(
                      padding: const EdgeInsets.symmetric(vertical: 3),
                      child: Text.rich(TextSpan(children: [
                        TextSpan(text: '$ts ', style: const TextStyle(color: Palette.muted)),
                        TextSpan(text: '${l['level']}'.toUpperCase().padRight(5), style: TextStyle(color: _color('${l['level']}'), fontWeight: FontWeight.w700)),
                        TextSpan(text: ' ${l['msg']}', style: const TextStyle(color: Palette.text2)),
                      ]), style: const TextStyle(fontSize: 11.5, height: 1.45, fontFamily: 'monospace')),
                    );
                  },
                ),
                ),
              ),
            ),
    );
  }
}
