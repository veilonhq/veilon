import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core/app_state.dart';
import '../core/models.dart';
import '../l10n/strings.dart';
import '../theme/palette.dart';
import 'pages/home_page.dart';
import 'pages/servers_page.dart';
import 'pages/settings_page.dart';
import 'pages/stats_page.dart';
import 'sheets/cert_dialog.dart';
import 'widgets/aurora.dart';

/// Motion tokens shared by the shell (200–350 ms, decelerating curves).
abstract final class Motion {
  /// Unused since 1.0.8 (tab switches are instant); kept for callers.
  static const page = Duration(milliseconds: 260);
  static const indicator = Duration(milliseconds: 300);
  static const curve = Curves.easeOutCubic;
}

/// App shell: one living aurora (its own repaint layer, never rebuilt by
/// page changes) behind a deck of pages kept alive in a stack. Tapping a tab
/// jumps straight to that page - never through the pages in between. A
/// horizontal fling moves to the adjacent page.
///
/// 1.0.8: the switch is instant. 1.0.7 cross-faded the two pages for 260 ms
/// with an opacity on each (= two full-screen offscreen layers per frame,
/// each re-rendering a whole page with its glows) over the aurora; on a
/// mid/low-end phone that is what made "Servers → Home" appear late and
/// stutter. Now the target page is simply put on stage: its layer tree is
/// kept from when it was last shown (RepaintBoundary, not repainted unless
/// something on it changed), so the first frame after the tap costs the
/// same as any other frame of that page.
class Shell extends StatefulWidget {
  const Shell({super.key});

  static const pageCount = 4;

  static void goTo(BuildContext context, int page) => context.findAncestorStateOfType<_ShellState>()?.go(page);

  /// Current page index of the nearest shell (tests).
  static int? indexOf(BuildContext context) => context.findAncestorStateOfType<_ShellState>()?._index;

  @override
  State<Shell> createState() => _ShellState();
}

class _ShellState extends State<Shell> {
  int _index = 0;

  static const _pages = <Widget>[HomePage(), ServersPage(), StatsPage(), SettingsPage()];

  @override
  void initState() {
    super.initState();
    // 1.0.8 (H1): certificate questions for literal-IP subscription servers.
    final app = AppScope.read(context);
    app.confirmCert = (server, fp, changed) async {
      if (!mounted) return false;
      return showCertDialog(context, server, fp, changed);
    };
  }

  void go(int i) {
    if (i < 0 || i >= Shell.pageCount || i == _index) return;
    HapticFeedback.selectionClick();
    setState(() => _index = i);
  }

  void _onFling(DragEndDetails d) {
    final v = d.primaryVelocity ?? 0;
    if (v.abs() < 350) return;
    final rtl = Directionality.of(context) == TextDirection.rtl;
    // Finger moving towards the reading-start side → next page.
    final forward = rtl ? v > 0 : v < 0;
    go(_index + (forward ? 1 : -1));
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final labels = [s.t('nav.home'), s.t('nav.servers'), s.t('nav.stats'), s.t('nav.settings')];
    return PopScope(
      canPop: _index == 0,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) go(0);
      },
      child: Scaffold(
        backgroundColor: Palette.bg,
        body: Stack(fit: StackFit.expand, children: [
          // Only the aurora depends on the connection state here; it lives in
          // its own repaint boundary so its frames never repaint the pages.
          const Positioned.fill(child: _ShellAurora()),
          SafeArea(
            bottom: false,
            child: Column(children: [
              TabRail(labels: labels, index: _index, onTap: go),
              Expanded(
                child: GestureDetector(
                  behavior: HitTestBehavior.translucent,
                  onHorizontalDragEnd: _onFling,
                  child: _Deck(index: _index, pages: _pages),
                ),
              ),
            ]),
          ),
        ]),
      ),
    );
  }
}

class _ShellAurora extends StatelessWidget {
  const _ShellAurora();
  // 1.0.7: rebuilds only when the connection state changes (it used to
  // rebuild on every app-state change).
  @override
  Widget build(BuildContext context) => AppSelector<ConnState>(
        select: (a) => a.state,
        builder: (context, st, _) => RepaintBoundary(child: AuroraBackground(state: st)),
      );
}

/// All pages stay mounted (state, scroll offsets kept); only the front page
/// is on stage, and only its tickers run (TickerMode), so hidden animations
/// are paused and hidden pages cost nothing per frame.
class _Deck extends StatelessWidget {
  const _Deck({required this.index, required this.pages});
  final int index;
  final List<Widget> pages;

  @override
  Widget build(BuildContext context) {
    return Stack(fit: StackFit.expand, children: [
      for (var i = 0; i < pages.length; i++) _Slot(key: ValueKey(i), front: i == index, child: pages[i]),
    ]);
  }
}

class _Slot extends StatelessWidget {
  const _Slot({super.key, required this.front, required this.child});
  final bool front;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Offstage(
      key: ValueKey('deck-page-${(key as ValueKey<int>).value}'),
      offstage: !front,
      child: TickerMode(
        enabled: front,
        child: ExcludeSemantics(
          excluding: !front,
          child: RepaintBoundary(child: child),
        ),
      ),
    );
  }
}

/// Evenly distributed tabs (equal width, centred labels, fixed font so no
/// width changes) with one indicator that slides straight to the target.
class TabRail extends StatelessWidget {
  const TabRail({super.key, required this.labels, required this.index, required this.onTap});
  final List<String> labels;
  final int index;
  final ValueChanged<int> onTap;

  static const _indicatorW = 22.0;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(Gap.page - Gap.s, Gap.s, Gap.page - Gap.s, Gap.xs),
      child: LayoutBuilder(builder: (context, c) {
        final w = c.maxWidth / labels.length;
        return SizedBox(
          height: 48,
          child: Stack(children: [
            Row(children: [
              for (var i = 0; i < labels.length; i++)
                Expanded(
                  child: Semantics(
                    selected: i == index,
                    button: true,
                    child: InkResponse(
                      key: ValueKey('tab-$i'),
                      onTap: () => onTap(i),
                      radius: w / 2,
                      highlightShape: BoxShape.rectangle,
                      containedInkWell: true,
                      borderRadius: BorderRadius.circular(Radii.s),
                      splashColor: Palette.lime.withValues(alpha: 0.06),
                      highlightColor: Colors.transparent,
                      child: Center(
                        child: AnimatedDefaultTextStyle(
                          duration: Motion.indicator,
                          curve: Motion.curve,
                          style: TextStyle(
                            fontFamily: Fonts.fa,
                            fontSize: 15,
                            height: 1.2,
                            fontWeight: FontWeight.w800,
                            color: i == index ? Palette.text : Palette.muted,
                          ),
                          child: Text(labels[i], maxLines: 1, overflow: TextOverflow.fade, softWrap: false),
                        ),
                      ),
                    ),
                  ),
                ),
            ]),
            AnimatedPositionedDirectional(
              duration: Motion.indicator,
              curve: Motion.curve,
              start: index * w + (w - _indicatorW) / 2,
              bottom: 2,
              width: _indicatorW,
              height: 3,
              child: IgnorePointer(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Palette.lime,
                    borderRadius: BorderRadius.all(Radius.circular(9)),
                    boxShadow: [BoxShadow(color: Palette.lime.withValues(alpha: 0.6), blurRadius: 8)],
                  ),
                ),
              ),
            ),
          ]),
        );
      }),
    );
  }
}
