import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { XRAY_MIN, versionAtLeast, xrayMissing, xrayNeeds, xrayServerWarnings } from '../src/lib/advanced.ts';
import { inboundDefaults } from '../src/lib/protocols.ts';

test('the version table mirrors the server (xrayneeds.go)', () => {
  const dir = new URL('../../panel/internal/inbounds/', import.meta.url);
  const go = readFileSync(new URL('xrayneeds.go', dir), 'utf8') + readFileSync(new URL('vlessenc.go', dir), 'utf8');
  const val = (name: string) => go.match(new RegExp(`${name}\\s*=\\s*"([^"]+)"`))?.[1];
  const pairs: [keyof typeof XRAY_MIN, string][] = [
    ['streamUpSecs', 'MinXrayStreamUpSecs'],
    ['realityLimit', 'MinXrayRealityLimit'],
    ['mldsa65', 'MinXrayMLDSA65'],
    ['ech', 'MinXrayECH'],
    ['reverse', 'MinXrayReverse'],
    ['hysteria', 'MinXrayHysteria'],
    ['masks', 'MinXrayMasks'],
    ['kcp', 'MinXrayKCP'],
    ['wsHeartbeat', 'MinXrayWSHeartbeat'],
    ['vlessEnc', 'MinVLESSEncXray'],
  ];
  for (const [k, g] of pairs) assert.equal(XRAY_MIN[k], val(g), k);
});

test('versions compare numerically; unknown is let through', () => {
  assert.equal(versionAtLeast('25.6.8', '25.7.26'), false);
  assert.equal(versionAtLeast('v25.10.15', '25.9.11'), true);
  assert.equal(versionAtLeast('26.10.31', '26.6.1'), true);
  assert.equal(versionAtLeast('25.7.26', '25.7.26'), true);
  assert.equal(versionAtLeast('', '26.6.1'), true);
  assert.equal(versionAtLeast(undefined, '26.6.1'), true);
});

test('needs follow the settings that are actually sent', () => {
  const base = inboundDefaults('vless');
  assert.deepEqual(xrayNeeds({ ...base, vless_encryption: null }), []);
  const pq = xrayNeeds({ ...base, vless_encryption: null, security: 'reality', advanced: { reality: { mldsa65_seed: 'x' } } });
  assert.deepEqual(pq.map((n) => n.feature), ['Reality ML-DSA-65']);
  // ML-DSA-65 under TLS is not a Reality setting, so it needs nothing.
  assert.deepEqual(xrayNeeds({ ...base, vless_encryption: null, security: 'tls', advanced: { reality: { mldsa65_seed: 'x' } } }), []);
  const many = xrayNeeds({ ...base, network: 'kcp', security: 'none', vless_encryption: { ...base.vless_encryption!, enabled: true }, advanced: { reverse: { enabled: true } } });
  assert.deepEqual(many.map((n) => n.min), ['26.6.1', '25.9.11', '25.9.5'], 'highest version first');
  assert.deepEqual(xrayNeeds({ ...inboundDefaults('tuic'), advanced: null }), []);
});

test('an old server: new needs block the save, needs it already had are only left out', () => {
  const needs = [
    { feature: 'Reality ML-DSA-65', min: '25.7.26' },
    { feature: 'VLESS reverse proxy', min: '25.9.11' },
  ];
  assert.equal(xrayMissing(needs, '26.10.31').length, 0);
  assert.equal(xrayMissing(needs, '').length, 0);
  const w = xrayServerWarnings(
    needs,
    [
      { id: 0, version: '26.10.31' },
      { id: 3, version: '25.6.8' },
      { id: 4, version: '' },
    ],
    [{ node_id: 3, needs: [needs[0]] }],
  );
  assert.equal(w.length, 1);
  assert.equal(w[0].id, 3);
  assert.deepEqual(w[0].block.map((n) => n.feature), ['VLESS reverse proxy']);
  assert.deepEqual(w[0].skip.map((n) => n.feature), ['Reality ML-DSA-65']);
});
