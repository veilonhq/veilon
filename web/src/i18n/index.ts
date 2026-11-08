import { useCallback, useMemo } from 'react';
import fa, { type MessageKey } from './fa';
import en from './en';
import { useUi, type Lang } from '@/store/ui';

const dicts: Record<Lang, Record<MessageKey, string>> = { fa, en };

export type { MessageKey };
export type TFn = (key: MessageKey, vars?: Record<string, string | number>) => string;

export function translate(lang: Lang, key: MessageKey, vars?: Record<string, string | number>): string {
  let s = dicts[lang][key] ?? dicts.en[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      const val = typeof v === 'number' ? formatNumber(lang, v) : v;
      s = s.split(`{${k}}`).join(val);
    }
  }
  return s;
}

const nfCache = new Map<string, Intl.NumberFormat>();
function nf(lang: Lang, opts: Intl.NumberFormatOptions = {}) {
  const k = lang + JSON.stringify(opts);
  let f = nfCache.get(k);
  if (!f) {
    f = new Intl.NumberFormat(lang === 'fa' ? 'fa-IR' : 'en-US', opts);
    nfCache.set(k, f);
  }
  return f;
}

export function formatNumber(lang: Lang, n: number, opts?: Intl.NumberFormatOptions) {
  return nf(lang, opts).format(n);
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
export function splitBytes(n: number): [number, string] {
  if (!n || n < 0) return [0, 'B'];
  const i = Math.min(UNITS.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return [n / Math.pow(1024, i), UNITS[i]];
}
export function formatBytes(lang: Lang, n: number, digits = 1) {
  const [v, u] = splitBytes(n);
  const d = v >= 100 || u === 'B' ? 0 : digits;
  return `${formatNumber(lang, v, { maximumFractionDigits: d, minimumFractionDigits: 0 })} ${u}`;
}
export function formatBps(lang: Lang, bps: number) {
  // bytes per second → bits per second, network-style units
  const bits = bps * 8;
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps'];
  let i = 0;
  let v = bits;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  return `${formatNumber(lang, v, { maximumFractionDigits: v >= 100 ? 0 : 1 })} ${units[i]}`;
}

/**
 * Persian Intl output (relative times, month names) contains zero-width
 * non-joiners ("پس<ZWNJ>فردا"); the UI convention is a plain space everywhere.
 */
const ZWNJ = /\u200c/g;
const plain = (v: string) => v.replace(ZWNJ, ' ');

export function locale(lang: Lang) {
  return lang === 'fa' ? 'fa-IR-u-ca-persian' : 'en-GB';
}

export function formatDate(lang: Lang, iso: string | Date, opts: Intl.DateTimeFormatOptions = { year: 'numeric', month: 'short', day: 'numeric' }) {
  const d = typeof iso === 'string' ? new Date(iso) : iso;
  return plain(new Intl.DateTimeFormat(locale(lang), opts).format(d));
}
export function formatDateTime(lang: Lang, iso: string | Date) {
  return formatDate(lang, iso, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}
export function formatTime(lang: Lang, iso: string | Date) {
  return formatDate(lang, iso, { hour: '2-digit', minute: '2-digit' });
}

/**
 * Full date and time in the panel's time zone: "۱۷ آبان ۱۴۰۵ · ۲۲:۱۵" /
 * "17 November 2026 · 22:15".
 */
export function formatStampParts(lang: Lang, iso: string | Date, timeZone?: string): [string, string] {
  const day = (tz?: string) => formatDate(lang, iso, { year: 'numeric', month: 'long', day: 'numeric', timeZone: tz });
  const hm = (tz?: string) => formatDate(lang, iso, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: tz });
  try {
    return [day(timeZone), hm(timeZone)];
  } catch {
    return [day(), hm()]; // unknown zone name: browser zone
  }
}
export function formatStamp(lang: Lang, iso: string | Date, timeZone?: string) {
  return formatStampParts(lang, iso, timeZone).join(' · ');
}

/** "3 days" / "۳ روز" — a compact human duration. */
export function formatDuration(lang: Lang, ms: number) {
  const abs = Math.abs(ms);
  const min = 60_000,
    hr = 60 * min,
    day = 24 * hr;
  const unit: Intl.NumberFormatOptions['unit'] = abs >= day ? 'day' : abs >= hr ? 'hour' : 'minute';
  const v = abs >= day ? Math.floor(abs / day) : abs >= hr ? Math.floor(abs / hr) : Math.max(1, Math.floor(abs / min));
  return plain(nf(lang, { style: 'unit', unit, unitDisplay: 'long' }).format(v));
}

export function formatRelative(lang: Lang, iso: string) {
  const diff = new Date(iso).getTime() - Date.now();
  const rtf = new Intl.RelativeTimeFormat(lang === 'fa' ? 'fa' : 'en', { numeric: 'auto' });
  const abs = Math.abs(diff);
  const f = (v: number, u: Intl.RelativeTimeFormatUnit) => plain(rtf.format(v, u));
  if (abs < 60_000) return f(Math.round(diff / 1000), 'second');
  if (abs < 3_600_000) return f(Math.round(diff / 60_000), 'minute');
  if (abs < 86_400_000) return f(Math.round(diff / 3_600_000), 'hour');
  if (abs < 30 * 86_400_000) return f(Math.round(diff / 86_400_000), 'day');
  return f(Math.round(diff / (30 * 86_400_000)), 'month');
}

export function useI18n() {
  const lang = useUi((s) => s.lang);
  const t = useCallback<TFn>((key, vars) => translate(lang, key, vars), [lang]);
  return useMemo(
    () => ({
      lang,
      dir: lang === 'fa' ? 'rtl' : 'ltr',
      t,
      n: (v: number, o?: Intl.NumberFormatOptions) => formatNumber(lang, v, o),
      bytes: (v: number, d?: number) => formatBytes(lang, v, d),
      bps: (v: number) => formatBps(lang, v),
      date: (v: string | Date, o?: Intl.DateTimeFormatOptions) => formatDate(lang, v, o),
      dateTime: (v: string | Date) => formatDateTime(lang, v),
      time: (v: string | Date) => formatTime(lang, v),
      duration: (ms: number) => formatDuration(lang, ms),
      relative: (iso: string) => formatRelative(lang, iso),
      stamp: (v: string | Date, tz?: string) => formatStamp(lang, v, tz),
      stampParts: (v: string | Date, tz?: string) => formatStampParts(lang, v, tz),
    }),
    [lang, t],
  );
}
