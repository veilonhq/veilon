import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ALL_PROTOCOLS, inboundDefaults, portHint } from '../src/lib/protocols.ts';

test('no protocol pre-fills the port of a new inbound', () => {
  assert.equal(ALL_PROTOCOLS.length, 14);
  for (const p of ALL_PROTOCOLS) {
    assert.equal(inboundDefaults(p).port, 0, `${p} must start with an empty port`);
  }
});

test('port hints are placeholders only', () => {
  assert.equal(portHint('vless'), '443');
  assert.equal(portHint('tun'), '');
  for (const p of ALL_PROTOCOLS) assert.equal(typeof portHint(p), 'string');
});
