import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BYTE_INCRS, bucketAt, labelsDays, timeIncrs, toSeries } from '../src/components/trafficScale.ts';

test('byte ticks are round binary sizes', () => {
  assert.ok(BYTE_INCRS.includes(1024 ** 2));
  assert.ok(BYTE_INCRS.includes(2 * 1024 ** 2));
  assert.ok(!BYTE_INCRS.includes(2_000_000));
  for (let i = 1; i < BYTE_INCRS.length; i++) assert.ok(BYTE_INCRS[i] > BYTE_INCRS[i - 1]);
});

test('multi-day ranges tick on whole days only', () => {
  for (const r of ['7d', '30d'] as const) for (const s of timeIncrs(r)) assert.equal(s % 86_400, 0);
  assert.ok(timeIncrs('24h').every((s) => s % 3600 === 0 && s < 86_400));
  assert.equal(labelsDays('24h', 900), false);
  assert.equal(labelsDays('7d', 3600), true);
});

test('series keeps bytes, sorts by time and closes the last bucket', () => {
  const pts = [
    { t: '2026-11-09T18:30:00Z', up: 10, down: 7_519_550 },
    { t: '2026-11-09T18:00:00Z', up: 273_683, down: 3_389_544 },
    { t: '2026-11-09T18:15:00Z', up: 0, down: 0 },
  ];
  const [xs, down, up] = toSeries(pts, 900);
  const t0 = Date.UTC(2026, 10, 9, 18, 0) / 1000;
  assert.deepEqual(xs, [t0, t0 + 900, t0 + 1800, t0 + 2700]);
  assert.deepEqual(down, [3_389_544, 0, 7_519_550, 7_519_550]);
  assert.deepEqual(up, [273_683, 0, 10, 10]);
  // the bucket total equals the server total (closing row excluded)
  assert.equal(down.slice(0, -1).reduce((a, b) => a + b, 0), 3_389_544 + 7_519_550);
});

test('hover picks the bucket containing the cursor', () => {
  const xs = [0, 900, 1800, 2700];
  assert.equal(bucketAt(xs, 0), 0);
  assert.equal(bucketAt(xs, 899), 0);
  assert.equal(bucketAt(xs, 900), 1);
  assert.equal(bucketAt(xs, 2699), 2);
  assert.equal(bucketAt(xs, 2700), -1);
  assert.equal(bucketAt(xs, -1), -1);
  assert.equal(bucketAt([], 5), -1);
});
