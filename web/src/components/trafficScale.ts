/**
 * Pure helpers for the traffic history chart (no DOM, no uPlot), so they can
 * be tested on their own.
 */

export type HistoryRange = '24h' | '7d' | '30d';

/** One server point: bytes counted in the bucket starting at t (RFC 3339). */
export interface BucketPoint {
  t: string;
  up: number;
  down: number;
}

const HOUR = 3600;
const DAY = 86_400;

/**
 * Y-axis tick steps in powers of 1024, so labels read as round values in the
 * units the UI prints (1 MB, 2 MB, 512 KB), not 1,9 MB / 3,8 MB from decimal
 * steps.
 */
export const BYTE_INCRS: number[] = (() => {
  const out: number[] = [];
  for (let p = 0; p <= 5; p++) {
    const u = 1024 ** p;
    for (const m of [1, 2, 5, 10, 20, 50, 100, 200, 512]) out.push(m * u);
  }
  return out.sort((a, b) => a - b);
})();

/**
 * X-axis tick steps (seconds) per range: hours for 24 hours, whole days for
 * 7 and 30 days, so a day never gets more than one label.
 */
export function timeIncrs(range: HistoryRange): number[] {
  if (range === '24h') return [HOUR, 2 * HOUR, 3 * HOUR, 4 * HOUR, 6 * HOUR, 12 * HOUR];
  return [DAY, 2 * DAY, 3 * DAY, 5 * DAY, 7 * DAY, 10 * DAY];
}

/** Whether the x axis labels days (true) or clock times (false). */
export function labelsDays(range: HistoryRange, stepSec: number) {
  return range !== '24h' || stepSec >= DAY;
}

/**
 * Chart columns for a step plot: x in seconds, sorted, one row per bucket,
 * plus a closing row at the end of the last bucket so it is drawn with its
 * full width. Values are bytes per bucket, never scaled.
 */
export function toSeries(points: BucketPoint[], stepSec: number): [number[], number[], number[]] {
  const rows = points
    .map((p) => ({ x: Math.round(new Date(p.t).getTime() / 1000), down: Math.max(0, p.down || 0), up: Math.max(0, p.up || 0) }))
    .filter((r) => Number.isFinite(r.x))
    .sort((a, b) => a.x - b.x);
  const xs = rows.map((r) => r.x);
  const down = rows.map((r) => r.down);
  const up = rows.map((r) => r.up);
  if (rows.length) {
    const last = rows[rows.length - 1];
    xs.push(last.x + stepSec);
    down.push(last.down);
    up.push(last.up);
  }
  return [xs, down, up];
}

/**
 * The bucket that contains x (seconds): the last bucket starting at or
 * before x. The closing row is not a bucket. Returns -1 outside the data.
 */
export function bucketAt(xs: number[], x: number): number {
  const n = xs.length - 1; // buckets (last row only closes the final one)
  if (n <= 0 || x < xs[0] || x >= xs[n]) return -1;
  let lo = 0;
  let hi = n - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (xs[mid] <= x) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}
