import { ArrowDownToLine, ArrowUpFromLine, CalendarClock, Check, Copy, Download, Hourglass, MessageSquareText, Moon, QrCode, RefreshCw, Sun, Wifi, X } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Backdrop } from '@/components/Backdrop';
import { Logo } from '@/components/Logo';
import {
  allLinks,
  expiryOf,
  formatBytes,
  formatDate,
  formatNumber,
  knownStatus,
  linkChips,
  linkify,
  liveURL,
  LIVE_POLL_MS,
  mergeLive,
  usageRatio,
  usageTone,
  type Lang,
  type SubData,
  type SubLink,
  onlineState,
} from './format';
import { translate, type SubKey } from './i18n';
import s from './sub.module.css';

type Theme = 'dark' | 'light';

const store = {
  get(k: string) {
    try {
      return localStorage.getItem(k);
    } catch {
      return null;
    }
  },
  set(k: string, v: string) {
    try {
      localStorage.setItem(k, v);
    } catch {
      /* storage blocked */
    }
  },
};

export const THEME_KEY = 'veilon.sub.theme';
export const LANG_KEY = 'veilon.sub.lang';

function applyTheme(t: Theme) {
  document.documentElement.dataset.theme = t;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', t === 'dark' ? '#07080C' : '#F2F1EC');
}
function applyLang(l: Lang) {
  document.documentElement.lang = l;
  document.documentElement.dir = l === 'fa' ? 'rtl' : 'ltr';
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  }
}

type T = (k: SubKey, v?: Record<string, string>) => string;

function CopyBtn({ text, label, t, wide, primary, onCopied }: { text: string; label: string; t: T; wide?: string; primary?: boolean; onCopied: () => void }) {
  const [done, setDone] = useState(false);
  const timer = useRef<number>();
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <button
      type="button"
      className={`btn btn--sm ${primary ? 'btn--primary' : ''} ${wide ? s.wideBtn : 'btn--icon'}`}
      aria-label={label}
      title={wide ? undefined : label}
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          onCopied();
          window.clearTimeout(timer.current);
          timer.current = window.setTimeout(() => setDone(false), 1600);
        }
      }}
    >
      {done ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
      {wide && (
        <span className={s.btnLabel}>
          {/* both labels hold the width, so the button never resizes */}
          <span className={done ? s.hidden : undefined}>{wide}</span>
          <span className={done ? undefined : s.hidden} aria-hidden={!done}>
            {t('common.copied')}
          </span>
        </span>
      )}
    </button>
  );
}

function QRDialog({ value, title, onClose, t }: { value: string; title: string; onClose: () => void; t: T }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={s.dialog}
      aria-label={`${t('qr.title')}: ${title}`}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) ref.current?.close();
      }}
    >
      <div className={`glass ${s.dialogBody}`}>
        <div className={s.dialogHead}>
          <span className={`${s.dialogTitle} ltr`} dir="auto">
            {title}
          </span>
          <button type="button" className="btn btn--sm btn--icon btn--ghost" aria-label={t('qr.close')} onClick={() => ref.current?.close()} autoFocus>
            <X size={18} aria-hidden="true" />
          </button>
        </div>
        <div className={s.qrBig} role="img" aria-label={`${t('qr.title')}: ${title}`}>
          <QRCodeSVG value={value} size={264} bgColor="#ffffff" fgColor="#07080C" level="M" marginSize={0} />
        </div>
      </div>
    </dialog>
  );
}

function Ring({ ratio, tone, label, children }: { ratio: number | null; tone: string; label: string; children: ReactNode }) {
  const size = 188;
  const stroke = 14;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const v = ratio ?? 1;
  return (
    <div className={s.ring} role="img" aria-label={label}>
      <svg viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--line)" strokeWidth={stroke} />
        {ratio === null ? (
          <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={`var(--${tone})`} strokeWidth={stroke} strokeLinecap="round" strokeDasharray={`3 ${c / 48 - 3}`} opacity={0.75} />
        ) : (
          <circle
            className={s.ringArc}
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={`var(--${tone})`}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={`${c} ${c}`}
            strokeDashoffset={c * (1 - Math.max(v, 0.004))}
          />
        )}
      </svg>
      <div className={s.ringInner} aria-hidden="true">
        {children}
      </div>
    </div>
  );
}

function Stat({ icon, label, value, sub, wide }: { icon: ReactNode; label: string; value: ReactNode; sub?: ReactNode; wide?: boolean }) {
  return (
    <div className={`${s.stat} ${wide ? s.statWide : ''}`}>
      <span className={s.statIcon} aria-hidden="true">
        {icon}
      </span>
      <div className={s.statText}>
        <span className={s.statLabel}>{label}</span>
        <span className={`${s.statValue} num`}>{value}</span>
        {sub && <span className={s.statSub}>{sub}</span>}
      </div>
    </div>
  );
}

/** Keeps status, usage and online fresh while the page is visible. */
function useLive(initial: SubData): SubData {
  const [data, setData] = useState(initial);
  useEffect(() => {
    if (import.meta.env.DEV && !/\/[^/]+\/[^/]+/.test(location.pathname)) return; // dev sample
    let timer = 0;
    let ctl: AbortController | null = null;
    const load = async () => {
      ctl?.abort();
      ctl = new AbortController();
      try {
        const r = await fetch(liveURL(location.pathname), { headers: { Accept: 'application/json' }, cache: 'no-store', signal: ctl.signal });
        if (r.ok) {
          const l: unknown = await r.json();
          setData((d) => mergeLive(d, l));
        }
      } catch {
        /* offline or aborted: keep what is shown */
      }
    };
    const stop = () => {
      window.clearInterval(timer);
      timer = 0;
      ctl?.abort();
    };
    const start = () => {
      if (timer || document.visibilityState !== 'visible') return;
      timer = window.setInterval(load, LIVE_POLL_MS);
    };
    const onVis = () => {
      if (document.visibilityState === 'visible') {
        load();
        start();
      } else stop();
    };
    start();
    document.addEventListener('visibilitychange', onVis);
    return () => {
      document.removeEventListener('visibilitychange', onVis);
      stop();
    };
  }, []);
  return data;
}

export function SubApp({ data: initial, serverLang }: { data: SubData; serverLang: Lang }) {
  const data = useLive(initial);
  const [theme, setThemeState] = useState<Theme>(() => (store.get(THEME_KEY) === 'light' ? 'light' : 'dark'));
  const [lang, setLangState] = useState<Lang>(() => {
    const l = store.get(LANG_KEY);
    return l === 'fa' || l === 'en' ? l : serverLang;
  });
  const [qr, setQr] = useState<{ value: string; title: string } | null>(null);
  const [announce, setAnnounce] = useState('');
  const t: T = useCallback((k, v) => translate(lang, k, v), [lang]);

  useEffect(() => applyTheme(theme), [theme]);
  useEffect(() => {
    applyLang(lang);
    document.title = translate(lang, 'meta.title', { name: data.username });
  }, [lang, data.username]);

  const setTheme = (v: Theme) => {
    store.set(THEME_KEY, v);
    setThemeState(v);
  };
  const setLang = (v: Lang) => {
    store.set(LANG_KEY, v);
    setLangState(v);
  };
  const copied = () => {
    setAnnounce('');
    window.setTimeout(() => setAnnounce(t('common.copied')), 30);
  };

  const status = knownStatus(data.status);
  const ratio = usageRatio(data);
  const tone = usageTone(data);
  const exp = useMemo(() => expiryOf(data), [data]);
  const tz = data.timezone || undefined;
  const n = (v: number) => formatNumber(lang, v);
  const bytes = (v: number) => formatBytes(lang, v);
  const usedLabel = ratio === null ? t('usage.ring.unlimited', { used: bytes(data.used) }) : t('usage.ring', { used: bytes(data.used), total: bytes(data.limit) });

  let daysValue: ReactNode;
  let daysSub: ReactNode = null;
  if (exp.kind === 'future') {
    daysValue = (
      <>
        {n(exp.days)} <small>{t('usage.days.unit')}</small>
      </>
    );
  } else if (exp.kind === 'past') daysValue = t('usage.days.past');
  else if (exp.kind === 'hold') daysValue = t('usage.days.hold', { days: n(exp.days) });
  else daysValue = '∞';
  if (exp.kind === 'none') daysSub = t('usage.days.none');

  const expiryLabel = exp.kind === 'past' ? t('usage.expired.on') : t('usage.expiry');
  const expiryValue = data.expire_at ? formatDate(lang, data.expire_at, tz) : exp.kind === 'hold' ? t('usage.expiry.hold') : t('usage.expiry.none');
  const seen = onlineState(data);
  const onlineValue =
    seen === 'online' ? (
      <span className={s.onlineTag}>
        <span className={s.onlineDot} aria-hidden="true" />
        {t('usage.online.now')}
      </span>
    ) : seen === 'seen' && data.last_online_at ? (
      formatDate(lang, data.last_online_at, tz, true)
    ) : (
      t('usage.online.never')
    );

  const h = data.update_hours;
  const updateText = h > 1 ? t('footer.update', { h: n(h) }) : h === 1 ? t('footer.update.one') : t('footer.update.off');
  const note = status !== 'active' ? t(`status.note.${status}` as SubKey) : null;
  const copyable = allLinks(data.links);

  return (
    <>
      <Backdrop />
      <div className={s.page}>
        <header className={s.top}>
          <div className={s.brand}>
            <Logo size={34} />
            <span className={`${s.brandName} display`} dir="auto">
              {data.title}
            </span>
          </div>
          <div className={s.toggles}>
            <div className="seg" role="radiogroup" aria-label={t('header.lang')}>
              <button type="button" role="radio" aria-checked={lang === 'fa'} lang="fa" onClick={() => setLang('fa')}>
                فارسی
              </button>
              <button type="button" role="radio" aria-checked={lang === 'en'} lang="en" onClick={() => setLang('en')}>
                EN
              </button>
            </div>
            <button
              type="button"
              className="btn btn--icon btn--sm"
              aria-label={theme === 'dark' ? t('header.theme.light') : t('header.theme.dark')}
              title={theme === 'dark' ? t('header.theme.light') : t('header.theme.dark')}
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            >
              {theme === 'dark' ? <Sun size={17} aria-hidden="true" /> : <Moon size={17} aria-hidden="true" />}
            </button>
          </div>
        </header>

        <main className={s.main}>
          <section className={s.hero} aria-labelledby="sub-user">
            <span className="eyebrow">{t('header.eyebrow')}</span>
            <div className={s.heroRow}>
              <h1 id="sub-user" className={`${s.username} display ltr`}>
                {data.username}
              </h1>
              <span className={`chip chip--${status} ${s.statusChip}`}>
                <span className="chip__dot" aria-hidden="true" />
                {t(`status.${status}` as SubKey)}
              </span>
            </div>
            {note && (
              <p className={`${s.note} ${s[`note_${status}`] ?? ''}`} role="status">
                {note}
              </p>
            )}
          </section>

          <div className={s.grid}>
            <section className={`glass ${s.card} ${s.usage}`} aria-labelledby="sub-usage">
              <h2 id="sub-usage" className={s.cardTitle}>
                {t('usage.title')}
              </h2>
              <div className={s.usageBody}>
                <Ring ratio={ratio} tone={tone} label={usedLabel}>
                  {ratio === null ? (
                    <>
                      <span className={`${s.ringBig} display`}>∞</span>
                      <span className={s.ringSmall}>{t('usage.used')}</span>
                    </>
                  ) : (
                    <>
                      <span className={`${s.ringBig} display num`}>{formatNumber(lang, ratio, { style: 'percent', maximumFractionDigits: ratio < 0.1 && ratio > 0 ? 1 : 0 })}</span>
                      <span className={s.ringSmall}>{t('usage.used')}</span>
                    </>
                  )}
                </Ring>
                <div className={s.usageNums}>
                  <div className={s.usedLine}>
                    <span className={`${s.usedBig} display num`}>{bytes(data.used)}</span>
                    <span className={s.usedOf}>{ratio === null ? t('usage.unlimited') : t('usage.of', { total: bytes(data.limit) })}</span>
                  </div>
                  {ratio !== null && (
                    <div className={s.bar} aria-hidden="true">
                      <span style={{ width: `${Math.max(ratio * 100, 0.6)}%`, background: `var(--${tone})` }} />
                    </div>
                  )}
                  {ratio !== null && <span className={s.leftLine}>{t('usage.left', { left: bytes(Math.max(0, data.limit - data.used)) })}</span>}
                </div>
              </div>
              <div className={s.stats}>
                <Stat icon={<ArrowUpFromLine size={17} />} label={t('usage.upload')} value={bytes(data.up)} />
                <Stat icon={<ArrowDownToLine size={17} />} label={t('usage.download')} value={bytes(data.down)} />
                <Stat icon={<Hourglass size={17} />} label={t('usage.days')} value={daysValue} sub={daysSub} />
                <Stat icon={<CalendarClock size={17} />} label={expiryLabel} value={expiryValue} />
                <Stat icon={<Wifi size={17} />} label={t('usage.online')} value={onlineValue} wide />
              </div>
            </section>

            <section className={`glass ${s.card} ${s.subCard}`} aria-labelledby="sub-link">
              <h2 id="sub-link" className={s.cardTitle}>
                {t('sub.title')}
              </h2>
              <div className={s.subQr} role="img" aria-label={t('sub.qr')}>
                <QRCodeSVG value={data.sub_url} size={184} bgColor="#ffffff" fgColor="#07080C" level="M" marginSize={0} />
              </div>
              <p className={s.hint}>{t('sub.hint')}</p>
              <div className={s.urlRow}>
                <code className={`${s.url} mono selectable`} dir="ltr" title={data.sub_url}>
                  {data.sub_url}
                </code>
              </div>
              <CopyBtn text={data.sub_url} label={t('sub.copy')} wide={t('sub.copy')} primary t={t} onCopied={copied} />
            </section>
          </div>

          <section className={`glass ${s.card}`} aria-labelledby="sub-configs">
            <div className={s.cardHead}>
              <div>
                <h2 id="sub-configs" className={s.cardTitle}>
                  {t('configs.title')}
                </h2>
                <span className={s.count}>{t('configs.count', { n: n(data.links.length) })}</span>
              </div>
              {copyable && <CopyBtn text={copyable} label={t('configs.copyAll')} wide={t('configs.copyAll')} t={t} onCopied={copied} />}
            </div>
            {data.links.length === 0 ? (
              <p className={s.empty}>{t('configs.empty')}</p>
            ) : (
              <ul className={s.links}>
                {data.links.map((l, i) => (
                  <LinkRow key={i} l={l} t={t} onCopied={copied} onQr={() => l.uri && setQr({ value: l.uri, title: l.name })} />
                ))}
              </ul>
            )}
          </section>

          {data.description?.trim() && (
            <section className={`glass ${s.card} ${s.descCard}`} aria-labelledby="sub-desc">
              <h2 id="sub-desc" className={s.cardTitle}>
                <MessageSquareText size={17} aria-hidden="true" />
                {t('desc.title')}
              </h2>
              <p className={`${s.desc} selectable`} dir="auto">
                {linkify(data.description.trim()).map((p, i) =>
                  p.href ? (
                    <a key={i} href={p.href} target="_blank" rel="noopener noreferrer nofollow" dir="ltr">
                      {p.text}
                    </a>
                  ) : (
                    <span key={i}>{p.text}</span>
                  ),
                )}
              </p>
            </section>
          )}
        </main>

        <footer className={s.footer}>
          <span className={s.footLine}>
            <RefreshCw size={15} aria-hidden="true" />
            {updateText}
          </span>
          {tz && (
            <span className={s.footTz}>
              {t('footer.tz', { tz: '' })}
              <span className="ltr">{tz}</span>
            </span>
          )}
        </footer>
      </div>
      <div className="sr-only" aria-live="polite">
        {announce}
      </div>
      {qr && <QRDialog value={qr.value} title={qr.title} t={t} onClose={() => setQr(null)} />}
    </>
  );
}

function LinkRow({ l, t, onCopied, onQr }: { l: SubLink; t: T; onCopied: () => void; onQr: () => void }) {
  const chips = linkChips(l);
  return (
    <li className={s.link}>
      <div className={s.linkMain}>
        <div className={s.chips}>
          {chips.map((c, i) => (
            <span key={c + i} className={`chip ${i === 0 ? 'chip--violet' : ''} ${s.smallChip}`} dir="ltr">
              {c}
            </span>
          ))}
        </div>
        <span className={`${s.linkName} ltr`} dir="auto" title={l.name}>
          {l.name}
        </span>
      </div>
      <div className={s.linkActions}>
        {l.uri && <CopyBtn text={l.uri} label={t('configs.copy', { name: l.name })} t={t} onCopied={onCopied} />}
        {l.uri && (
          <button type="button" className="btn btn--sm btn--icon" aria-label={t('configs.qr', { name: l.name })} title={t('configs.qr', { name: l.name })} onClick={onQr}>
            <QrCode size={16} aria-hidden="true" />
          </button>
        )}
        {l.conf_url && (
          <a className="btn btn--sm btn--icon" href={l.conf_url} download aria-label={t('configs.download', { name: l.name })} title={t('configs.download', { name: l.name })}>
            <Download size={16} aria-hidden="true" />
          </a>
        )}
      </div>
    </li>
  );
}
