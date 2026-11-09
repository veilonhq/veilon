import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MAX_RELEASES, versionItems } from '../src/pages/dashboard/xrayVersions.ts';

const rel = (...tags: string[]) => tags.map((tag) => ({ tag }));
const list = rel('v26.11.9', 'v26.10.31', 'v26.10.21', 'v26.9.30', 'v26.9.9', 'v26.7.28', 'v26.7.11');

test('only the newest five releases are offered', () => {
  const items = versionItems(list, 'v26.10.31');
  assert.equal(items.length, MAX_RELEASES);
  assert.deepEqual(items.map((i) => i.tag), ['v26.11.9', 'v26.10.31', 'v26.10.21', 'v26.9.30', 'v26.9.9']);
  assert.deepEqual(items.filter((i) => i.current).map((i) => i.tag), ['v26.10.31']);
});

test('an older installed version stays listed and marked current', () => {
  const items = versionItems(list, 'v25.9.5');
  assert.equal(items.length, MAX_RELEASES + 1);
  assert.deepEqual(items.at(-1), { tag: 'v25.9.5', current: true });
  assert.equal(items.filter((i) => i.current).length, 1);
});

test('a newer unlisted install goes on top; no install, no current', () => {
  assert.deepEqual(versionItems(list, 'v26.12.1-rc1')[0], { tag: 'v26.12.1-rc1', current: true });
  assert.ok(versionItems(list, '').every((i) => !i.current));
  assert.deepEqual(versionItems([], 'v26.9.9'), [{ tag: 'v26.9.9', current: true }]);
});
