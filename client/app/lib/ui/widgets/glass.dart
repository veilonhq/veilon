import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../theme/palette.dart';

/// Frosted glass surface with a light-catching edge.
class Glass extends StatelessWidget {
  const Glass({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(Gap.l),
    this.radius = Radii.l,
    this.blur = 18,
    this.tint,
    this.glow,
    this.borderColor,
  });

  final Widget child;
  final EdgeInsetsGeometry padding;
  final double radius;
  final double blur;
  final Color? tint;
  final Color? glow;
  final Color? borderColor;

  static const _base = Color(0x2E0B0D14);

  @override
  Widget build(BuildContext context) {
    final r = BorderRadius.circular(radius);
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: r,
        boxShadow: [
          if (glow != null) BoxShadow(color: glow!.withValues(alpha: 0.22), blurRadius: 36, spreadRadius: -6),
          // No dark drop shadow: the card is translucent, so it would show
          // through as a hard inner rectangle.
        ],
      ),
      // No BackdropFilter: the background is a soft, constantly moving
      // aurora, so a live blur would be recomputed for every card on every
      // frame (the main source of jank) for almost no visual difference.
      // A translucent gradient over a slightly darker base looks the same.
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: r,
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [
              Color.alphaBlend((tint ?? Colors.white).withValues(alpha: tint == null ? 0.085 : 0.15), _base),
              Color.alphaBlend((tint ?? Colors.white).withValues(alpha: tint == null ? 0.03 : 0.055), _base),
            ],
          ),
          border: Border.all(color: borderColor ?? Palette.line, width: 1),
        ),
        child: Padding(padding: padding, child: child),
      ),
    );
  }
}

/// Fully opaque surface for bottom sheets: solid near-black, nothing behind
/// it shows through (no glass / transparency / blur).
class SolidSheet extends StatelessWidget {
  const SolidSheet({super.key, required this.child, this.padding = const EdgeInsets.all(Gap.l), this.radius = Radii.xl});

  final Widget child;
  final EdgeInsetsGeometry padding;
  final double radius;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: Palette.bg2,
        borderRadius: BorderRadius.circular(radius),
        border: Border.all(color: Palette.line, width: 1),
      ),
      padding: padding,
      child: child,
    );
  }
}

/// A pressable surface with scale + haptic micro-interaction.
class Pressable extends StatefulWidget {
  const Pressable({super.key, required this.child, this.onTap, this.onLongPress, this.scale = 0.97, this.haptic = true});
  final Widget child;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;
  final double scale;
  final bool haptic;

  @override
  State<Pressable> createState() => _PressableState();
}

class _PressableState extends State<Pressable> {
  bool _down = false;

  void _set(bool v) {
    if (_down != v) setState(() => _down = v);
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTapDown: widget.onTap == null ? null : (_) => _set(true),
      onTapUp: widget.onTap == null ? null : (_) => _set(false),
      onTapCancel: () => _set(false),
      onTap: widget.onTap == null
          ? null
          : () {
              if (widget.haptic) HapticFeedback.selectionClick();
              widget.onTap!();
            },
      onLongPress: widget.onLongPress == null
          ? null
          : () {
              HapticFeedback.mediumImpact();
              _set(false);
              widget.onLongPress!();
            },
      child: AnimatedScale(
        scale: _down ? widget.scale : 1,
        duration: const Duration(milliseconds: 140),
        curve: Curves.easeOutCubic,
        child: widget.child,
      ),
    );
  }
}

/// Small rounded label.
class Pill extends StatelessWidget {
  const Pill({super.key, required this.text, this.color = Palette.text2, this.filled = false, this.icon});
  final String text;
  final Color color;
  final bool filled;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: filled ? color.withValues(alpha: 0.16) : Colors.transparent,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        if (icon != null) ...[Icon(icon, size: 13, color: color), const SizedBox(width: 4)],
        Text(text, style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w600, height: 1.2)),
      ]),
    );
  }
}

/// Section title used across pages.
class SectionTitle extends StatelessWidget {
  const SectionTitle(this.text, {super.key, this.trailing});
  final String text;
  final Widget? trailing;
  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(Gap.xs, Gap.xl, Gap.xs, Gap.s),
      child: Row(children: [
        Container(width: 6, height: 6, decoration: const BoxDecoration(color: Palette.lime, shape: BoxShape.circle)),
        const SizedBox(width: Gap.s),
        Expanded(
          child: Text(text,
              style: const TextStyle(
                  color: Palette.text2, fontSize: 13, fontWeight: FontWeight.w700, letterSpacing: 0.4)),
        ),
        ?trailing,
      ]),
    );
  }
}

/// Round icon button with glass backing.
class GlassIconButton extends StatelessWidget {
  const GlassIconButton({super.key, required this.icon, this.onTap, this.color = Palette.text, this.busy = false, this.tooltip});
  final IconData icon;
  final VoidCallback? onTap;
  final Color color;
  final bool busy;
  final String? tooltip;

  @override
  Widget build(BuildContext context) {
    final btn = Pressable(
      onTap: busy ? null : onTap,
      scale: 0.9,
      child: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          color: Palette.glass,
          border: Border.all(color: Palette.line),
        ),
        child: Center(
          child: busy
              ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
              : Icon(icon, size: 20, color: color),
        ),
      ),
    );
    return tooltip == null ? btn : Tooltip(message: tooltip!, child: btn);
  }
}

/// Lime primary button.
class PrimaryButton extends StatelessWidget {
  const PrimaryButton({super.key, required this.label, this.onTap, this.icon, this.busy = false});
  final String label;
  final VoidCallback? onTap;
  final IconData? icon;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    return Pressable(
      onTap: busy ? null : onTap,
      child: Container(
        height: 52,
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(Radii.m),
          gradient: const LinearGradient(colors: [Color(0xFFD8FF6E), Palette.lime]),
          boxShadow: [BoxShadow(color: Palette.lime.withValues(alpha: 0.35), blurRadius: 24, spreadRadius: -6)],
        ),
        child: Center(
          child: busy
              ? const SizedBox(
                  width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.4, color: Palette.limeInk))
              : Row(mainAxisSize: MainAxisSize.min, children: [
                  if (icon != null) ...[Icon(icon, color: Palette.limeInk, size: 20), const SizedBox(width: Gap.s)],
                  Text(label,
                      style: const TextStyle(color: Palette.limeInk, fontWeight: FontWeight.w800, fontSize: 15)),
                ]),
        ),
      ),
    );
  }
}

void showToast(BuildContext context, String msg, {bool error = false, Duration? duration}) =>
    toastOn(ScaffoldMessenger.of(context), msg, error: error, duration: duration);

void toastOn(ScaffoldMessengerState m, String msg, {bool error = false, Duration? duration}) {
  m
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(
      content: Row(children: [
        Icon(error ? Icons.error_outline_rounded : Icons.check_circle_outline_rounded,
            color: error ? Palette.danger : Palette.lime, size: 20),
        const SizedBox(width: Gap.m),
        Expanded(child: Text(msg)),
      ]),
      duration: duration ?? Duration(milliseconds: error ? 4200 : 2400),
    ));
}
