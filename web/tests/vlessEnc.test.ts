import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ALL_PROTOCOLS, emptyVlessEnc, flowAllowed, inboundDefaults } from '../src/lib/protocols.ts';

test('a new VLESS inbound has VLESS Encryption off with the vlessenc defaults', () => {
  const e = inboundDefaults('vless').vless_encryption;
  assert.deepEqual(e, emptyVlessEnc());
  assert.equal(e?.enabled, false);
  assert.equal(e?.auth, 'x25519');
  assert.equal(e?.mode, 'native');
  assert.equal(e?.rtt, '0rtt');
  assert.equal(e?.ticket_seconds, 600);
  for (const p of ALL_PROTOCOLS) if (p !== 'vless') assert.ok(!inboundDefaults(p).vless_encryption, `${p} has no VLESS Encryption`);
});

test('Vision: VLESS with encryption on any transport, else tcp with TLS/Reality', () => {
  const on = { ...emptyVlessEnc(), enabled: true };
  for (const network of ['tcp', 'ws', 'grpc', 'httpupgrade', 'xhttp'] as const) {
    assert.equal(flowAllowed({ protocol: 'vless', network, security: 'none', vless_encryption: on }), true, network);
  }
  assert.equal(flowAllowed({ protocol: 'vless', network: 'tcp', security: 'none', vless_encryption: emptyVlessEnc() }), false);
  assert.equal(flowAllowed({ protocol: 'vless', network: 'ws', security: 'tls', vless_encryption: emptyVlessEnc() }), false);
  assert.equal(flowAllowed({ protocol: 'vless', network: 'tcp', security: 'reality', vless_encryption: null }), true);
  assert.equal(flowAllowed({ protocol: 'trojan', network: 'tcp', security: 'tls', vless_encryption: on }), false);
});
