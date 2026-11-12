import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../theme/palette.dart';
import 'aurora.dart';
import 'glass.dart';

/// Scaffold for secondary pages: aurora, big title, glass back button.
class SubScaffold extends StatelessWidget {
  const SubScaffold({super.key, required this.title, required this.child, this.actions = const [], this.bottom});
  final String title;
  final Widget child;
  final List<Widget> actions;
  final Widget? bottom;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final rtl = Directionality.of(context) == TextDirection.rtl;
    return Scaffold(
      backgroundColor: Palette.bg,
      body: AuroraBackground(
        state: app.state,
        child: SafeArea(
          child: Column(children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Gap.page, Gap.m, Gap.page, Gap.s),
              child: Row(children: [
                GlassIconButton(
                  icon: rtl ? Icons.arrow_forward_rounded : Icons.arrow_back_rounded,
                  onTap: () => Navigator.of(context).maybePop(),
                ),
                const SizedBox(width: Gap.m),
                Expanded(
                  child: Text(title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w900)),
                ),
                ...actions,
              ]),
            ),
            Expanded(child: child),
            ?bottom,
          ]),
        ),
      ),
    );
  }
}
