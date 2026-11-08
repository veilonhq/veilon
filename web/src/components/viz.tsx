import { animate, useReducedMotion } from 'framer-motion';
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import s from './viz.module.css';

/* ---------------- CountUp ---------------- */
/**
 * Last shown value per number, kept outside React so a remount (leaving the
 * dashboard and coming back, a route transition) never restarts an animation.
 */
const lastShown = new Map<string, number>();

/**
 * Animated number. Rules (no count-from-zero, ever):
 *  - first render shows the value immediately;
 *  - later updates tween briefly from the previously shown value to the new one;
 *  - `canTween(prev, next)` can veto a tween (e.g. bytes crossing MB → GB, where
 *    digits and unit would disagree mid-way): then the value is set directly;
 *  - tabular digits in a box that never shrinks while the number keeps its
 *    digit count, so digits don't jiggle.
 * `id` names the number (e.g. 'dash.total') so its last value survives remounts.
 */
export function CountUp({
  value,
  format,
  id,
  duration = 0.45,
  canTween,
}: {
  value: number;
  format: (v: number) => string;
  id?: string;
  duration?: number;
  canTween?: (prev: number, next: number) => boolean;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  // where the DOM text currently stands; starts at the value itself (or the
  // value this number last showed before a remount), never at 0
  const shown = useRef<number>(id !== undefined && lastShown.has(id) ? lastShown.get(id)! : value);
  const reduce = useReducedMotion();
  const fmt = useRef(format);
  fmt.current = format;
  const veto = useRef(canTween);
  veto.current = canTween;
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const from = shown.current;
    const set = (v: number) => {
      shown.current = v;
      if (id !== undefined) lastShown.set(id, v);
      el.textContent = fmt.current(v);
    };
    if (reduce || from === value || !Number.isFinite(from) || (veto.current && !veto.current(from, value))) {
      set(value);
      return;
    }
    const c = animate(from, value, { duration, ease: [0.22, 1, 0.36, 1], onUpdate: set });
    return () => {
      c.stop();
      // an interrupted tween leaves the DOM where it stopped; shown.current already tracks that
    };
  }, [value, duration, reduce, id]);
  const text = format(shown.current);
  return (
    <span ref={ref} className="num" style={{ display: 'inline-block', minWidth: `${format(value).length}ch`, textAlign: 'end' }}>
      {text}
    </span>
  );
}

/* ---------------- Quota ring ---------------- */
export function QuotaRing({
  ratio,
  size = 64,
  stroke = 7,
  tone = 'accent',
  children,
  label,
}: {
  ratio: number | null; // null = unlimited
  size?: number;
  stroke?: number;
  tone?: 'accent' | 'warn' | 'danger' | 'muted' | 'info';
  children?: React.ReactNode;
  label?: string;
}) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const ref = useRef<SVGSVGElement>(null);
  const v = ratio === null ? 1 : Math.max(0, Math.min(1, ratio));
  const color = { accent: 'var(--accent)', warn: 'var(--warn)', danger: 'var(--danger)', muted: 'var(--muted)', info: 'var(--info)' }[tone];
  return (
    <div className={s.ring} style={{ width: size, height: size }} role="img" aria-label={label}>
      <svg ref={ref} width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--line)" strokeWidth={stroke} />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={ratio === null ? `2 ${c / 36 - 2}` : `${c} ${c}`}
          // real value from the first paint (no fill-from-empty on mount); the CSS transition only animates later changes
          strokeDashoffset={ratio === null ? 0 : c * (1 - v)}
          style={{ transition: 'stroke-dashoffset 1.1s cubic-bezier(.22,1,.36,1)', transform: 'rotate(-90deg)', transformOrigin: 'center' }}
          opacity={ratio === null ? 0.7 : 1}
        />
      </svg>
      <div className={s.ringInner}>{children}</div>
    </div>
  );
}

/* ---------------- Arc gauge (270°) ---------------- */
/**
 * Arc gauge. The percent and name sit inside the arc; capacity lines sit in
 * normal flow underneath, so nothing can overlap whatever the width. The arc
 * scales with its container (it never sets a fixed pixel width).
 */
export function ArcGauge({ value, label, detail, lines, title, off }: { value: number; label: string; detail: string; lines?: ReactNode[]; title?: string; off?: boolean }) {
  const size = 132;
  const stroke = 10;
  const r = (size - stroke) / 2 - 2;
  const cx = size / 2,
    cy = size / 2;
  const sweep = 270;
  const start = 135;
  const polar = (deg: number) => {
    const a = (deg * Math.PI) / 180;
    return [cx + r * Math.cos(a), cy + r * Math.sin(a)];
  };
  const [sx, sy] = polar(start);
  const [ex, ey] = polar(start + sweep);
  const path = `M ${sx} ${sy} A ${r} ${r} 0 1 1 ${ex} ${ey}`;
  const len = (sweep / 360) * 2 * Math.PI * r;
  const v = Number.isFinite(value) ? Math.max(0, Math.min(100, value)) / 100 : 0;
  const tone = v > 0.85 ? 'var(--danger)' : v > 0.65 ? 'var(--warn)' : 'var(--accent)';
  const gid = useId();
  const ticks = useMemo(() => Array.from({ length: 28 }, (_, i) => start + (i * sweep) / 27), []);
  return (
    <div className={`${s.gauge} ${off ? s.gaugeOff : ''} viz-gauge`} title={title}>
      <div className={s.gaugeArc}>
        <svg viewBox={`0 0 ${size} ${size * 0.86}`} preserveAspectRatio="xMidYMin meet" aria-hidden="true">
          <defs>
            <linearGradient id={gid} x1="0" x2="1">
              <stop offset="0" stopColor="var(--violet)" />
              <stop offset="1" stopColor={tone} />
            </linearGradient>
          </defs>
          {ticks.map((d, i) => {
            const a = (d * Math.PI) / 180;
            const r1 = r - 12,
              r2 = r - (i % 3 === 0 ? 17 : 15);
            return (
              <line key={i} x1={cx + r1 * Math.cos(a)} y1={cy + r1 * Math.sin(a)} x2={cx + r2 * Math.cos(a)} y2={cy + r2 * Math.sin(a)} stroke="var(--line-2)" strokeWidth={1.2} />
            );
          })}
          <path d={path} fill="none" stroke="var(--line)" strokeWidth={stroke} strokeLinecap="round" />
          {!off && (
            <path
              d={path}
              fill="none"
              stroke={`url(#${gid})`}
              strokeWidth={stroke}
              strokeLinecap="round"
              strokeDasharray={`${len} ${len}`}
              strokeDashoffset={len * (1 - v)}
              style={{ transition: 'stroke-dashoffset .9s cubic-bezier(.22,1,.36,1)' }}
            />
          )}
        </svg>
        <div className={s.gaugeText}>
          <strong className="num display">{label}</strong>
          <span>{detail}</span>
        </div>
      </div>
      {lines && lines.length > 0 && (
        <div className={s.gaugeLines}>
          {lines.map((l, i) => (
            <span key={i} className={i === 0 ? s.gaugeLine1 : s.gaugeLine2}>
              {l}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------------- Live waveform ---------------- */
/** Two mirrored smooth waves (download above, upload below the axis) drawn on canvas. */
export function Waveform({ up, down, height = 180, theme }: { up: number[]; down: number[]; height?: number; theme?: string }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const reduce = useReducedMotion();
  const data = useRef({ up, down });
  data.current = { up, down };
  const [w, setW] = useState(600);

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
    ctx.scale(dpr, dpr);
    let raf = 0;
    let phase = 0;
    const css = getComputedStyle(document.documentElement);
    const accent = css.getPropertyValue('--accent').trim() || '#c6ff3d';
    const violet = css.getPropertyValue('--violet').trim() || '#8b7cff';
    const line = css.getPropertyValue('--line').trim();
    const rtl = document.documentElement.dir === 'rtl';

    const draw = () => {
      const { up: u, down: d } = data.current;
      ctx.clearRect(0, 0, w, height);
      const mid = height * 0.6;
      // baseline grid
      ctx.strokeStyle = line;
      ctx.lineWidth = 1;
      ctx.setLineDash([2, 6]);
      for (const y of [mid - (mid - 8) * 0.5, mid - (mid - 8)]) {
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

      const series = (vals: number[], dirSign: 1 | -1, amp: number, color: string) => {
        const n = vals.length;
        if (n < 2) return;
        // each direction is scaled to its own peak so the smaller upload wave stays readable
        const max = Math.max(1, ...vals) * 1.12;
        const step = w / (n - 1);
        const pts: [number, number][] = vals.map((v, i) => {
          const wobble = reduce ? 0 : Math.sin(i * 0.9 + phase * (dirSign > 0 ? 1 : 1.3)) * 2.2;
          const x = rtl ? w - i * step : i * step;
          return [x, mid - dirSign * ((v / max) * amp + wobble * (v > 0 ? 1 : 0))];
        });
        const path = new Path2D();
        path.moveTo(pts[0][0], pts[0][1]);
        for (let i = 1; i < pts.length; i++) {
          const [x0, y0] = pts[i - 1];
          const [x1, y1] = pts[i];
          const cx = (x0 + x1) / 2;
          path.bezierCurveTo(cx, y0, cx, y1, x1, y1);
        }
        const fill = new Path2D(path);
        fill.lineTo(pts[pts.length - 1][0], mid);
        fill.lineTo(pts[0][0], mid);
        fill.closePath();
        const g = ctx.createLinearGradient(0, dirSign > 0 ? mid - amp : mid + amp, 0, mid);
        g.addColorStop(0, color + '55');
        g.addColorStop(1, color + '00');
        ctx.fillStyle = g;
        ctx.fill(fill);
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.shadowColor = color;
        ctx.shadowBlur = 12;
        ctx.stroke(path);
        ctx.shadowBlur = 0;
        // head dot
        const [hx, hy] = pts[pts.length - 1];
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(hx, hy, 4, 0, Math.PI * 2);
        ctx.fill();
        if (!reduce) {
          ctx.globalAlpha = 0.25 + 0.25 * Math.sin(phase * 3);
          ctx.beginPath();
          ctx.arc(hx, hy, 10, 0, Math.PI * 2);
          ctx.fill();
          ctx.globalAlpha = 1;
        }
      };
      series(d, 1, mid - 10, accent);
      series(u, -1, height - mid - 8, violet);
      phase += 0.03;
      if (!reduce) raf = requestAnimationFrame(draw);
    };
    draw();
    return () => cancelAnimationFrame(raf);
    // With reduced motion there is no rAF loop, so redraw whenever the data changes.
  }, [w, height, reduce, reduce ? up : null, reduce ? down : null, theme]);

  return (
    <div ref={wrap} className={s.wave} style={{ height }}>
      <canvas ref={canvas} style={{ width: w, height }} aria-hidden="true" />
    </div>
  );
}

/* ---------------- Core pulse ---------------- */
export function CorePulse({ running, size = 168 }: { running: boolean; size?: number }) {
  return (
    <div className={`${s.pulse} ${running ? s.pulseOn : s.pulseOff}`} style={{ width: size, height: size }} aria-hidden="true">
      <span className={s.pr1} />
      <span className={s.pr2} />
      <span className={s.pr3} />
      <svg className={s.pulseSvg} viewBox="0 0 100 100">
        <circle cx="50" cy="50" r="46" fill="none" stroke="currentColor" strokeOpacity=".18" strokeDasharray="1 3.2" />
        <circle cx="50" cy="50" r="36" fill="none" stroke="currentColor" strokeOpacity=".3" strokeDasharray="12 6" className={s.rot} />
        <circle cx="50" cy="50" r="26" fill="none" stroke="currentColor" strokeOpacity=".5" strokeWidth="1.5" strokeDasharray="40 20" className={s.rotRev} />
      </svg>
      <span className={s.core} />
    </div>
  );
}

/* ---------------- Online orbit ---------------- */
export function OnlineOrbit({ names, total, size = 220 }: { names: string[]; total: number; size?: number }) {
  const rings = [0.41, 0.3, 0.19];
  const shown = names.slice(0, 14);
  return (
    <div className={s.orbit} style={{ width: size, height: size }} aria-hidden="true">
      {rings.map((r, i) => (
        <span key={i} className={s.orbitRing} style={{ inset: `${(0.5 - r) * 100}%` }} />
      ))}
      {rings.map((r, ri) => {
        const members = shown.filter((_, i) => i % 3 === ri);
        return (
          <div key={ri} className={s.orbitTrack} style={{ animationDuration: `${38 + ri * 14}s`, animationDirection: ri % 2 ? 'reverse' : 'normal' }}>
            {members.map((n, i) => {
              const a = (i / Math.max(1, members.length)) * Math.PI * 2 + ri;
              const x = 50 + r * 100 * Math.cos(a);
              const y = 50 + r * 100 * Math.sin(a);
              return (
                <span
                  key={n + i}
                  className={s.sat}
                  style={{ left: `${x}%`, top: `${y}%`, animationDuration: `${38 + ri * 14}s`, animationDirection: ri % 2 ? 'normal' : 'reverse' }}
                >
                  {n.slice(0, 2).toUpperCase()}
                </span>
              );
            })}
          </div>
        );
      })}
      <span className={s.orbitCore} data-total={total} />
    </div>
  );
}

/* ---------------- uPlot-free sparkline (tiny) ---------------- */
export function Spark({ values, color = 'var(--accent)', height = 36 }: { values: number[]; color?: string; height?: number }) {
  const w = 120;
  const max = Math.max(1, ...values);
  const pts = values.map((v, i) => `${(i / Math.max(1, values.length - 1)) * w},${height - (v / max) * (height - 4) - 2}`).join(' ');
  return (
    <svg width="100%" height={height} viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" aria-hidden="true">
      <polyline points={pts} fill="none" stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}
