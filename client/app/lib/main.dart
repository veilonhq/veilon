import 'dart:async';
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'core/app_state.dart';
import 'l10n/strings.dart';
import 'theme/palette.dart';
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
  SystemChrome.setSystemUIOverlayStyle(const SystemUiOverlayStyle(
    statusBarColor: Colors.transparent,
    systemNavigationBarColor: Colors.transparent,
    statusBarIconBrightness: Brightness.light,
    systemNavigationBarIconBrightness: Brightness.light,
  ));
  final state = AppState();
  try {
    await state.init();
  } catch (e, st) {
    // Start with whatever loaded rather than not at all.
    _note(e, st);
  }
  runApp(VeilonApp(state: state));
}

class VeilonApp extends StatelessWidget {
  const VeilonApp({super.key, required this.state});
  final AppState state;

  @override
  Widget build(BuildContext context) {
    return AppScope(
      state: state,
      child: Builder(builder: (context) {
        final lang = AppScope.of(context).settings.language;
        final s = S(lang);
        return SScope(
          s: s,
          child: MaterialApp(
            title: 'Veilon',
            debugShowCheckedModeBanner: false,
            theme: buildTheme(),
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
        );
      }),
    );
  }
}
