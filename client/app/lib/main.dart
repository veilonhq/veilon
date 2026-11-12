import 'dart:async';
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'core/app_state.dart';
import 'l10n/strings.dart';
import 'theme/palette.dart';
import 'theme/theme_host.dart';
import 'ui/shell.dart';

/// 1.0.6: no Dart error may take the app down. Framework errors (build /
/// layout / paint) are logged and the broken widget shows an empty box;
/// uncaught async errors (zone + platform dispatcher) are logged and
/// swallowed. Recent errors are kept in [recentErrors] for the log page.
final recentErrors = <String>[];

void _note(Object error, StackTrace? stack) {
  final line = '$error'.split('\n').first;
  recentErrors.add(line);
  if (recentErrors.length > 50) recentErrors.removeAt(0);
  debugPrint('veilon: contained error: $error\n${stack ?? ''}');
}

void installErrorGuards() {
  final previous = FlutterError.onError;
  FlutterError.onError = (details) {
    _note(details.exception, details.stack);
    if (kDebugMode && previous != null) previous(details);
  };
  PlatformDispatcher.instance.onError = (error, stack) {
    _note(error, stack);
    return true; // handled: never fatal
  };
  if (!kDebugMode) ErrorWidget.builder = (_) => const SizedBox.shrink();
}

Future<void> main() async {
  runZonedGuarded(_main, _note);
}

Future<void> _main() async {
  WidgetsFlutterBinding.ensureInitialized();
  installErrorGuards();
  SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
  final state = AppState();
  try {
    await state.init();
  } catch (e, st) {
    // Start with whatever loaded rather than not at all.
    _note(e, st);
  }
  // 1.0.8: the saved theme is applied before the first frame (no dark
  // frame for light users, and the status bar icons match from the start).
  final palette = paletteFor(state.settings.theme, PlatformDispatcher.instance.platformBrightness);
  Palette.use(palette);
  SystemChrome.setSystemUIOverlayStyle(systemBarsFor(palette));
  runApp(VeilonApp(state: state));
}

class VeilonApp extends StatelessWidget {
  const VeilonApp({super.key, required this.state});
  final AppState state;

  @override
  Widget build(BuildContext context) {
    return AppScope(
      state: state,
      // 1.0.7: rebuild the MaterialApp only when the language changes. It
      // used to depend on the whole app state, so every state change (and,
      // while connected, every 1 s stats tick) rebuilt it with a fresh
      // ThemeData; AnimatedTheme then re-ran its theme animation and every
      // Theme dependant (all four pages, each Scrollable, which also
      // replaced its ScrollPosition) rebuilt on every frame of it.
      child: AppSelector<(String, String)>(
        select: (a) => (a.settings.language, a.settings.theme),
        builder: (context, sel, _) {
        final (lang, theme) = sel;
        final s = S(lang);
        return ThemeHost(
          mode: theme,
          onPalette: (p) => state.nativeThemeChanged(theme),
          builder: (context, palette) => SScope(
          s: s,
          child: MaterialApp(
            title: 'Veilon',
            debugShowCheckedModeBanner: false,
            theme: themeFor(palette),
            // The theme switch is ThemeHost's cross-fade; MaterialApp must
            // not lerp ThemeData over extra frames on top of it.
            themeAnimationDuration: Duration.zero,
            scrollBehavior: const VeilonScrollBehavior(),
            locale: Locale(lang),
            supportedLocales: const [Locale('fa'), Locale('en')],
            localizationsDelegates: const [
              GlobalMaterialLocalizations.delegate,
              GlobalWidgetsLocalizations.delegate,
              GlobalCupertinoLocalizations.delegate,
            ],
            builder: (context, child) => SScope(s: s, child: child!),
            home: const Shell(),
          ),
        ),
        );
        },
      ),
    );
  }
}
