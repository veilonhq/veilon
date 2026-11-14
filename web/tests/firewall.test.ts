import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { addEntries, checkEntry, entryKind, splitEntries, FW_MAX } from '../src/lib/firewall.ts';

const cases = JSON.parse(readFileSync(new URL('../../panel/internal/userfw/testdata/cases.json', import.meta.url), 'utf8')) as {
  ok: [string, string, string][];
  bad: [string, string][];
};

test('firewall entries: same verdicts as the server (shared cases)', () => {
  for (const [input, kind, canonical] of cases.ok) {
    const r = checkEntry(input);
    assert.ok(r.ok, `${input} refused: ${JSON.stringify(r)}`);
    if (r.ok) assert.equal(r.kind, kind, input);
    assert.equal(entryKind(canonical), kind, canonical);
  }
  for (const [input, reason] of cases.bad) {
    const r = checkEntry(input);
    assert.ok(!r.ok, `${input} accepted`);
    if (!r.ok) assert.equal(r.reason, reason, input);
  }
  assert.deepEqual(checkEntry('a.'.repeat(130) + 'com'), { ok: false, reason: 'too_long' });
});

test('firewall: split and add (duplicates skipped, first bad entry stops, max)', () => {
  assert.deepEqual(splitEntries('a.com, b.com\n1.2.3.4؛x.org،y.org;z.org'), ['a.com', 'b.com', '1.2.3.4', 'x.org', 'y.org', 'z.org']);
  let r = addEntries(['example.com'], 'Example.com domain:example.com full:api.x.org 10.0.0.0/8');
  assert.deepEqual(r.list, ['example.com', 'full:api.x.org', '10.0.0.0/8']);
  assert.equal(r.added, 2);
  r = addEntries(['a.com'], 'b.com geosite:x c.com');
  assert.deepEqual(r.list, ['a.com']);
  assert.deepEqual(r.error, { entry: 'geosite:x', reason: 'unsupported' });
  const full = Array.from({ length: FW_MAX }, (_, i) => `d${i}.com`);
  assert.equal(addEntries(full, 'one-more.com').error?.reason, 'too_many');
});
