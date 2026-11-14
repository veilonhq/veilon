import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';

import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';

/// 1.0.10: QR scanner drawn in Flutter (same palette, fonts and buttons as
/// the rest of the app) over a native camera preview (ZXing BarcodeView in a
/// hybrid-composition platform view). A QR picked from the gallery is decoded
/// natively with the same ZXing library and returned exactly like a camera
/// scan. Pops with the decoded text, or null when closed.
class QrScanPage extends StatefulWidget {
  const QrScanPage({super.key, this.previewBuilder});

  /// Replaces the camera preview (tests: platform views do not render there).
  final WidgetBuilder? previewBuilder;

  /// Same as [previewBuilder] for pages opened through [open] (tests).
  @visibleForTesting
  static WidgetBuilder? debugPreview;

  static const viewType = 'app.veilon/qr-camera';

  /// Native → Dart: "scanned" (text) and "cameraError" (message).
  static const channel = MethodChannel('app.veilon/qr');

  static Future<String?> open(BuildContext context) {
    return Navigator.of(context, rootNavigator: true).push<String>(PageRouteBuilder<String>(
      opaque: true,
      transitionDuration: const Duration(milliseconds: 260),
      reverseTransitionDuration: const Duration(milliseconds: 200),
      pageBuilder: (_, _, _) => const QrScanPage(),
      transitionsBuilder: (_, a, _, child) => FadeTransition(
        opacity: CurvedAnimation(parent: a, curve: Curves.easeOutCubic),
        child: child,
      ),
    ));
  }

  @override
  State<QrScanPage> createState() => _QrScanPageState();
}

/// Camera state shown by the page.
enum QrCamera { checking, ready, denied, blocked, failed }

class _QrScanPageState extends State<QrScanPage> with WidgetsBindingObserver {
  QrCamera _camera = QrCamera.checking;
  bool _hasTorch = false;
  bool _torch = false;
  bool _picking = false;
  bool _done = false;
  String? _error;
  Timer? _errorTimer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    QrScanPage.channel.setMethodCallHandler(_onNative);
    // A toast left from an earlier action would sit over the gallery button;
    // the scanner shows its own messages in the hint pill.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.removeCurrentSnackBar();
    });
    _checkCamera(request: true);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    QrScanPage.channel.setMethodCallHandler(null);
    _errorTimer?.cancel();
    if (_torch) Native.instance.qrTorch(false);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    // Back from the system settings after allowing the camera there.
    if (s == AppLifecycleState.resumed && (_camera == QrCamera.blocked || _camera == QrCamera.denied)) {
      _checkCamera(request: false);
    }
  }

  Future<dynamic> _onNative(MethodCall call) async {
    if (!mounted) return null;
    switch (call.method) {
      case 'scanned':
        final text = (call.arguments as String?)?.trim() ?? '';
        if (text.isNotEmpty) _finish(text);
      case 'cameraError':
        setState(() {
          _camera = QrCamera.failed;
          _torch = false;
        });
    }
    return null;
  }

  Future<void> _checkCamera({required bool request}) async {
    final r = await Native.instance.cameraPermission(request: request);
    if (!mounted) return;
    final cam = switch (r) {
      'granted' => QrCamera.ready,
      'blocked' => QrCamera.blocked,
      _ => QrCamera.denied,
    };
    final torch = cam == QrCamera.ready && await Native.instance.qrHasTorch();
    if (!mounted) return;
    setState(() {
      _camera = cam;
      _hasTorch = torch;
    });
  }

  void _finish(String text) {
    if (_done || !mounted) return;
    _done = true;
    HapticFeedback.mediumImpact();
    Navigator.of(context).pop(text);
  }

  void _showError(String msg) {
    HapticFeedback.heavyImpact();
    _errorTimer?.cancel();
    setState(() => _error = msg);
    _errorTimer = Timer(const Duration(milliseconds: 4500), () {
      if (mounted) setState(() => _error = null);
    });
  }

  Future<void> _pickImage() async {
    if (_picking) return;
    final s = S.of(context);
    setState(() {
      _picking = true;
      _error = null;
    });
    final r = await Native.instance.pickQrImage();
    if (!mounted) return;
    setState(() => _picking = false);
    if (r == null) return; // cancelled
    final text = (r['text'] as String?)?.trim() ?? '';
    if (text.isNotEmpty) return _finish(text);
    _showError(s.t(r['error'] == 'unreadable' ? 'qr.unreadable' : 'qr.noQr'));
  }

  Future<void> _toggleTorch() async {
    final on = !_torch;
    setState(() => _torch = on);
    if (!await Native.instance.qrTorch(on) && mounted) setState(() => _torch = false);
  }

  Future<void> _cameraAction() async {
    if (_camera == QrCamera.blocked) {
      await Native.instance.openAppSettings();
    } else {
      await _checkCamera(request: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final mq = MediaQuery.of(context);
    final geo = QrGeometry.of(mq.size, mq.viewPadding, textScale: mq.textScaler.scale(13.5) / 13.5);
    final rtl = Directionality.of(context) == TextDirection.rtl;
    final live = _camera == QrCamera.ready;
    final animate = live && !mq.disableAnimations;

    Widget preview;
    if (!live) {
      preview = ColoredBox(color: Palette.bg);
    } else {
      preview = (widget.previewBuilder ?? QrScanPage.debugPreview)?.call(context) ?? const _CameraPreview();
    }

    final String hint;
    switch (_camera) {
      case QrCamera.denied || QrCamera.blocked:
        hint = s.t('qr.noCameraHint');
      case QrCamera.failed:
        hint = s.t('qr.cameraFailed');
      default:
        hint = s.t('qr.hint');
    }

    return Scaffold(
      backgroundColor: Palette.bg,
      resizeToAvoidBottomInset: false,
      body: Stack(children: [
        Positioned.fill(child: preview),
        Positioned.fill(
          child: IgnorePointer(
            child: _Overlay(rect: geo.cutout, animate: animate, live: live),
          ),
        ),
        if (!live && _camera != QrCamera.checking)
          Positioned.fromRect(
            rect: geo.cutout,
            child: _NoCamera(
              state: _camera,
              onAction: _camera == QrCamera.failed ? null : _cameraAction,
            ),
          ),
        // Top bar: close, title, torch (below the status bar / cutout).
        Positioned(
          top: geo.padding.top + Gap.s,
          left: geo.padding.left + Gap.l,
          right: geo.padding.right + Gap.l,
          height: QrGeometry.barHeight - Gap.s * 2,
          child: Row(children: [
            GlassIconButton(
              key: const ValueKey('qr-close'),
              icon: Icons.close_rounded,
              tooltip: s.t('qr.close'),
              onTap: () => Navigator.of(context).maybePop(),
            ),
            const SizedBox(width: Gap.m),
            Expanded(
              child: Text(s.t('add.qr'),
                  textAlign: TextAlign.center,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: Palette.text, fontSize: 17, fontWeight: FontWeight.w800)),
            ),
            const SizedBox(width: Gap.m),
            if (live && _hasTorch)
              _TorchButton(key: const ValueKey('qr-torch'), on: _torch, tooltip: s.t(_torch ? 'qr.torchOff' : 'qr.torchOn'), onTap: _toggleTorch)
            else
              const SizedBox(width: 44),
          ]),
        ),
        // Hint (or error) under the frame.
        if (geo.showHint || _error != null)
        Positioned(
          top: geo.showHint ? geo.cutout.bottom + QrGeometry.hintGap : geo.cutout.top + Gap.m,
          left: geo.padding.left + Gap.xl,
          right: geo.padding.right + Gap.xl,
          height: geo.showHint ? geo.hintHeight : QrGeometry.hintFor(1),
          child: Align(
            alignment: Alignment.topCenter,
            child: AnimatedSwitcher(
              duration: const Duration(milliseconds: 180),
              child: _HintPill(
                key: ValueKey(_error ?? hint),
                text: _error ?? hint,
                error: _error != null,
              ),
            ),
          ),
        ),
        // Gallery button above the navigation bar / gesture area.
        Positioned(
          left: geo.padding.left + Gap.page,
          right: geo.padding.right + Gap.page,
          bottom: geo.padding.bottom + QrGeometry.bottomGap,
          height: QrGeometry.buttonHeight,
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 360),
              child: _GalleryButton(
                key: const ValueKey('qr-gallery'),
                label: s.t(_picking ? 'qr.reading' : 'qr.gallery'),
                busy: _picking,
                rtl: rtl,
                onTap: _pickImage,
              ),
            ),
          ),
        ),
      ]),
    );
  }
}

/// Where everything sits, from the screen size and the system bar insets
/// (status bar, display cutout, navigation bar / gesture area). Pure so it
/// can be tested for every size.
@immutable
class QrGeometry {
  const QrGeometry(this.size, this.padding, this.cutout, this.hintHeight);

  static const barHeight = 64.0;
  static const buttonHeight = 52.0;
  static const bottomGap = 16.0;
  static const hintGap = 18.0;
  static const minSide = 150.0;
  static const maxSide = 300.0;

  /// Hint pill: up to three lines of 13.5 sp × 1.45 plus padding, scaled
  /// with the system font size.
  static double hintFor(double textScale) => 24 + 3 * 13.5 * 1.45 * textScale;

  final Size size;
  final EdgeInsets padding;
  final Rect cutout;

  /// 0 when there is no room for the hint (short landscape screens).
  final double hintHeight;

  bool get showHint => hintHeight > 0;

  /// Top of the gallery button.
  double get buttonTop => size.height - padding.bottom - bottomGap - buttonHeight;

  static QrGeometry of(Size size, EdgeInsets padding, {double textScale = 1}) {
    final top = padding.top + barHeight + Gap.s;
    final bottom = size.height - padding.bottom - bottomGap - buttonHeight - Gap.m;
    final w = size.width - padding.left - padding.right;
    var hint = hintFor(textScale);
    var avail = bottom - top - hintGap - hint;
    if (avail < minSide) {
      // Not enough height (landscape phone): frame only, no hint.
      hint = 0;
      avail = bottom - top;
    }
    final side = max(min(maxSide, min(w * 0.74, avail)), min(80.0, max(avail, 0.0)));
    // Centred on the screen when it fits (the native decoder looks at the
    // centre of the preview), pushed down / up just enough otherwise.
    final cx = padding.left + w / 2;
    var y = size.height / 2 - side / 2;
    final maxY = bottom - (hint > 0 ? hintGap + hint : 0) - side;
    if (y > maxY) y = maxY;
    if (y < top) y = top;
    return QrGeometry(size, padding, Rect.fromLTWH(cx - side / 2, y, side, side), hint);
  }
}

class _CameraPreview extends StatelessWidget {
  const _CameraPreview();

  @override
  Widget build(BuildContext context) {
    return PlatformViewLink(
      viewType: QrScanPage.viewType,
      surfaceFactory: (context, controller) => AndroidViewSurface(
        controller: controller as AndroidViewController,
        gestureRecognizers: const <Factory<OneSequenceGestureRecognizer>>{},
        hitTestBehavior: PlatformViewHitTestBehavior.transparent,
      ),
      onCreatePlatformView: (params) {
        // Hybrid composition: the camera view is a real Android view, so
        // its preview surface composes reliably with the Flutter overlay.
        return PlatformViewsService.initExpensiveAndroidView(
          id: params.id,
          viewType: QrScanPage.viewType,
          layoutDirection: TextDirection.ltr,
          creationParams: const <String, Object?>{},
          creationParamsCodec: const StandardMessageCodec(),
          onFocus: () => params.onFocusChanged(true),
        )
          ..addOnPlatformViewCreatedListener(params.onPlatformViewCreated)
          ..create();
      },
    );
  }
}

/// Dimmed veil with a rounded cut-out, corner accents and a scan line.
class _Overlay extends StatefulWidget {
  const _Overlay({required this.rect, required this.animate, required this.live});
  final Rect rect;
  final bool animate;
  final bool live;

  @override
  State<_Overlay> createState() => _OverlayState();
}

class _OverlayState extends State<_Overlay> with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 2400), value: 0.5);

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(_Overlay old) {
    super.didUpdateWidget(old);
    _sync();
  }

  void _sync() {
    if (widget.animate && !_c.isAnimating) {
      _c.repeat(reverse: true);
    } else if (!widget.animate && _c.isAnimating) {
      _c.stop();
      _c.value = 0.5;
    }
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: CustomPaint(
        painter: QrOverlayPainter(rect: widget.rect, scan: _c, showLine: widget.live),
        size: Size.infinite,
      ),
    );
  }
}

class QrOverlayPainter extends CustomPainter {
  QrOverlayPainter({required this.rect, required this.scan, required this.showLine}) : super(repaint: scan);
  final Rect rect;
  final Animation<double> scan;
  final bool showLine;

  static const radius = 28.0;

  @override
  void paint(Canvas canvas, Size size) {
    final light = Palette.isLight;
    final rr = RRect.fromRectAndRadius(rect, const Radius.circular(radius));
    final veil = light ? Palette.bg.withValues(alpha: 0.86) : const Color(0xC2050609);
    final full = Path()..addRect(Offset.zero & size);
    canvas.drawPath(Path.combine(PathOperation.difference, full, Path()..addRRect(rr)), Paint()..color = veil);

    // Hairline around the window.
    canvas.drawRRect(
        rr,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1
          ..color = light ? Palette.text.withValues(alpha: 0.16) : Colors.white.withValues(alpha: 0.18));

    // Corner accents (lime), with a soft glow on dark.
    final accent = Palette.lime;
    final len = radius + min(22.0, rect.width * 0.09); // arm length from the corner
    final corners = Path();
    final l = rect.left, t = rect.top, r = rect.right, b = rect.bottom;
    const cr = radius;
    // top-left
    corners.moveTo(l, t + len);
    corners.lineTo(l, t + cr);
    corners.arcToPoint(Offset(l + cr, t), radius: const Radius.circular(cr));
    corners.lineTo(l + len, t);
    // top-right
    corners.moveTo(r - (len), t);
    corners.lineTo(r - cr, t);
    corners.arcToPoint(Offset(r, t + cr), radius: const Radius.circular(cr));
    corners.lineTo(r, t + len);
    // bottom-right
    corners.moveTo(r, b - (len));
    corners.lineTo(r, b - cr);
    corners.arcToPoint(Offset(r - cr, b), radius: const Radius.circular(cr));
    corners.lineTo(r - (len), b);
    // bottom-left
    corners.moveTo(l + len, b);
    corners.lineTo(l + cr, b);
    corners.arcToPoint(Offset(l, b - cr), radius: const Radius.circular(cr));
    corners.lineTo(l, b - (len));

    if (!light) {
      canvas.drawPath(
          corners,
          Paint()
            ..style = PaintingStyle.stroke
            ..strokeWidth = 10
            ..strokeCap = StrokeCap.round
            ..color = accent.withValues(alpha: 0.16)
            ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 6));
    }
    canvas.drawPath(
        corners,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 4.5
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round
          ..color = accent);

    if (!showLine) return;
    // Scan line: a thin lime bar with a faint trail, moving inside the window.
    final inset = 18.0;
    final p = Curves.easeInOut.transform(scan.value);
    final y = rect.top + inset + (rect.height - inset * 2) * p;
    canvas.save();
    canvas.clipRRect(rr);
    final trailDown = scan.status != AnimationStatus.reverse;
    final trail = Rect.fromLTRB(rect.left + inset, trailDown ? y - 36 : y, rect.right - inset, trailDown ? y : y + 36);
    canvas.drawRect(
        trail,
        Paint()
          ..shader = LinearGradient(
            begin: trailDown ? Alignment.topCenter : Alignment.bottomCenter,
            end: trailDown ? Alignment.bottomCenter : Alignment.topCenter,
            colors: [accent.withValues(alpha: 0), accent.withValues(alpha: light ? 0.10 : 0.16)],
          ).createShader(trail));
    final line = Rect.fromLTRB(rect.left + inset, y - 1, rect.right - inset, y + 1);
    canvas.drawRRect(
        RRect.fromRectAndRadius(line, const Radius.circular(1)),
        Paint()
          ..shader = LinearGradient(colors: [
            accent.withValues(alpha: 0),
            accent.withValues(alpha: 0.95),
            accent.withValues(alpha: 0),
          ]).createShader(line));
    canvas.restore();
  }

  @override
  bool shouldRepaint(QrOverlayPainter old) => old.rect != rect || old.showLine != showLine || old.scan != scan;
}

class _HintPill extends StatelessWidget {
  const _HintPill({super.key, required this.text, required this.error});
  final String text;
  final bool error;

  @override
  Widget build(BuildContext context) {
    final light = Palette.isLight;
    final c = error ? Palette.danger : Palette.text;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: Gap.l, vertical: 10),
      decoration: BoxDecoration(
        color: error
            ? Color.alphaBlend(Palette.danger.withValues(alpha: light ? 0.10 : 0.16), light ? Palette.bg2 : const Color(0xE60D0F16))
            : (light ? Palette.bg2.withValues(alpha: 0.92) : const Color(0xB30D0F16)),
        borderRadius: BorderRadius.circular(Radii.m),
        border: Border.all(color: error ? Palette.danger.withValues(alpha: 0.45) : Palette.line),
      ),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        if (error) ...[Icon(Icons.error_outline_rounded, size: 18, color: c), const SizedBox(width: Gap.s)],
        Flexible(
          child: Text(text,
              textAlign: TextAlign.center,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: c, fontSize: 13.5, height: 1.45, fontWeight: FontWeight.w600)),
        ),
      ]),
    );
  }
}

class _TorchButton extends StatelessWidget {
  const _TorchButton({super.key, required this.on, required this.onTap, required this.tooltip});
  final bool on;
  final VoidCallback onTap;
  final String tooltip;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Pressable(
        onTap: onTap,
        scale: 0.9,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 180),
          width: 44,
          height: 44,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: on ? Palette.limeFill : Palette.glass,
            border: Border.all(color: on ? Palette.limeFill : Palette.line),
            boxShadow: on ? [BoxShadow(color: Palette.limeFill.withValues(alpha: 0.4), blurRadius: 18, spreadRadius: -4)] : null,
          ),
          child: Icon(on ? Icons.flashlight_on_rounded : Icons.flashlight_off_rounded,
              size: 20, color: on ? Palette.limeInk : Palette.text),
        ),
      ),
    );
  }
}

class _GalleryButton extends StatelessWidget {
  const _GalleryButton({super.key, required this.label, required this.busy, required this.rtl, required this.onTap});
  final String label;
  final bool busy;
  final bool rtl;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final light = Palette.isLight;
    return Pressable(
      onTap: busy ? null : onTap,
      child: Container(
        height: QrGeometry.buttonHeight,
        padding: const EdgeInsets.symmetric(horizontal: Gap.l),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(Radii.m),
          color: light ? Palette.bg2 : const Color(0xCC0D0F16),
          border: Border.all(color: light ? Palette.line2 : Palette.line2),
          boxShadow: light ? [BoxShadow(color: Palette.shadow, blurRadius: 18, spreadRadius: -6, offset: const Offset(0, 6))] : null,
        ),
        child: Row(mainAxisSize: MainAxisSize.min, mainAxisAlignment: MainAxisAlignment.center, children: [
          SizedBox(
            width: 22,
            height: 22,
            child: busy
                ? Padding(padding: const EdgeInsets.all(2), child: CircularProgressIndicator(strokeWidth: 2.2, color: Palette.lime))
                : Icon(Icons.photo_library_rounded, size: 22, color: Palette.lime),
          ),
          const SizedBox(width: Gap.m),
          Flexible(
            child: Text(label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Palette.text, fontSize: 15, fontWeight: FontWeight.w700)),
          ),
        ]),
      ),
    );
  }
}

/// Inside the window when the camera cannot be used: an icon and, when the
/// permission can still be given, a button.
class _NoCamera extends StatelessWidget {
  const _NoCamera({required this.state, required this.onAction});
  final QrCamera state;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    return ClipRRect(
      borderRadius: BorderRadius.circular(QrOverlayPainter.radius),
      child: ColoredBox(
        color: Palette.isLight ? Palette.bg3 : Palette.bg3.withValues(alpha: 0.9),
        child: Padding(
          padding: const EdgeInsets.all(Gap.l),
          child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
            Icon(Icons.no_photography_rounded, size: 40, color: Palette.muted),
            const SizedBox(height: Gap.s),
            Text(s.t(state == QrCamera.failed ? 'qr.cameraFailedTitle' : 'qr.noCamera'),
                textAlign: TextAlign.center,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: Palette.text, fontSize: 14, fontWeight: FontWeight.w700, height: 1.4)),
            if (onAction != null) ...[
              const SizedBox(height: Gap.m),
              Pressable(
                key: const ValueKey('qr-camera-action'),
                onTap: onAction,
                child: Container(
                  height: 40,
                  padding: const EdgeInsets.symmetric(horizontal: Gap.l),
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(99),
                    gradient: LinearGradient(colors: [Palette.limeFill2, Palette.limeFill]),
                  ),
                  child: Center(
                    widthFactor: 1,
                    child: Text(s.t(state == QrCamera.blocked ? 'qr.openSettings' : 'qr.allow'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: Palette.limeInk, fontSize: 13.5, fontWeight: FontWeight.w800)),
                  ),
                ),
              ),
            ],
          ]),
        ),
      ),
    );
  }
}
