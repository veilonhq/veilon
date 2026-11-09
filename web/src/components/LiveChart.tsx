import { useEffect, useRef, useState } from 'react';
import { useReducedMotion } from 'framer-motion';
import type { SystemStats } from '@/api/types';
import { formatBps } from '@/i18n';
import type { Lang } from '@/store/ui';

/** One measured rate: bytes per second over the sampler window ending at t (ms). */
export interface RateSample {
  t: number;
  up: number;
  down: number;
}

/** How much history the live chart keeps and shows. */
export const LIVE_WINDOW_MS = 120_000;

/**
 * Collects the server's measured network rates into a time series. Only
 * real samples are kept: a poll that returns the same measurement again
 * (the sampler runs on its own interval) adds nothing, and there is no
 * synthetic history. Older servers without net_sampled_at fall back to the
 * time the poll arrived.
 */
export function useRateSeries(d: SystemStats | undefined, receivedAt: number) {
  const [buf, setBuf] = useState<RateSample[]>([]);
  const last = useRef<string>('');
  useEffect(() => {
    if (!d || typeof d.net_up_bps !== 'number' || typeof d.net_down_bps !== 'number') return;
    const key = d.net_sampled_at ?? `${receivedAt}`;
    if (key === last.current) return;
    last.current = key;
    const t = d.net_sampled_at ? new Date(d.net_sampled_at).getTime() : receivedAt;
    if (!Number.isFinite(t)) return;
    setBuf((b) => {
      const next = b.length && t <= b[b.length - 1].t ? b : [...b, { t, up: Math.max(0, d.net_up_bps), down: Math.max(0, d.net_down_bps) }];
      const from = t - LIVE_WINDOW_MS - 10_000;
      return next[0]?.t < from ? next.filter((p) => p.t >= from) : next;
    });
  }, [d, receivedAt]);
  return buf;
}

/** A "nice" axis maximum ≥ v (1, 2, 2.5 or 5 × 10ⁿ in bits/s). */
function niceMax(bytesPerSec: number) {
  const bits = Math.max(bytesPerSec * 8, 8_000); // floor: 8 kbps, so an idle line does not fill the chart with noise
  const p = Math.pow(10, Math.floor(Math.log10(bits)));
  const m = [1, 2, 2.5, 5, 10].find((k) => k * p >= bits) ?? 10;
  return (m * p) / 8;
}

/**
 * Live network rate: download above the axis, upload below, both on one
 * shared scale (the top and bottom grid lines show it). x is real time: the
 * newest sample sits at the right edge and the chart scrolls as time
 * passes; a gap in sampling (tab hidden, server busy) leaves a gap.
 */
export function LiveChart({ samples, height = 200, lang, theme }: { samples: RateSample[]; height?: number; lang: Lang; theme?: string }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const reduce = useReducedMotion();
  const data = useRef(samples);
  data.current = samples;
  const [w, setW] = useState(600);
  const scale = useRef(0);
  // when the newest sample arrived in this tab, for smooth scrolling
  const drawnAt = useRef(performance.now());
  const lastT = samples[samples.length - 1]?.t;
  useEffect(() => {
    drawnAt.current = performance.now();
  }, [lastT]);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.max(120, Math.floor(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const cv = canvas.current;
    if (!cv) return;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    cv.width = w * dpr;
    cv.height = height * dpr;
    const ctx = cv.getContext('2d')!;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    let raf = 0;
    const css = getComputedStyle(document.documentElement);
    const accent = css.getPropertyValue('--accent').trim() || '#c6ff3d';
    const violet = css.getPropertyValue('--violet').trim() || '#8b7cff';
    const line = css.getPropertyValue('--line').trim() || 'rgba(255,255,255,.1)';
    const muted = css.getPropertyValue('--muted').trim() || '#8a8f98';
    const font = `11px ${lang === 'fa' ? 'Vazirmatn Variable' : 'Inter Variable'}, sans-serif`;
    const top = 18;
    const bottom = height - 18;
    const mid = top + (bottom - top) * 0.58;

    const draw = () => {
      const pts = data.current;
      ctx.clearRect(0, 0, w, height);
      const last = pts[pts.length - 1];
      // the right edge is "now" on the server's clock, advanced smoothly between samples
      const spacing = pts.length > 1 ? Math.min(10_000, last.t - pts[pts.length - 2].t) : 3000;
      const head = last ? (reduce ? last.t : Math.min(last.t + spacing, last.t + (performance.now() - drawnAt.current))) : 0;
      const x0 = head - LIVE_WINDOW_MS;
      const xOf = (t: number) => ((t - x0) / LIVE_WINDOW_MS) * w;
      const visible = pts.filter((p) => p.t >= x0 - spacing);
      const peak = Math.max(0, ...visible.map((p) => Math.max(p.up, p.down)));
      const target = niceMax(peak);
      // ease the scale so a new peak does not make the lines jump
      scale.current = scale.current ? scale.current + (target - scale.current) * (reduce ? 1 : 0.15) : target;
      const max = scale.current;

      ctx.strokeStyle = line;
      ctx.lineWidth = 1;
      ctx.setLineDash([2, 6]);
      for (const y of [top, bottom]) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(w, y);
        ctx.stroke();
      }
      ctx.setLineDash([]);
      ctx.beginPath();
      ctx.moveTo(0, mid);
      ctx.lineTo(w, mid);
      ctx.stroke();
      ctx.fillStyle = muted;
      ctx.font = font;
      ctx.textBaseline = 'bottom';
      ctx.textAlign = 'left';
      ctx.fillText(`↓ ${formatBps(lang, max)}`, 4, top - 3);
      ctx.textBaseline = 'top';
      ctx.fillText(`↑ ${formatBps(lang, max)}`, 4, bottom + 3);

      const series = (key: 'up' | 'down', sign: 1 | -1, amp: number, color: string) => {
        if (visible.length < 1) return;
        const yOf = (v: number) => mid - sign * Math.min(1, v / max) * amp;
        // split at gaps longer than 3 sampling intervals
        const runs: RateSample[][] = [];
        let run: RateSample[] = [];
        for (const p of visible) {
          if (run.length && p.t - run[run.length - 1].t > Math.max(3 * spacing, 9000)) {
            runs.push(run);
            run = [];
          }
          run.push(p);
        }
        if (run.length) runs.push(run);
        for (const r of runs) {
          const xy = r.map((p) => [xOf(p.t), yOf(p[key])] as const);
          const path = new Path2D();
          path.moveTo(xy[0][0], xy[0][1]);
          for (let i = 1; i < xy.length; i++) {
            const [xa, ya] = xy[i - 1];
            const [xb, yb] = xy[i];
            const cx = (xa + xb) / 2; // horizontal tangents: smooth, never overshoots a sample
            path.bezierCurveTo(cx, ya, cx, yb, xb, yb);
          }
          const fill = new Path2D(path);
          fill.lineTo(xy[xy.length - 1][0], mid);
          fill.lineTo(xy[0][0], mid);
          fill.closePath();
          const g = ctx.createLinearGradient(0, sign > 0 ? mid - amp : mid + amp, 0, mid);
          g.addColorStop(0, color + '55');
          g.addColorStop(1, color + '00');
          ctx.fillStyle = g;
          ctx.fill(fill);
          ctx.strokeStyle = color;
          ctx.lineWidth = 2;
          ctx.shadowColor = color;
          ctx.shadowBlur = 10;
          ctx.stroke(path);
          ctx.shadowBlur = 0;
        }
        const lastRun = runs[runs.length - 1];
        const hp = lastRun[lastRun.length - 1];
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(xOf(hp.t), yOf(hp[key]), 3.5, 0, Math.PI * 2);
        ctx.fill();
      };
      series('down', 1, mid - top, accent);
      series('up', -1, bottom - mid, violet);
      if (!reduce) raf = requestAnimationFrame(draw);
    };
    draw();
    return () => cancelAnimationFrame(raf);
    // With reduced motion there is no rAF loop, so redraw whenever the data changes.
  }, [w, height, reduce, reduce ? samples : null, theme, lang]);

  return (
    <div ref={wrap} style={{ width: '100%', height }} dir="ltr">
      <canvas ref={canvas} style={{ width: '100%', height, display: 'block' }} role="img" aria-label="Live traffic" />
    </div>
  );
}
