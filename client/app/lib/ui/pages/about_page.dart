import 'package:flutter/material.dart';

import '../../core/app_state.dart';
import '../../core/native.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import '../widgets/sub_scaffold.dart';

class AboutPage extends StatefulWidget {
  const AboutPage({super.key});
  @override
  State<AboutPage> createState() => _AboutPageState();
}

class _AboutPageState extends State<AboutPage> {
  Map<String, dynamic> _v = {};

  @override
  void initState() {
    super.initState();
    Native.instance.versions().then((v) {
      if (mounted) setState(() => _v = v);
    });
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final app = AppScope.of(context);
    return SubScaffold(
      title: s.t('settings.about'),
      child: ListView(padding: const EdgeInsets.all(Gap.page), children: [
        Center(
          child: Column(children: [
            ShaderMask(
              shaderCallback: (r) => LinearGradient(colors: [Palette.lime, Palette.violetFg]).createShader(r),
              child: Text('Veilon',
                  style: TextStyle(fontFamily: Fonts.display, fontFamilyFallback: Fonts.fallback, fontSize: 44, fontWeight: FontWeight.w800, color: Palette.text, letterSpacing: -1.5)),
            ),
            const SizedBox(height: Gap.xs),
            Text(s.t('about.license'), style: TextStyle(color: Palette.muted)),
          ]),
        ),
        const SizedBox(height: Gap.xl),
        Glass(
          child: Column(children: [
            for (final (k, v) in [
              (s.t('about.version'), '${app.device['appVersion'] ?? ''}'),
              ('sing-box', '${_v['singbox'] ?? ''}'),
              ('Xray-core', '${_v['xray'] ?? ''}'),
              ('veilon-core', '${_v['veilon'] ?? ''}'),
            ])
              Padding(
                padding: const EdgeInsets.symmetric(vertical: Gap.s),
                child: Row(children: [
                  Text(k, style: TextStyle(color: Palette.muted)),
                  const Spacer(),
                  Text(v, textDirection: TextDirection.ltr, style: const TextStyle(fontWeight: FontWeight.w700)),
                ]),
              ),
          ]),
        ),
        const SizedBox(height: Gap.m),
        Pressable(
          onTap: () => Native.instance.openUrl('https://github.com/veilonhq/veilon'),
          child: Glass(
            child: Row(children: [
              Icon(Icons.code_rounded, color: Palette.lime),
              const SizedBox(width: Gap.m),
              Expanded(child: Text(s.t('about.source'), style: const TextStyle(fontWeight: FontWeight.w700))),
              Text('github.com/veilonhq/veilon', textDirection: TextDirection.ltr, style: TextStyle(color: Palette.muted, fontSize: 12)),
            ]),
          ),
        ),
        const SizedBox(height: Gap.l),
        Text(
          'Includes sing-box (GPL-3.0), Xray-core (MPL-2.0), amneziawg-go (MIT), Vazirmatn & Sora fonts (OFL-1.1).',
          textDirection: TextDirection.ltr,
          style: TextStyle(color: Palette.muted, fontSize: 12, height: 1.6),
        ),
      ]),
    );
  }
}
