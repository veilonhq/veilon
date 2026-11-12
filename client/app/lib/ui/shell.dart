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
import 'widgets/aurora.dart';

/// Motion tokens shared by the shell (200–350 ms, decelerating curves).
abstract final class Motion {
  static const page = Duration(milliseconds: 260);
  static const indicator = Duration(milliseconds: 300);
  static const curve = Curves.easeOutCubic;
}

/// App shell: one living aurora (its own repaint layer, never rebuilt by
/// page changes) behind a deck of pages kept alive in a stack. Tapping a tab
/// jumps straight to that page with a short cross-fade — never through the
/// pages in between. A horizontal fling moves to the adjacent page.
class Shell extends StatefulWidget {
  const Shell({super.key});

  static const pageCount = 4;

  static void goTo(BuildContext context, int page) => context.findAncestorStateOfType<_ShellState>()?.go(page);

  /// Current page index of the nearest shell (tests).
  static int? indexOf(BuildContext context) => context.findAncestorStateOfType<_ShellState>()?._index;

  @override
  State<Shell> createState() => _ShellState();
}

class _ShellState extends State<Shell> with SingleTickerProviderStateMixin {
  late final AnimationController _fade = AnimationController(vsync: this, duration: Motion.page, value: 1);
  int _index = 0;
  // Opacity of every page when the current fade started. A tab tap during
  // a running fade starts the new fade from what is on screen right now
  // (1.0.2 restarted from the front page at 1 and dropped the page that was
  // fading out: a visible jump on quick taps).
  List<double> _from = [1, 0, 0, 0];

  static const _pages = <Widget>[HomePage(), ServersPage(), StatsPage(), SettingsPage()];

  @override
  void dispose() {
    _fade.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    // When a fade ends, the pages that faded out go off stage (one rebuild
    // per switch, not one per frame).
    _fade.addStatusListener((st) {
      if (st == AnimationStatus.completed && mounted) {
        setState(() => _from = [for (var i = 0; i < Shell.pageCount; i++) i == _index ? 1.0 : 0.0]);
      }
    });
  }

  void go(int i) {
    if (i < 0 || i >= Shell.pageCount || i == _index) return;
    HapticFeedback.selectionClick();
    setState(() {
      _from = _Deck.opacities(_from, _index, _fade.value);
      _index = i;
    });
    _fade.forward(from: 0);
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
                  child: _Deck(fade: _fade, index: _index, from: _from, pages: _pages),
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
/// (and the one fading out) is painted, and only the front page's tickers
/// run (TickerMode), so hidden animations are paused.
///
/// 1.0.7: the fade runs in the render tree (one FadeTransition per page
/// driven by the shell's controller) instead of rebuilding the deck on every
/// frame of the switch; the deck itself rebuilds only when the target page
/// changes and once when the fade ends (to take the old page off stage).
class _Deck extends StatelessWidget {
  const _Deck({required this.fade, required this.index, required this.from, required this.pages});
  final AnimationController fade;
  final int index;
  final List<double> from;
  final List<Widget> pages;

  /// Page opacities at fade progress [v]: every page moves from its opacity
  /// when the fade started ([from]) towards 1 (front) or 0 (others), so a
  /// re-target mid-fade continues from the frame on screen. Pure
  /// cross-fade, no vertical offset (pages never move up or down).
  static List<double> opacities(List<double> from, int index, double v) {
    final t = Motion.curve.transform(v.clamp(0.0, 1.0));
    return [for (var i = 0; i < from.length; i++) from[i] + ((i == index ? 1.0 : 0.0) - from[i]) * t];
  }

  @override
  Widget build(BuildContext context) {
    return Stack(fit: StackFit.expand, children: [
      for (var i = 0; i < pages.length; i++)
        _Slot(
          key: ValueKey(i),
          front: i == index,
          visible: i == index || from[i] > 0.001,
          opacity: _SlotOpacity(fade, from[i], i == index ? 1.0 : 0.0),
          child: pages[i],
        ),
    ]);
  }
}

/// Opacity of one page during the fade: from → target along [Motion.curve]
/// (the same values as [_Deck.opacities]).
class _SlotOpacity extends Animation<double> with AnimationWithParentMixin<double> {
  _SlotOpacity(this.parent, this.from, this.to);
  @override
  final Animation<double> parent;
  final double from, to;

  @override
  double get value {
    if (from == to) return to;
    final t = Motion.curve.transform(parent.value.clamp(0.0, 1.0));
    return (from + (to - from) * t).clamp(0.0, 1.0);
  }
}

class _Slot extends StatelessWidget {
  const _Slot({super.key, required this.front, required this.visible, required this.opacity, required this.child});
  final bool front, visible;
  final Animation<double> opacity;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Offstage(
      offstage: !visible,
      child: TickerMode(
        enabled: front,
        child: IgnorePointer(
          ignoring: !front,
          child: ExcludeSemantics(
            excluding: !front,
            child: FadeTransition(
              key: ValueKey('deck-opacity-${(key as ValueKey<int>).value}'),
              opacity: opacity,
              child: RepaintBoundary(child: child),
            ),
          ),
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
              child: const IgnorePointer(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Palette.lime,
                    borderRadius: BorderRadius.all(Radius.circular(9)),
                    boxShadow: [BoxShadow(color: Color(0x99C6FF3D), blurRadius: 8)],
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
