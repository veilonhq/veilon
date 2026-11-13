import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applies, cleanAdvanced, fallbackDestOk, headerNameOk, rangeOk } from '../src/lib/advanced.ts';
import { inboundDefaults, transportsOf } from '../src/lib/protocols.ts';

test('a fresh inbound has no advanced settings and sends none', () => {
  for (const p of ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'tuic'] as const) {
    const d = inboundDefaults(p);
    assert.equal(cleanAdvanced(d), null, p);
    assert.equal(cleanAdvanced({ ...d, advanced: {} }), null, p);
  }
});

test('settings that do not apply are dropped like the server does', () => {
  const d = { ...inboundDefaults('vless'), network: 'ws' as const, security: 'tls' as const, sniffing: false };
  const a = cleanAdvanced({
    ...d,
    advanced: {
      fallbacks: [{ dest: '80' }],
      reality: { max_time_diff: 5 },
      kcp: { tti: 20 },
      sniffing: { route_only: true },
      ws: { heartbeat_period: 10, headers: { 'X-A': '1' } },
      tcp: { type: 'http' },
      sockopt: { accept_proxy_protocol: true },
    },
  });
  assert.deepEqual(Object.keys(a ?? {}).sort(), ['sockopt', 'ws']);
  // HTTPUpgrade keeps headers but has no heartbeat.
  const h = cleanAdvanced({ ...d, network: 'httpupgrade', advanced: { ws: { heartbeat_period: 10 } } });
  assert.equal(h, null);
  // Fallbacks: VLESS RAW + TLS, not with VLESS Encryption.
  const raw = { ...inboundDefaults('vless'), network: 'tcp' as const, security: 'tls' as const };
  assert.deepEqual(cleanAdvanced({ ...raw, advanced: { fallbacks: [{ dest: '80' }, { dest: ' ' }] } })?.fallbacks, [{ dest: '80' }]);
  assert.equal(cleanAdvanced({ ...raw, vless_encryption: { ...raw.vless_encryption!, enabled: true }, advanced: { fallbacks: [{ dest: '80' }] } }), null);
  // HTTP header obfuscation only without TLS.
  assert.equal(cleanAdvanced({ ...raw, advanced: { tcp: { type: 'http' } } }), null);
  assert.ok(cleanAdvanced({ ...raw, security: 'none', advanced: { tcp: { type: 'http', host: ['a.com'] } } })?.tcp);
  // Proxy protocol needs TCP: kept only on TCP transports.
  const kcp = { ...inboundDefaults('vmess'), network: 'kcp' as const };
  assert.equal(cleanAdvanced({ ...kcp, advanced: { sockopt: { accept_proxy_protocol: true } } }), null);
  assert.ok(applies(kcp).noise && !applies(kcp).fragment);
});

test('ranges, header names and fallback targets', () => {
  for (const ok of ['', '100', '100-1000', ' 5 - 9 ']) assert.equal(rangeOk(ok), true, ok);
  for (const bad of ['9-1', 'a', '1-2-3', '-5']) assert.equal(rangeOk(bad), false, bad);
  assert.equal(rangeOk('2000', 0, 1000), false);
  assert.equal(headerNameOk('X-Forwarded-For'), true);
  assert.equal(headerNameOk('Bad Name'), false);
  for (const ok of ['80', '127.0.0.1:8080', 'site.example.com:443', '/run/a.sock', '@abstract']) assert.equal(fallbackDestOk(ok), true, ok);
  for (const bad of ['', '0', '70000', 'nope', 'host:', 'a b:80']) assert.equal(fallbackDestOk(bad), false, bad);
});

test('listening sockets follow mKCP and the Shadowsocks UDP switch', () => {
  const ss = inboundDefaults('shadowsocks');
  assert.deepEqual(transportsOf(ss), { tcp: true, udp: true });
  assert.deepEqual(transportsOf({ ...ss, advanced: { ss: { no_udp: true } } }), { tcp: true, udp: false });
  assert.deepEqual(transportsOf({ ...ss, network: 'ws' }), { tcp: true, udp: false });
  assert.deepEqual(transportsOf({ ...ss, network: 'kcp' }), { tcp: false, udp: true });
  assert.deepEqual(transportsOf({ ...inboundDefaults('vless'), network: 'kcp' }), { tcp: false, udp: true });
});
