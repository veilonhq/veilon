import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/app_state.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../pages/profile_editor_page.dart';
import '../pages/qr_scan_page.dart';
import '../widgets/glass.dart';

/// True while the add sheet is open (the Servers page hides its "+" button).
final addSheetOpen = ValueNotifier<bool>(false);

Future<void> showAddSheet(BuildContext context) async {
  addSheetOpen.value = true;
  try {
    await showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      barrierColor: Palette.barrier,
      builder: (_) => const AddSheet(),
    );
  } finally {
    addSheetOpen.value = false;
  }
}

/// Shows the outcome of an import as a toast.
void reportImport(ScaffoldMessengerState m, S s, (int, bool, String?) r) {
  final (n, subAdded, err) = r;
  if (subAdded) {
    toastOn(m, s.t('add.subAdded'));
  } else if (n > 0 && err == 'warn:privateHost') {
    toastOn(m, s.f('add.addedPrivateHost', {'n': n}), error: true);
  } else if (err == 'err.slipnetRemoved' || err == 'err.typeRemoved') {
    toastOn(m, s.t(err!), error: true);
  } else if (err != null && err.startsWith('refused:')) {
    toastOn(m, s.f('sub.refused', {'e': err.substring(8)}), error: true);
  } else if (err != null) {
    toastOn(m, s.f('sub.failed', {'e': err}), error: true);
  } else if (n > 0) {
    toastOn(m, s.f('add.added', {'n': n}));
  } else {
    toastOn(m, s.t('add.nothing'), error: true);
  }
}

class AddSheet extends StatefulWidget {
  const AddSheet({super.key});
  @override
  State<AddSheet> createState() => _AddSheetState();
}

class _AddSheetState extends State<AddSheet> {
  final _url = TextEditingController();
  final _name = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _url.dispose();
    _name.dispose();
    super.dispose();
  }

  Future<void> _run(Future<(int, bool, String?)> Function() job) async {
    setState(() => _busy = true);
    final app = AppScope.read(context);
    final messenger = ScaffoldMessenger.of(context);
    final nav = Navigator.of(context);
    final s = S.of(context);
    final r = await job();
    if (!mounted) return;
    setState(() => _busy = false);
    final (n, sub, _) = r;
    if (sub || n > 0) {
      HapticFeedback.lightImpact();
      nav.pop();
    }
    reportImport(messenger, s, r);
    if (app.profiles.isNotEmpty && app.pings.isEmpty) {
      app.pingAll();
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final app = AppScope.read(context);
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SolidSheet(
        radius: Radii.xl,
        padding: const EdgeInsets.fromLTRB(Gap.page, Gap.m, Gap.page, Gap.xl),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Center(
            child: Container(
                width: 42, height: 4, decoration: BoxDecoration(color: Palette.line2, borderRadius: BorderRadius.circular(9))),
          ),
          const SizedBox(height: Gap.l),
          Text(s.t('add.title'), style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w900)),
          const SizedBox(height: Gap.l),
          TextField(
            controller: _url,
            keyboardType: TextInputType.url,
            textDirection: TextDirection.ltr,
            decoration: InputDecoration(
              hintText: s.t('add.subHint'),
              prefixIcon: Icon(Icons.link_rounded, color: Palette.muted),
            ),
          ),
          const SizedBox(height: Gap.s),
          TextField(
            controller: _name,
            decoration: InputDecoration(
                hintText: s.t('add.name'), prefixIcon: Icon(Icons.label_outline_rounded, color: Palette.muted)),
          ),
          const SizedBox(height: Gap.m),
          PrimaryButton(
            label: s.t('add.add'),
            icon: Icons.add_rounded,
            busy: _busy,
            onTap: () {
              final t = _url.text.trim();
              if (t.isEmpty) return;
              _run(() => app.importText(t, name: _name.text.trim()));
            },
          ),
          const SizedBox(height: Gap.l),
          Row(children: [
            Expanded(
              child: _Option(
                icon: Icons.content_paste_rounded,
                label: s.t('add.clipboard'),
                onTap: () async {
                  final data = await Clipboard.getData(Clipboard.kTextPlain);
                  final text = data?.text?.trim() ?? '';
                  if (!context.mounted) return;
                  if (text.isEmpty) {
                    showToast(context, s.t('add.clipEmpty'), error: true);
                    return;
                  }
                  _run(() => app.importText(text));
                },
              ),
            ),
            const SizedBox(width: Gap.s),
            Expanded(
              child: _Option(
                icon: Icons.qr_code_scanner_rounded,
                label: s.t('add.qr'),
                onTap: () async {
                  final code = await QrScanPage.open(context);
                  if (code != null && code.isNotEmpty) _run(() => app.importText(code));
                },
              ),
            ),
            const SizedBox(width: Gap.s),
            Expanded(
              child: _Option(
                icon: Icons.description_outlined,
                label: s.t('add.file'),
                onTap: () async {
                  final f = await Native.instance.pickConfFile();
                  if (f == null) return;
                  final text = f['text'] as String? ?? '';
                  final name = f['name'] as String? ?? '';
                  if (text.contains('[Interface]')) {
                    _run(() async {
                      final (n, err) = await app.importConf(text, name);
                      return (n, false, err);
                    });
                  } else {
                    _run(() => app.importText(text));
                  }
                },
              ),
            ),
          ]),
          const SizedBox(height: Gap.s),
          // Every Xray / sing-box protocol can be created by hand; AmneziaWG
          // only comes from its .conf file or pasted config text.
          _Option(
            key: const ValueKey('add-manual'),
            icon: Icons.edit_note_rounded,
            label: s.t('add.manual'),
            height: 56,
            onTap: () {
              final nav = Navigator.of(context);
              nav.pop();
              ProfileEditorPage.openNew(nav.context);
            },
          ),
          const SizedBox(height: Gap.s),
          Text(s.t('add.awgHint'),
              textAlign: TextAlign.center, style: TextStyle(color: Palette.muted, fontSize: 11.5, height: 1.4)),
        ]),
      ),
    );
  }
}

class _Option extends StatelessWidget {
  const _Option({super.key, required this.icon, required this.label, required this.onTap, this.height = 96});
  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final double height;

  @override
  Widget build(BuildContext context) {
    return Pressable(
      onTap: onTap,
      child: Container(
        height: height,
        padding: const EdgeInsets.all(Gap.s),
        decoration: BoxDecoration(
          color: Palette.glass,
          borderRadius: BorderRadius.circular(Radii.m),
          border: Border.all(color: Palette.line),
        ),
        child: height < 80
            ? Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                Icon(icon, color: Palette.lime, size: 24),
                const SizedBox(width: Gap.s),
                Text(label, maxLines: 1, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w700)),
              ])
            : Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                Icon(icon, color: Palette.lime, size: 26),
                const SizedBox(height: Gap.s),
                Text(label,
                    textAlign: TextAlign.center,
                    maxLines: 2,
                    style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600, height: 1.3)),
              ]),
      ),
    );
  }
}
