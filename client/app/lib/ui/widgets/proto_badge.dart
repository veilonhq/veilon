import 'package:flutter/material.dart';

import '../../core/models.dart';
import '../../theme/palette.dart';

/// A small rounded-square badge carrying the protocol's monogram.
class ProtoBadge extends StatelessWidget {
  const ProtoBadge({super.key, required this.type, this.size = 40, this.active = false});
  final String type;
  final double size;
  final bool active;

  static (String, Color) look(String type) => switch (type) {
        'vless' => ('VL', Palette.lime),
        'vmess' => ('VM', Palette.info),
        'trojan' => ('TR', Palette.violetFg),
        'shadowsocks' => ('SS', Palette.warn),
        'hysteria2' => ('H2', Palette.isLight ? const Color(0xFFC2410C) : const Color(0xFFFF8A5C)),
        'tuic' => ('TU', Palette.isLight ? const Color(0xFF047857) : const Color(0xFF5CFFB8)),
        'wireguard' => ('WG', Palette.isLight ? const Color(0xFF3B5BDB) : const Color(0xFF88A6FF)),
        'amneziawg' => ('AW', Palette.isLight ? const Color(0xFF9A5B00) : const Color(0xFFFFD35C)),
        'http' => ('HT', Palette.text2),
        'socks' => ('S5', Palette.isLight ? const Color(0xFF5B6478) : const Color(0xFFB8C2D9)),
        'mtproto' => ('TG', Palette.isLight ? const Color(0xFF1D6FB8) : const Color(0xFF4FB3FF)),
        'masque' => ('MQ', Palette.isLight ? const Color(0xFFA82B8E) : const Color(0xFFFF7CE0)),
        _ => ('··', Palette.muted),
      };

  @override
  Widget build(BuildContext context) {
    final (mono, color) = look(type);
    return AnimatedContainer(
      duration: const Duration(milliseconds: 300),
      width: size,
      height: size,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(size * 0.32),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [color.withValues(alpha: active ? 0.36 : 0.2), color.withValues(alpha: 0.05)],
        ),
        border: Border.all(color: color.withValues(alpha: active ? 0.8 : 0.32)),
        boxShadow: active ? [BoxShadow(color: color.withValues(alpha: 0.35), blurRadius: 14)] : null,
      ),
      child: Center(
        child: Text(mono,
            textDirection: TextDirection.ltr,
            style: TextStyle(
                fontFamily: Fonts.display, fontFamilyFallback: Fonts.fallback,
                color: color,
                fontWeight: FontWeight.w800,
                fontSize: size * 0.34,
                letterSpacing: -0.5)),
      ),
    );
  }

  static String name(String type) => Profile.protocolName(type);
}
