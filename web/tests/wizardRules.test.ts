import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  ALL_PROTOCOLS,
  ENC_AUTHS,
  ENC_MODES,
  ENC_RTTS,
  GRPC_MODES,
  MASQUE_ALPNS,
  NETWORKS,
  REALITY_FPS,
  REALITY_NETWORKS,
  SS_METHODS,
  STLS_METHODS,
  TLS_ALPNS,
  TUIC_CONGESTIONS,
  TUNNEL_NETWORKS,
  XHTTP_MODES,
  emptyVlessEnc,
  flowAllowed,
  hasPort,
  networksOf,
  nodeCapable,
  securitiesOf,
  sniffingApplies,
  tlsRequired,
} from '../src/lib/protocols.ts';
import { TLS_FPS } from '../src/lib/advanced.ts';
import type { Protocol } from '../src/api/types.ts';

// The Go side writes this file (panel/internal/inbounds/rules.go,
// TestWizardRulesGolden); the bot wizard and the server use the same table.
const golden = JSON.parse(readFileSync(new URL('../../panel/internal/inbounds/testdata/wizard_rules.json', import.meta.url), 'utf8'));

const sorted = (l: string[]) => [...l].sort();

test('the web wizard offers the same transports, securities and Vision as the server and the bot', () => {
  assert.deepEqual(sorted(ALL_PROTOCOLS), sorted(golden.protocols));
  for (const p of ALL_PROTOCOLS as Protocol[]) {
    const nets = networksOf(p);
    const securities: Record<string, string[]> = {};
    const flow: string[] = [];
    for (const n of nets.length ? nets : ['' as const]) {
      securities[n] = securitiesOf(p, n);
      for (const sec of securitiesOf(p, n)) {
        for (const enc of [false, true]) {
          if (p !== 'vless' && enc) continue;
          const d = { protocol: p, network: (n || 'tcp') as never, security: sec, vless_encryption: { ...emptyVlessEnc(), enabled: enc } };
          if (n === '' && p !== 'vless') continue;
          if (flowAllowed(d)) flow.push(`${n}/${sec}${enc ? '/enc' : ''}`);
        }
      }
    }
    assert.deepEqual(
      { networks: nets, securities, flow, sniffing: sniffingApplies(p), port: hasPort(p), node_capable: nodeCapable(p), tls_required: tlsRequired(p) },
      golden.per[p],
      p,
    );
  }
});

test('the option lists match', () => {
  const o = golden.options;
  const pairs: [string, string[]][] = [
    ['networks', NETWORKS],
    ['reality_networks', REALITY_NETWORKS],
    ['ss_methods', SS_METHODS],
    ['shadowtls_ss', STLS_METHODS],
    ['xhttp_modes', XHTTP_MODES],
    ['grpc_modes', GRPC_MODES],
    ['tls_alpn', TLS_ALPNS],
    ['masque_alpn', MASQUE_ALPNS],
    ['tls_fingerprints', TLS_FPS],
    ['reality_fps', REALITY_FPS],
    ['tuic_congestion', TUIC_CONGESTIONS],
    ['tunnel_networks', TUNNEL_NETWORKS],
    ['enc_auths', ENC_AUTHS],
    ['enc_modes', ENC_MODES],
    ['enc_rtts', ENC_RTTS],
  ];
  for (const [k, v] of pairs) assert.deepEqual(sorted(v), sorted(o[k]), k);
  assert.deepEqual(Object.keys(o).sort(), pairs.map(([k]) => k).sort());
});
