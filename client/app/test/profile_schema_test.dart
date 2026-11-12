import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:veilon/core/profile_schema.dart';

import 'helpers.dart';

void main() {
  test('every type has a general group with host override', () {
    for (final t in profileTypes) {
      final g = groupsFor(t);
      expect(g.first.id, 'general', reason: t);
      expect(g.first.fields.map((f) => f.path), contains('addressOverride'), reason: t);
    }
  });

  test('round trip keeps unknown keys and nested values', () {
    final j = wireguard();
    final out = applyValues(j, valuesOf(j, 'amneziawg'));
    expect(out['wg']['address'], ['10.8.0.2/32', 'fd00::2/128']);
    expect(out['wg']['reserved'], [1, 2, 3]);
    expect(out['wg']['awg']['jc'], 4);
    expect(connectionChanged(j, out), isFalse);

    final v = vlessReality();
    final o2 = applyValues(v, valuesOf(v, 'vless'));
    expect(o2['futureField'], {'keep': true});
    expect(o2['pbk'], v['pbk']);
    expect(connectionChanged(v, o2), isFalse);
  });

  test('edits are applied, empty optional fields removed', () {
    final v = vlessReality();
    final vals = valuesOf(v, 'vless')
      ..['port'] = '8443'
      ..['addressOverride'] = '1.2.3.4'
      ..['flow'] = ''
      ..['alpn'] = 'h2, http/1.1';
    final o = applyValues(v, vals);
    expect(o['port'], 8443);
    expect(o['addressOverride'], '1.2.3.4');
    expect(o.containsKey('flow'), isFalse);
    expect(o['alpn'], ['h2', 'http/1.1']);
    expect(connectionChanged(v, o), isTrue);
    // A host override alone does not invalidate the share link.
    final o3 = applyValues(v, valuesOf(v, 'vless')..['addressOverride'] = '1.2.3.4');
    expect(connectionChanged(v, o3), isFalse);
  });

  test('validation catches bad values', () {
    final vals = valuesOf(vlessReality(), 'vless')
      ..['port'] = '70000'
      ..['uuid'] = ''
      ..['pbk'] = 'short'
      ..['sid'] = 'xyz'
      ..['addressOverride'] = 'http://bad';
    final e = validateAll(vals);
    expect(e.keys, containsAll(['port', 'uuid', 'pbk', 'sid', 'addressOverride']));
    expect(errorText(e['port']!, false), contains('65535'));
    expect(errorText(e['uuid']!, true), 'الزامی است');
  });

  test('wireguard validation', () {
    final w = valuesOf(wireguard(), 'amneziawg')..['wg.privateKey'] = 'nope';
    expect(validateAll(w).keys, contains('wg.privateKey'));
    final w2 = valuesOf(wireguard(), 'amneziawg')..['wg.address'] = '10.0.0.2';
    expect(validateAll(w2).keys, contains('wg.address'));
    expect(validateAll(valuesOf(wireguard(), 'amneziawg')), isEmpty);

  });

  test('removed types are not offered; AmneziaWG is not created by hand', () {
    for (final t in ['ssh', 'dnstt', 'noizdns', 'vaydns', 'slipstream', 'soar', 'naive']) {
      expect(profileTypes, isNot(contains(t)));
      expect(manualTypes, isNot(contains(t)));
    }
    expect(manualTypes, isNot(contains('amneziawg')));
    expect(manualTypes, containsAll(['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'http', 'socks']));
    // Generic SOCKS5 stays (standard Xray / sing-box protocol): auth + TLS.
    expect(groupsFor('socks').map((g) => g.id), containsAll(['general', 'auth', 'tls']));
    for (final t in manualTypes) {
      expect(profileTypes, contains(t));
    }
  });

  test('reality fields only validated when security is reality', () {
    final vals = valuesOf(vlessReality(), 'vless')
      ..['security'] = 'tls'
      ..['pbk'] = 'garbage';
    expect(validateAll(vals), isEmpty);
  });

  test('validators', () {
    expect(checkHost('1.2.3.4'), isNull);
    expect(checkHost('cdn.example.com'), isNull);
    expect(checkHost('2001:db8::1'), isNull);
    expect(checkHost('a b'), isNotNull);
    expect(checkHostPort('[2001:db8::1]:53'), isNull);
    expect(checkHostPort('8.8.8.8:99999'), isNotNull);
    expect(checkCidr('10.0.0.1/33'), isNotNull);
    expect(checkPorts('443,20000-30000'), isNull);
    expect(checkPorts('0-5'), isNotNull);
    expect(checkOverride('1.2.3.4'), isNull);
    expect(checkOverride('1.2.3.4:8443'), isNull);
    expect(checkOverride('[2001:db8::1]:443'), isNull);
    expect(checkOverride('[2001:db8::1]'), isNull);
    expect(checkOverride('cdn.example.com:99999'), isNotNull);
    expect(checkOverride('https://x'), isNotNull);
  });

  group('transports', () {
    bool shown(Vals v, String path) => groupsFor(v['type']!).where((g) => g.shown(v)).expand((g) => g.fields).any((f) => f.path == path && f.shown(v));

    test('every transport offered; xdrive only for vless', () {
      final vless = groupsFor('vless').expand((g) => g.fields).firstWhere((f) => f.path == 'network');
      expect(vless.choices, containsAll(['tcp', 'kcp', 'ws', 'grpc', 'httpupgrade', 'xhttp', 'xdrive']));
      final vmess = groupsFor('vmess').expand((g) => g.fields).firstWhere((f) => f.path == 'network');
      expect(vmess.choices, isNot(contains('xdrive')));
    });

    test('only the chosen transport\'s fields are shown', () {
      final v = valuesOf(vlessReality(), 'vless');
      expect(shown(v, 'headerType'), isTrue);
      expect(shown(v, 'path'), isFalse, reason: 'RAW without http header');
      v['headerType'] = 'http';
      expect(shown(v, 'path'), isTrue);
      v['network'] = 'kcp';
      onNetworkChanged(v);
      expect(v['headerType'], '', reason: 'http is not an mKCP header');
      expect(shown(v, 'seed'), isTrue);
      expect(shown(v, 'kcp.tti'), isTrue);
      expect(shown(v, 'finalMask.udp'), isTrue);
      expect(shown(v, 'finalMask.tcp'), isFalse);
      expect(shown(v, 'serviceName'), isFalse);
      v['network'] = 'grpc';
      expect(shown(v, 'serviceName'), isTrue);
      expect(shown(v, 'kcp.tti'), isFalse);
      expect(shown(v, 'finalMask.tcp'), isTrue);
      v['network'] = 'xdrive';
      expect(shown(v, 'xdrive.remoteFolder'), isTrue);
      expect(shown(v, 'xdrive.s3.bucket'), isFalse);
      v['xdrive.service'] = 's3';
      expect(shown(v, 'xdrive.s3.bucket'), isTrue);
      expect(shown(v, 'sockopt.tcpFastOpen'), isTrue);
    });

    test('masks, kcp, sockopt and mux round trip (raw JSON strings)', () {
      final j = vlessReality()
        ..['network'] = 'kcp'
        ..['seed'] = 's3cret'
        ..['kcp'] = {'mtu': 1350, 'tti': 20}
        ..['finalMask'] = '{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200"}}],"udp":[{"type":"mkcp-legacy"}],"quicParams":{"x":1}}'
        ..['sockopt'] = '{"tcpFastOpen":true,"mark":255,"happyEyeballs":{"tryDelayMs":250}}'
        ..['mux'] = '{"enabled":true,"concurrency":8}';
      final vals = valuesOf(j, 'vless');
      expect(vals['sockopt.mark'], '255');
      expect(vals['mux.enabled'], 'true');
      expect(validateAll(vals), isEmpty);
      final out = applyValues(j, vals);
      expect(connectionChanged(j, out), isFalse);
      expect(out['finalMask'], isA<String>());
      expect(out['sockopt'], isA<String>());
      final fm = jsonDecode(out['finalMask'] as String) as Map;
      expect(fm['tcp'][0]['settings']['length'], '100-200');
      expect(fm['quicParams'], {'x': 1}, reason: 'keys outside the form are kept');
      expect((jsonDecode(out['sockopt'] as String) as Map)['happyEyeballs'], {'tryDelayMs': 250});
      expect(out['kcp']['tti'], 20);

      final masks = decodeMaskVals(vals['finalMask.tcp']!)!..add({'type': 'sudoku', 'settings': '{"password":"x"}'});
      vals['finalMask.tcp'] = encodeMaskVals(masks);
      final o2 = applyValues(j, vals);
      final fm2 = jsonDecode(o2['finalMask'] as String) as Map;
      expect((fm2['tcp'] as List).length, 2);
      expect(fm2['tcp'][1], {'type': 'sudoku', 'settings': {'password': 'x'}});

      // Clearing every sockopt field drops the key entirely.
      final v3 = valuesOf(vlessReality()..['sockopt'] = '{"mark":1}', 'vless')..['sockopt.mark'] = '';
      expect(applyValues(vlessReality()..['sockopt'] = '{"mark":1}', v3).containsKey('sockopt'), isFalse);

      // Invalid raw string is left untouched (only editable as raw JSON).
      final bad = vlessReality()..['mux'] = 'not json';
      expect(applyValues(bad, valuesOf(bad, 'vless'))['mux'], 'not json');

      vals['network'] = 'tcp';
      vals['finalMask.tcp'] = encodeMaskVals([{'type': '', 'settings': ''}]);
      expect(validateAll(vals).keys, contains('finalMask.tcp'));
      vals['finalMask.tcp'] = encodeMaskVals([{'type': 'fragment', 'settings': '{bad'}]);
      expect(validateAll(vals).keys, contains('finalMask.tcp'));
    });
  });
}
