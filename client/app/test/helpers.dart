import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:veilon/core/native.dart';
import 'package:veilon/theme/theme_host.dart';
import 'package:veilon/ui/sheets/add_sheet.dart';

/// What goldens capture: the whole screen at device resolution. Since 1.0.8
/// the app sits in a [ThemeHost] whose RepaintBoundary is the nearest one
/// above MaterialApp (it would capture at 1x); starting at the host walks up
/// to the view (dpr). Tests that pump a bare MaterialApp keep using it.
Finder goldenRoot() {
  final host = find.byType(ThemeHost);
  return host.evaluate().isNotEmpty ? host : find.byType(MaterialApp);
}

const coreChannel = MethodChannel('app.veilon/core');
const eventsChannel = EventChannel('app.veilon/events');

/// Fixture profiles in the core's Profile JSON shape (core/API.md).
Map<String, dynamic> vlessReality() => {
      'id': 'p-vless',
      'name': 'Frankfurt · Reality',
      'type': 'vless',
      'server': 'de1.example.com',
      'port': 443,
      'uuid': '2f1c8a3e-6b4d-4c1a-9e2f-7a8b9c0d1e2f',
      'flow': 'xtls-rprx-vision',
      'encryption': 'none',
      'network': 'tcp',
      'security': 'reality',
      'sni': 'www.microsoft.com',
      'fp': 'chrome',
      'pbk': 'Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw',
      'sid': '6ba85179e30d4fc2',
      'link': 'vless://2f1c8a3e-6b4d-4c1a-9e2f-7a8b9c0d1e2f@de1.example.com:443?security=reality#x',
      'futureField': {'keep': true},
    };

Map<String, dynamic> wireguard() => {
      'id': 'p-wg',
      'name': 'WG Home',
      'type': 'amneziawg',
      'server': '203.0.113.7',
      'port': 51820,
      'wg': {
        'privateKey': 'yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=',
        'peerPublicKey': 'xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=',
        'address': ['10.8.0.2/32', 'fd00::2/128'],
        'mtu': 1280,
        'reserved': [1, 2, 3],
        'awg': {'jc': 4, 'jmin': 40, 'jmax': 70, 'h1': '1'},
      },
    };

Map<String, dynamic> dnstt() => {
      'id': 'p-dnstt',
      'name': 'DNSTT tunnel',
      'type': 'dnstt',
      'server': 't.example.com',
      'dns': {
        'domain': 't.example.com',
        'publicKey': '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
        'resolvers': ['8.8.8.8:53', '1.1.1.1:53'],
        'transport': 'udp',
        'backend': 'ssh',
        'payloadSize': 1232,
      },
      'ssh': {'host': '127.0.0.1', 'port': 22, 'user': 'veilon'},
    };

/// Records method calls and answers like a minimal native side.
final calls = <MethodCall>[];
Map<String, dynamic>? fakeSubResult;
String? normalizeError;
List<Map<String, dynamic>> fakeLogs = [];
MockStreamHandlerEventSink? eventSink;

/// Sends a native event and lets it reach the listeners: the mock sink's
/// stream was created outside the widget test's fake-async zone, so delivery
/// needs a real async turn.
Future<void> emitEvent(WidgetTester t, Map<String, Object?> event) async {
  eventSink!.success(event);
  await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
}
String? exportLink;
String? fakeParseText;
int fakeDelay = 97;

/// When set, 'fetchSub' waits for it (a hanging subscription server).
Completer<void>? fetchSubGate;

/// 1.0.8: when set, answers 'fetchSub' (args: url, device) instead of
/// [fakeSubResult]; [lastSubPins] is the last 'setSubPins' JSON.
Map<String, dynamic> Function(Map args)? fakeSubHandler;
String? lastSubPins;
List<Map<String, Object>>? fakeApps;

/// Icon bytes the mock returns for every 'appIcon' call (null = no icon).
Uint8List? fakeAppIcon;

/// ABI folders the mock reports for 'installedAbis'.
List<String> fakeAbis = const [];

/// 1.0.9 updater mocks: the update folder ('updateDir'), the native
/// 'sha256File' answer (null = the PlatformException 1.0.8 turned into a
/// fake checksum mismatch) and the 'installApk' answer.
String? fakeUpdateDir;
String? fakeNativeSha;
String fakeInstallResult = 'ok';

void installNativeMocks() {
  final m = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  m.setMockMethodCallHandler(coreChannel, (call) async {
    calls.add(call);
    switch (call.method) {
      case 'deviceInfo':
        return {'assetDir': '/data/rules', 'package': 'app.veilon', 'hwid': 'test', 'model': 'Test'};
      case 'state':
        return {'type': 'state', 'state': 'disconnected'};
      case 'checkProfile':
        return '';
      case 'resolveCore':
        final prof = jsonDecode((call.arguments as Map)['profile'] as String) as Map;
        final type = prof['type'];
        final core = type == 'amneziawg' ? 'amneziawg' : (type == 'dnstt' ? 'dns-tunnel' : (prof['core'] == 'xray' ? 'xray' : 'sing-box'));
        return jsonEncode({'core': core});
      case 'profileEdited':
        return false;
      case 'normalizeProfile':
        return normalizeError == null ? null : jsonEncode({'error': normalizeError});
      case 'profileToLink':
        return jsonEncode(exportLink == null ? {'error': 'unsupported'} : {'link': exportLink});
      case 'logs':
        final after = (call.arguments as Map)['after'] as int? ?? 0;
        return jsonEncode(fakeLogs.where((l) => (l['seq'] as int) > after).toList());
      case 'parseText':
        return fakeParseText ?? jsonEncode({'profiles': []});
      case 'urlTest':
        final prof = jsonDecode((call.arguments as Map)['profile'] as String) as Map;
        if (const {'amneziawg', 'wireguard', 'dnstt', 'noizdns', 'vaydns', 'slipstream'}.contains(prof['type'])) {
          throw StateError('urlTest must never be called for ${prof['type']}');
        }
        return jsonEncode({'delay': fakeDelay});
      case 'installedApps':
        return fakeApps;
      case 'appIcon':
        return fakeAppIcon;
      case 'installedAbis':
        return fakeAbis;
      case 'updateDir':
        return fakeUpdateDir;
      case 'sha256File':
        if (fakeNativeSha == null) throw PlatformException(code: 'native', message: 'file is not in the update folder');
        return fakeNativeSha;
      case 'installApk':
        return fakeInstallResult;
      case 'setSubPins':
        lastSubPins = (call.arguments as Map)['pins'] as String?;
        return true;
      case 'fetchSub':
        if (fetchSubGate != null) await fetchSubGate!.future;
        if (fakeSubHandler != null) return jsonEncode(fakeSubHandler!(call.arguments as Map));
        return jsonEncode(fakeSubResult ?? {'status': 200, 'profiles': []});
      default:
        return null;
    }
  });
  Native.instance.debugResetEvents();
  // A test that ends with the add sheet still open never runs its `finally`;
  // reset the global flag so the next test's Servers page shows its + button.
  addSheetOpen.value = false;
  m.setMockStreamHandler(eventsChannel, MockStreamHandler.inline(onListen: (args, sink) {
    eventSink = sink;
  }));
}

String flutterRoot() => Platform.environment['FLUTTER_ROOT'] ?? File(Platform.resolvedExecutable).parent.parent.parent.parent.parent.parent.path /* <flutter>/bin/cache/artifacts/engine/<host>/flutter_tester */;

/// Loads the app fonts (Vazirmatn, Sora) and Material icons so goldens show real glyphs.
Future<void> loadAppFonts() async {
  Future<void> load(String family, List<String> files) async {
    final l = FontLoader(family);
    for (final f in files) {
      final b = File(f).readAsBytesSync();
      l.addFont(Future.value(ByteData.view(b.buffer)));
    }
    await l.load();
  }

  const w = ['Light', 'Regular', 'Medium', 'SemiBold', 'Bold', 'ExtraBold', 'Black'];
  await load('Vazirmatn', [for (final x in w) 'assets/fonts/Vazirmatn-$x.ttf']);
  await load('Sora', ['assets/fonts/Sora.ttf']);
  await load('MaterialIcons', ['${flutterRoot()}/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf']);
  final mono = ['/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf'].where((f) => File(f).existsSync()).toList();
  await load('monospace', mono.isNotEmpty ? mono : ['assets/fonts/Vazirmatn-Regular.ttf']);
}

/// A SlipNet-imported SSH profile (slipnet:// link, runs on sing-box).
Map<String, dynamic> slipnetSsh() => {
      'id': 'p-slip-ssh',
      'name': 'SlipGate SSH',
      'type': 'ssh',
      'server': 'ssh.example.com',
      'port': 22,
      'link': 'slipnet://c3NofHgu',
      'ssh': {'host': 'ssh.example.com', 'port': 22, 'user': 'veilon'},
    };

Map<String, dynamic> trojan() => {
      'id': 'p-trojan',
      'name': 'Stockholm · Trojan',
      'type': 'trojan',
      'server': 'se1.example.com',
      'port': 443,
      'password': 'pw',
      'network': 'ws',
      'security': 'tls',
      'link': 'trojan://pw@se1.example.com:443?type=ws&security=tls#se',
    };
