import { test } from 'node:test';
import assert from 'node:assert/strict';
import { portClash } from '../src/lib/portConflict.ts';

const nodes = [
  { id: 1, name: 'Lightsail', api_port: 62050 },
  { id: 2, name: 'Hetzner', api_port: 62051 },
];
const others = [
  { id: 10, remark: 'main443', port: 443, protocol: 'vless', serve_main: true, node_ids: [] },
  { id: 11, remark: 'n1-443', port: 443, protocol: 'vless', serve_main: false, node_ids: [1] },
  { id: 12, remark: 'tun', port: 8443, protocol: 'tun', serve_main: true, node_ids: [] },
];
const clash = (v: { port: number; serve_main?: boolean; node_ids?: number[]; protocol?: string; id?: number }, prev?: Parameters<typeof portClash>[4]) =>
  portClash({ protocol: 'vless', ...v }, others, nodes, [2053], prev);

test('ports are unique per server', () => {
  assert.deepEqual(clash({ port: 443, serve_main: true }), { server: 'main', other: 'main443' });
  assert.deepEqual(clash({ port: 443, serve_main: false, node_ids: [1] }), { server: 'node', nodeId: 1, name: 'Lightsail', other: 'n1-443' });
  assert.deepEqual(clash({ port: 443, serve_main: false, node_ids: [2, 1] }), { server: 'node', nodeId: 1, name: 'Lightsail', other: 'n1-443' });
  assert.equal(clash({ port: 443, serve_main: false, node_ids: [2] }), null);
  assert.equal(clash({ port: 8443, serve_main: true }), null, 'TUN has no port');
});

test('reserved ports', () => {
  assert.deepEqual(clash({ port: 2053, serve_main: true }), { server: 'main' });
  assert.equal(clash({ port: 2053, serve_main: false, node_ids: [1] }), null);
  assert.deepEqual(clash({ port: 62050, serve_main: false, node_ids: [1] }), { server: 'node', nodeId: 1, name: 'Lightsail' });
  assert.equal(clash({ port: 62050, serve_main: false, node_ids: [2] }), null);
});

test('edits re-check only what changed', () => {
  const prev = { id: 20, port: 443, protocol: 'vless', serve_main: false, node_ids: [2] };
  assert.equal(clash({ id: 20, port: 443, serve_main: false, node_ids: [2] }, prev), null);
  assert.equal(clash({ id: 20, port: 443, serve_main: false, node_ids: [1, 2] }, prev)?.server, 'node');
  assert.equal(clash({ id: 20, port: 443, serve_main: true, node_ids: [2] }, prev)?.server, 'main');
  // Stored before the rule: same port, same server is not re-checked.
  const legacy = { id: 21, port: 443, protocol: 'vless', serve_main: true, node_ids: [] };
  assert.equal(clash({ id: 21, port: 443, serve_main: true }, legacy), null);
});

import { hopClash, parseHopRanges } from '../src/lib/portConflict.ts';

test('hop ranges are locked per server like ports', () => {
  const hy = (id: number, remark: string, port: number, hop: string, serve_main = true, node_ids: number[] = [], listen = '') => ({ id, remark, port, protocol: 'hysteria', hop_ports: hop, udp: true, serve_main, node_ids, listen });
  const os = [hy(1, 'a', 8443, '20000-30000'), { id: 2, remark: 'wg', port: 40000, protocol: 'wireguard', udp: true, serve_main: true, node_ids: [] }, { id: 3, remark: 'vl', port: 45000, protocol: 'vless', udp: false, serve_main: true, node_ids: [] }];
  assert.deepEqual(hopClash(hy(9, 'b', 9443, '25000-26000'), os, nodes), { field: 'hop', kind: 'overlap', other: 'a', where: { server: 'main' } });
  assert.deepEqual(hopClash(hy(9, 'b', 9443, '30000-31000'), os, nodes)?.kind, 'overlap', 'touching edge');
  assert.equal(hopClash(hy(9, 'b', 9443, '30001-31000'), os, nodes), null);
  assert.deepEqual(hopClash(hy(9, 'b', 9443, '39000-41000'), os, nodes)?.kind, 'covers');
  assert.equal(hopClash(hy(9, 'b', 9443, '44000-46000'), os, nodes), null, 'TCP port inside is fine');
  assert.deepEqual(hopClash(hy(9, 'b', 21000, ''), os, nodes), { field: 'port', kind: 'inside', other: 'a', where: { server: 'main' } });
  assert.equal(hopClash(hy(9, 'b', 9443, '25000-26000', false, [1]), os, nodes), null, 'other server');
  assert.equal(hopClash(hy(1, 'a', 8443, '20000-30000'), os, nodes), null, 'itself');
  const listenA = [hy(1, 'a', 8443, '20000-30000', true, [], '1.1.1.1')];
  assert.equal(hopClash(hy(9, 'b', 9443, '25000-26000', true, [], '2.2.2.2'), listenA, nodes), null, 'different listen IPs');
  assert.ok(hopClash(hy(9, 'b', 9443, '25000-26000', true, [], ''), listenA, nodes), 'all addresses overlap');
  const onNode = [hy(1, 'a', 8443, '20000-30000', false, [1])];
  assert.deepEqual(hopClash(hy(9, 'b', 9443, '۲۵۰۰۰-۲۶۰۰۰', true, [2, 1]), onNode, nodes)?.where, { server: 'node', nodeId: 1, name: 'Lightsail' });
  assert.deepEqual(parseHopRanges('30000-31000, 20000'), [[20000, 20000], [30000, 31000]]);
  assert.equal(parseHopRanges('1-2'), null);
});
