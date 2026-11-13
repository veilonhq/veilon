import assert from 'node:assert/strict';
import { test } from 'node:test';
import { engineOf, inboundDefaults, isSingbox, nodeCapable, tlsRequired, transportsOf, withSettings } from '../src/lib/protocols.ts';

test('ShadowTLS, AnyTLS and NaiveProxy are main-only sing-box TCP protocols', () => {
  for (const p of ['shadowtls', 'anytls', 'naive'] as const) {
    assert.equal(engineOf(p), 'sing-box');
    assert.equal(isSingbox(p), true);
    assert.equal(nodeCapable(p), false);
    assert.deepEqual(transportsOf(inboundDefaults(p)), { tcp: true, udp: false });
  }
  assert.equal(tlsRequired('anytls'), true);
  assert.equal(tlsRequired('naive'), true);
  assert.equal(tlsRequired('shadowtls'), false);
});

test('defaults of the new protocols', () => {
  const st = inboundDefaults('shadowtls');
  assert.equal(st.settings.handshake_server, 'www.microsoft.com');
  assert.equal(st.settings.handshake_port, 443);
  assert.equal(st.ss_method, '2022-blake3-aes-128-gcm');
  assert.equal(st.security, 'none');
  assert.deepEqual(inboundDefaults('naive').tls?.alpn, ['h2', 'http/1.1']);
  assert.equal(inboundDefaults('anytls').security, 'tls');
  assert.equal(inboundDefaults('hysteria').settings.hop_ports, '');
});

test('settings from an older server get the new fields', () => {
  const s = withSettings({ obfs_password: 'x' } as never);
  assert.equal(s.hop_ports, '');
  assert.deepEqual(s.padding_scheme, []);
});
