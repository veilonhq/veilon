import { useEffect, useRef } from 'react';
import uPlot from 'uplot';
import type { TrafficPoint, TrafficRange } from '@/api/types';
import { formatBytes, formatDate, formatTime } from '@/i18n';
import { useUi } from '@/store/ui';

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** Stacked area history chart (download + upload) using uPlot. Always LTR: time flows left→right. */
export function TrafficChart({ points, range, height = 240 }: { points: TrafficPoint[]; range: TrafficRange; height?: number }) {
  const wrap = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);
  const { lang, theme } = useUi();

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const xs = points.map((p) => new Date(p.t).getTime() / 1000);
    const down = points.map((p) => p.down);
    const up = points.map((p) => p.up);
    const accent = cssVar('--accent');
    const violet = cssVar('--violet');
    const muted = cssVar('--muted');
    const line = cssVar('--line');
    const font = `12px ${lang === 'fa' ? 'Vazirmatn Variable' : 'Inter Variable'}`;

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
      cursor: { points: { size: 9, fill: (_u, i) => (i === 1 ? accent : violet) as string }, drag: { x: false, y: false } },
      scales: { x: { time: true } },
      axes: [
        {
          stroke: muted,
          font,
          grid: { show: false },
          ticks: { show: false },
          gap: 8,
          size: 34,
          values: (_u, splits) => splits.map((v) => (range === '24h' ? formatTime(lang, new Date(v * 1000)) : formatDate(lang, new Date(v * 1000), { month: 'short', day: 'numeric' }))),
        },
        {
          stroke: muted,
          font,
          size: 70,
          gap: 6,
          grid: { stroke: line, width: 1, dash: [3, 5] },
          ticks: { show: false },
          values: (_u, splits) => splits.map((v) => formatBytes(lang, v, 0)),
        },
      ],
      series: [
        {},
        { label: 'down', stroke: accent, width: 2, fill: (u) => fill(u, accent), points: { show: false } },
        { label: 'up', stroke: violet, width: 2, fill: (u) => fill(u, violet), points: { show: false } },
      ],
    };
    plot.current?.destroy();
    plot.current = new uPlot(opts, [xs, down, up], el);
    const ro = new ResizeObserver(() => plot.current?.setSize({ width: el.clientWidth, height }));
    ro.observe(el);
    return () => {
      ro.disconnect();
      plot.current?.destroy();
      plot.current = null;
    };
  }, [points, range, height, lang, theme]);

  return <div ref={wrap} dir="ltr" style={{ width: '100%', minHeight: height }} role="img" aria-label="Traffic history chart" />;
}
