import { useEffect, useRef, useState } from 'react';
import uPlot from 'uplot';
import type { TrafficPoint, TrafficRange } from '@/api/types';
import { formatBytes, formatDate, formatTime, translate } from '@/i18n';
import { useUi } from '@/store/ui';
import { BYTE_INCRS, bucketAt, labelsDays, timeIncrs, toSeries } from './trafficScale';

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** One hovered bucket: its start, end and the bytes counted in it. */
interface Readout {
  t: number;
  end: number;
  down: number;
  up: number;
}

/**
 * Traffic history (download and upload) with uPlot. Each point is the volume
 * counted in one bucket (bytes), as the server returns it; buckets start on
 * the panel's clock and the axis is drawn in the panel's time zone. A bucket
 * is drawn as a flat step over its whole span (a line between bucket starts
 * would put each bucket's volume at its start and smear it into the next
 * one). Always LTR: time flows left→right.
 */
export function TrafficChart({ points, range, bucketSec, height = 240 }: { points: TrafficPoint[]; range: TrafficRange; bucketSec?: number; height?: number }) {
  const wrap = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);
  const { lang, theme, tz } = useUi();
  const [hover, setHover] = useState<Readout | null>(null);
  const step = bucketSec || (points.length > 1 ? (new Date(points[1].t).getTime() - new Date(points[0].t).getTime()) / 1000 : 900);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const [xs, down, up] = toSeries(points, step);
    const accent = cssVar('--accent');
    const violet = cssVar('--violet');
    const muted = cssVar('--muted');
    const line = cssVar('--line');
    const font = `12px ${lang === 'fa' ? 'Vazirmatn Variable' : 'Inter Variable'}`;
    const daily = labelsDays(range, step);
    const stepped = uPlot.paths.stepped!({ align: 1 });
    const x0 = xs.length ? xs[0] : 0;
    // the scale stops just short of the window's end, so a tick on that edge (the next midnight) is not drawn half cut off
    const x1 = xs.length ? xs[xs.length - 1] - 1 : 1;

    const fill = (u: uPlot, color: string) => {
      const g = u.ctx.createLinearGradient(0, u.bbox.top, 0, u.bbox.top + u.bbox.height);
      g.addColorStop(0, color + '66');
      g.addColorStop(1, color + '00');
      return g;
    };

    const opts: uPlot.Options = {
      width: el.clientWidth,
      height,
      padding: [12, 8, 0, 0],
      legend: { show: false },
      // ticks fall on the panel's clock (hours/midnights there), not the browser's
      tzDate: tz ? (ts) => uPlot.tzDate(new Date(ts * 1000), tz) : undefined,
      cursor: {
        points: { show: false },
        drag: { x: false, y: false },
        // the hovered bucket is the one under the pointer, not the nearest bucket start
        dataIdx: (_u, _s, _closest, xv) => {
          const i = bucketAt(xs, xv);
          return i < 0 ? null : i;
        },
      },
      scales: { x: { time: true, range: () => [x0, x1] }, y: { range: (_u, _min, max) => [0, max > 0 ? max * 1.08 : 1024] } },
      axes: [
        {
          stroke: muted,
          font,
          grid: { show: false },
          ticks: { show: false },
          gap: 8,
          size: 34,
          incrs: timeIncrs(range),
          values: (_u, splits) => splits.map((v) => (daily ? formatDate(lang, new Date(v * 1000), { month: 'short', day: 'numeric' }) : formatTime(lang, new Date(v * 1000)))),
        },
        {
          stroke: muted,
          font,
          size: 70,
          incrs: BYTE_INCRS,
          gap: 6,
          grid: { stroke: line, width: 1, dash: [3, 5] },
          ticks: { show: false },
          values: (_u, splits) => splits.map((v) => formatBytes(lang, v, v > 0 && v < 10 * 1024 ** 3 ? 1 : 0)),
        },
      ],
      series: [
        {},
        { label: 'down', stroke: accent, width: 2, fill: (u) => fill(u, accent), paths: stepped, points: { show: false } },
        { label: 'up', stroke: violet, width: 2, fill: (u) => fill(u, violet), paths: stepped, points: { show: false } },
      ],
      hooks: {
        setCursor: [
          (u) => {
            const i = u.cursor.idx;
            if (i == null || i < 0 || i >= xs.length - 1) return setHover(null);
            setHover({ t: xs[i] * 1000, end: (xs[i] + step) * 1000, down: down[i], up: up[i] });
          },
        ],
      },
    };
    plot.current?.destroy();
    plot.current = new uPlot(opts, [xs, down, up], el);
    const ro = new ResizeObserver(() => plot.current?.setSize({ width: el.clientWidth, height }));
    ro.observe(el);
    const leave = () => setHover(null);
    el.addEventListener('mouseleave', leave);
    return () => {
      ro.disconnect();
      el.removeEventListener('mouseleave', leave);
      plot.current?.destroy();
      plot.current = null;
    };
  }, [points, range, height, lang, theme, tz, step]);

  const sum = points.reduce((a, p) => [a[0] + p.down, a[1] + p.up], [0, 0]);
  // "18 Aban, 09:30–10:00" (time range isolated LTR so it reads the same in RTL)
  const when = (r: Readout) => {
    const day = formatDate(lang, new Date(r.t), { month: 'short', day: 'numeric' });
    if (step >= 86_400) return day;
    return `${day}، \u2066${formatTime(lang, new Date(r.t))}–${formatTime(lang, new Date(r.end))}\u2069`;
  };

  return (
    <div>
      <div className="num" style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 14px', fontSize: 12.5, color: 'var(--muted)', minHeight: 20, marginBottom: 6 }} aria-live="polite">
        <span>{hover ? (lang === 'fa' ? when(hover) : when(hover).replace('،', ',')) : translate(lang, 'dash.rangeTotal')}</span>
        <span style={{ color: 'var(--accent)' }}>
          ↓ <bdi>{formatBytes(lang, hover ? hover.down : sum[0])}</bdi>
        </span>
        <span style={{ color: 'var(--violet)' }}>
          ↑ <bdi>{formatBytes(lang, hover ? hover.up : sum[1])}</bdi>
        </span>
      </div>
      <div ref={wrap} dir="ltr" style={{ width: '100%', minHeight: height }} role="img" aria-label="Traffic history chart" />
    </div>
  );
}
