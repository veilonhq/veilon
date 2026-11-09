import { Minus, Plus } from 'lucide-react';
import { useEffect, useRef, useState, type MouseEvent, type PointerEvent } from 'react';
import { Switch } from '@/components/misc';
import { useI18n } from '@/i18n';
import s from './Users.module.css';

/** Persian / Arabic-Indic digits to ASCII; keeps one decimal point when allowed, drops everything else. */
const clean = (v: string, decimals: boolean) => {
  const a = v
    .replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 0x06f0))
    .replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 0x0660))
    .replace(/[٫,]/g, '.');
  if (!decimals) return a.replace(/\D/g, '');
  const [i, ...f] = a.replace(/[^\d.]/g, '').split('.');
  return f.length ? `${i}.${f.join('').slice(0, 2)}` : i;
};
const r2 = (v: number) => Math.round(v * 100) / 100;

const HOLD_DELAY = 380;
const HOLD_RATE = 70;

interface Props {
  label: string;
  /** 0 = unlimited */
  value: number;
  onChange: (v: number) => void;
  unit: string;
  /** allow fractions (two places), e.g. 0.5 GB; buttons still step by 1 */
  decimals?: boolean;
  /** value restored when "unlimited" is switched off and nothing was set before */
  fallback?: number;
  /** optional quick picks shown under the field */
  presets?: number[];
  hint?: string;
}

/**
 * A limit typed freely between − / + buttons (step 1, hold to repeat), with
 * an "unlimited" switch. 0 means unlimited; the buttons never go below 1 and
 * there is no upper cap.
 */
export function LimitField({ label, value, onChange, unit, decimals = false, fallback = 10, presets, hint }: Props) {
  const { t, n } = useI18n();
  const unlimited = value <= 0;
  const [draft, setDraft] = useState(unlimited ? '' : String(value));
  const last = useRef(unlimited ? fallback : value);
  const cur = useRef(value);
  cur.current = value;
  const timer = useRef<number | null>(null);

  const stop = () => {
    if (timer.current !== null) {
      clearTimeout(timer.current);
      clearInterval(timer.current);
      timer.current = null;
    }
  };
  useEffect(() => {
    if (value > 0) last.current = value;
    setDraft((d) => (d !== '' && Number(d) === value ? d : value > 0 ? String(value) : ''));
  }, [value]);
  useEffect(() => stop, []);

  const bump = (d: number) => {
    const v = cur.current;
    const next = d < 0 && v <= 1 ? v : Math.max(1, r2(v + d));
    if (next === v) return stop();
    cur.current = next;
    onChange(next);
  };
  const press = (d: number) => (e: PointerEvent<HTMLButtonElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    stop();
    bump(d);
    timer.current = window.setTimeout(() => {
      timer.current = window.setInterval(() => bump(d), HOLD_RATE);
    }, HOLD_DELAY);
  };
  // keyboard (Enter / Space) arrives as a click with detail 0; pointer presses are handled above
  const key = (d: number) => (e: MouseEvent<HTMLButtonElement>) => {
    if (e.detail === 0) bump(d);
  };
  const hold = (d: number) => ({ onPointerDown: press(d), onPointerUp: stop, onPointerLeave: stop, onPointerCancel: stop, onClick: key(d), onContextMenu: (e: MouseEvent) => e.preventDefault() });

  const setUnlimited = (on: boolean) => {
    stop();
    onChange(on ? 0 : last.current > 0 ? last.current : fallback);
  };

  return (
    <div className={s.fs}>
      <div className={s.speedHead}>
        <span className="field__label">{label}</span>
        <label className={s.speedUnl}>
          <span>{t('common.unlimited')}</span>
          <Switch checked={unlimited} onChange={setUnlimited} label={`${label}: ${t('common.unlimited')}`} />
        </label>
      </div>
      <div className={`${s.stepper} ${s.speedStep} ${unlimited ? s.speedOff : ''}`} dir="ltr" role="group" aria-label={label}>
        <button type="button" className={s.stepBtn} {...hold(-1)} disabled={unlimited || value <= 1} aria-label={t('form.decrease')}>
          <Minus size={16} />
        </button>
        <span className={s.speedVal}>
          <input
            className={`num ${s.speedInput}`}
            type="text"
            inputMode={decimals ? 'decimal' : 'numeric'}
            autoComplete="off"
            value={unlimited ? '∞' : draft}
            disabled={unlimited}
            aria-label={`${label} (${unit})`}
            onChange={(e) => {
              const raw = clean(e.target.value, decimals);
              setDraft(raw);
              const v = Number(raw);
              if (raw !== '' && Number.isFinite(v) && v > 0 && (decimals || v >= 1)) onChange(decimals ? r2(v) : Math.round(v));
            }}
            onBlur={() => setDraft(String(value > 0 ? value : 1))}
            onKeyDown={(e) => {
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                bump(1);
              } else if (e.key === 'ArrowDown') {
                e.preventDefault();
                bump(-1);
              }
            }}
          />
          <span className={s.speedUnit}>{unit}</span>
        </span>
        <button type="button" className={s.stepBtn} {...hold(1)} disabled={unlimited} aria-label={t('form.increase')}>
          <Plus size={16} />
        </button>
      </div>
      {presets && presets.length > 0 && (
        <div className={s.quickPicks}>
          {presets.map((p) => (
            <button type="button" key={p} className="pill" aria-pressed={value === p} onClick={() => onChange(p)}>
              {`${n(p)} ${unit}`}
            </button>
          ))}
        </div>
      )}
      {hint && <span className="field__hint">{hint}</span>}
    </div>
  );
}
