import { Check, Copy, Eye, EyeOff } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import { useState, type InputHTMLAttributes, type ReactNode } from 'react';
import type { UserStatus } from '@/api/types';
import { useI18n } from '@/i18n';
import { toast } from '@/store/feedback';
import s from './misc.module.css';

export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  }
}

export function CopyButton({ text, label, compact }: { text: string; label?: string; compact?: boolean }) {
  const { t } = useI18n();
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className={`btn btn--sm ${compact ? 'btn--icon' : ''}`}
      aria-label={label ?? t('common.copy')}
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          toast.success(t('common.copied'));
          setTimeout(() => setDone(false), 1600);
        }
      }}
    >
      {done ? <Check size={16} /> : <Copy size={16} />}
      {!compact && <span>{done ? t('common.copied') : t('common.copy')}</span>}
    </button>
  );
}

export function QR({ value, size = 168, label }: { value: string; size?: number; label: string }) {
  return (
    <div className={s.qr} role="img" aria-label={label}>
      <QRCodeSVG value={value} size={size} bgColor="#ffffff" fgColor="#07080C" level="M" marginSize={0} />
    </div>
  );
}

export function StatusChip({ status }: { status: UserStatus }) {
  const { t } = useI18n();
  return (
    <span className={`chip chip--${status}`}>
      <span className="chip__dot" />
      {t(`users.status.${status}`)}
    </span>
  );
}

export function Skeleton({ w = '100%', h = 16, r }: { w?: number | string; h?: number | string; r?: number }) {
  return <span className="skel" style={{ display: 'block', width: w, height: h, borderRadius: r }} aria-hidden="true" />;
}

export function PageHeader({ eyebrow, title, subtitle, actions }: { eyebrow?: ReactNode; title: ReactNode; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <header className={s.pageHeader}>
      <div className={s.phText}>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1 className={`${s.phTitle} display`}>{title}</h1>
        {subtitle && <p className={s.phSub}>{subtitle}</p>}
      </div>
      {actions && <div className={s.phActions}>{actions}</div>}
    </header>
  );
}

export function Section({ title, children, actions, className }: { title?: ReactNode; children: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <section className={`glass ${s.section} ${className ?? ''}`}>
      {(title || actions) && (
        <div className={s.sectionHead}>
          {title && <h2 className={`${s.sectionTitle} display`}>{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useI18n();
  return (
    <div className={s.error} role="alert">
      <strong>{t('common.error')}</strong>
      <span>{(error as Error)?.message || t('common.errorBody')}</span>
      {onRetry && (
        <button className="btn btn--sm" onClick={onRetry}>
          {t('common.retry')}
        </button>
      )}
    </div>
  );
}

/* ---------- Empty states with hand-made SVG illustrations ---------- */
type Illo = 'passes' | 'gates' | 'certs' | 'search' | 'logs';

function Illustration({ kind }: { kind: Illo }) {
  const common = { width: 180, height: 140, viewBox: '0 0 180 140', fill: 'none', 'aria-hidden': true } as const;
  if (kind === 'passes')
    return (
      <svg {...common}>
        <ellipse cx="90" cy="124" rx="60" ry="6" fill="var(--line)" />
        <g className={s.float1}>
          <rect x="38" y="30" width="86" height="56" rx="12" fill="var(--panel-3)" stroke="var(--line-2)" transform="rotate(-10 81 58)" />
        </g>
        <g className={s.float2}>
          <rect x="54" y="40" width="88" height="58" rx="12" fill="var(--panel-solid)" stroke="var(--accent)" strokeOpacity=".6" />
          <circle cx="54" cy="69" r="6" fill="var(--bg)" />
          <circle cx="142" cy="69" r="6" fill="var(--bg)" />
          <line x1="62" y1="69" x2="134" y2="69" stroke="var(--line-2)" strokeDasharray="3 4" />
          <rect x="66" y="50" width="38" height="6" rx="3" fill="var(--text)" opacity=".8" />
          <rect x="66" y="78" width="24" height="5" rx="2.5" fill="var(--muted)" />
          <circle cx="124" cy="85" r="7" stroke="var(--accent)" strokeWidth="3" strokeDasharray="30 44" />
        </g>
        <circle cx="150" cy="26" r="3" fill="var(--violet)" className={s.twinkle} />
        <circle cx="28" cy="96" r="2" fill="var(--accent)" className={s.twinkle} />
      </svg>
    );
  if (kind === 'gates')
    return (
      <svg {...common}>
        <ellipse cx="90" cy="124" rx="64" ry="6" fill="var(--line)" />
        <path d="M50 120 V60 a40 40 0 0 1 80 0 V120" stroke="var(--line-2)" strokeWidth="2" />
        <path d="M64 120 V64 a26 26 0 0 1 52 0 V120" stroke="var(--violet)" strokeOpacity=".7" strokeWidth="2" strokeDasharray="4 5" className={s.dash} />
        <circle cx="90" cy="88" r="9" fill="var(--accent)" className={s.float2} />
        <circle cx="90" cy="88" r="18" stroke="var(--accent)" strokeOpacity=".35" className={s.float2} />
      </svg>
    );
  if (kind === 'certs')
    return (
      <svg {...common}>
        <ellipse cx="90" cy="124" rx="56" ry="6" fill="var(--line)" />
        <g className={s.float1}>
          <path d="M90 18 L128 32 V64 C128 90 110 106 90 114 C70 106 52 90 52 64 V32 Z" fill="var(--panel-3)" stroke="var(--line-2)" />
          <path d="M74 64 l11 11 l22 -24" stroke="var(--accent)" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />
        </g>
      </svg>
    );
  if (kind === 'logs')
    return (
      <svg {...common}>
        <rect x="34" y="26" width="112" height="84" rx="14" fill="var(--panel-solid)" stroke="var(--line-2)" />
        <rect x="48" y="44" width="44" height="5" rx="2.5" fill="var(--accent)" opacity=".8" />
        <rect x="48" y="58" width="70" height="5" rx="2.5" fill="var(--muted)" opacity=".6" />
        <rect x="48" y="72" width="56" height="5" rx="2.5" fill="var(--muted)" opacity=".4" />
        <rect x="48" y="86" width="8" height="10" fill="var(--accent)" className={s.twinkle} />
      </svg>
    );
  return (
    <svg {...common}>
      <ellipse cx="90" cy="124" rx="50" ry="6" fill="var(--line)" />
      <circle cx="82" cy="62" r="30" stroke="var(--line-2)" strokeWidth="3" fill="var(--panel-2)" className={s.float1} />
      <path d="M104 84 L126 106" stroke="var(--violet)" strokeWidth="7" strokeLinecap="round" />
      <path d="M72 56 h20 M72 68 h12" stroke="var(--muted)" strokeWidth="4" strokeLinecap="round" />
    </svg>
  );
}

export function EmptyState({ kind, title, body, action }: { kind: Illo; title: string; body?: string; action?: ReactNode }) {
  return (
    <div className={s.empty}>
      <Illustration kind={kind} />
      <h3 className="display">{title}</h3>
      {body && <p>{body}</p>}
      {action}
    </div>
  );
}

export function Field({ label, hint, error, children, optional }: { label: string; hint?: string; error?: string; children: ReactNode; optional?: boolean }) {
  const { t } = useI18n();
  return (
    <label className="field">
      <span className="field__label">
        {label}
        {optional && <small>{t('common.optional')}</small>}
      </span>
      {children}
      {error ? <span className="field__error">{error}</span> : hint ? <span className="field__hint">{hint}</span> : null}
    </label>
  );
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return <button type="button" role="switch" aria-checked={checked} aria-label={label} className="switch" onClick={() => onChange(!checked)} />;
}

/** Password field with a show/hide (eye) toggle. Always laid out LTR. */
export function PasswordInput({ className, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  const { t } = useI18n();
  const [show, setShow] = useState(false);
  return (
    <span className="pw-field" dir="ltr">
      <input {...props} type={show ? 'text' : 'password'} className={`input ltr ${className ?? ''}`} autoCapitalize="none" autoCorrect="off" spellCheck={false} />
      <button
        type="button"
        className="pw-field__eye"
        onClick={(e) => {
          e.preventDefault();
          setShow((x) => !x);
        }}
        aria-label={show ? t('login.hidePassword') : t('login.showPassword')}
        aria-pressed={show}
        title={show ? t('login.hidePassword') : t('login.showPassword')}
      >
        {show ? <EyeOff size={18} /> : <Eye size={18} />}
      </button>
    </span>
  );
}
