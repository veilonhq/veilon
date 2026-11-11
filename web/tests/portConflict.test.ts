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
