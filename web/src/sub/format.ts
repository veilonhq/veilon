/**
 * Pure helpers of the subscription page (no store, no DOM), shared with the
 * node tests. Numbers and dates follow the admin panel: Persian digits and
 * the Persian calendar for fa, the panel's time zone for every date, and a
 * plain space instead of the zero-width non-joiner Intl puts in Persian.
 */
export type Lang = 'fa' | 'en';
export type Status = 'active' | 'disabled' | 'limited' | 'expired' | 'on_hold';
export type Tone = 'accent' | 'warn' | 'danger' | 'muted' | 'info';

export interface SubLink {
  name: string;
  protocol: string;
  network?: string;
  security?: string;
  uri?: string;
  conf_url?: string;
}

/** The JSON of ?format=status (also inlined as window.__VEILON_SUB__.data). */
export interface SubData {
  username: string;
  status: Status | string;
  up: number;
  down: number;
  used: number;
  limit: number;
  expire_at: string | null;
  on_hold_days?: number | null;
  last_online_at: string | null;
  /** the admin panel's online rule, decided by the server */
  online?: boolean;
  sub_url: string;
  title: string;
  update_hours: number;
  timezone: string;
  links: SubLink[];
  /** the admin's description for the user (absent = no card) */
  description?: string;
}

/** The JSON of ?format=live: the parts the open page refreshes. */
export type LiveData = Pick<SubData, 'status' | 'up' | 'down' | 'used' | 'limit' | 'expire_at' | 'on_hold_days' | 'last_online_at' | 'online'>;

/** How often the open page refreshes status, usage and online (only while visible). */
export const LIVE_POLL_MS = 5_000;

export function liveURL(pathname: string): string {
  return `${pathname}?format=live`;
}

const LIVE_KEYS = ['status', 'up', 'down', 'used', 'limit', 'expire_at', 'on_hold_days', 'last_online_at', 'online'] as const;

/** The page data with a ?format=live answer applied (a malformed answer changes nothing). */
export function mergeLive(d: SubData, l: unknown): SubData {
  if (!l || typeof l !== 'object' || typeof (l as LiveData).status !== 'string') return d;
  const next: SubData = { ...d };
  let changed = false;
  for (const k of LIVE_KEYS) {
    if (!(k in l)) continue;
    const v = (l as Record<string, unknown>)[k];
    if ((next as unknown as Record<string, unknown>)[k] !== v) {
      (next as unknown as Record<string, unknown>)[k] = v;
      changed = true;
    }
  }
  return changed ? next : d;
}

const ZWNJ = /\u200c/g;
export const plain = (v: string) => v.replace(ZWNJ, ' ');

export function numLocale(lang: Lang) {
  return lang === 'fa' ? 'fa-IR' : 'en-US';
}
export function dateLocale(lang: Lang) {
  return lang === 'fa' ? 'fa-IR-u-ca-persian' : 'en-GB';
}

export function formatNumber(lang: Lang, n: number, opts?: Intl.NumberFormatOptions) {
  return new Intl.NumberFormat(numLocale(lang), opts).format(n);
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
/** [value, unit] in binary units, as the admin panel shows traffic. */
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

function validTz(tz: string | undefined) {
  if (!tz) return undefined;
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: tz });
    return tz;
  } catch {
    return undefined;
  }
}

/** Date (and time) in the panel's zone. */
export function formatDate(lang: Lang, iso: string, tz: string | undefined, withTime = false) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  const o: Intl.DateTimeFormatOptions = { year: 'numeric', month: 'short', day: 'numeric', timeZone: validTz(tz) };
  if (withTime) Object.assign(o, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
  return plain(new Intl.DateTimeFormat(dateLocale(lang), o).format(d));
}

/** 0..1 of the data limit, or null when unlimited. */
export function usageRatio(d: Pick<SubData, 'used' | 'limit'>): number | null {
  if (!d.limit) return null;
  return Math.max(0, Math.min(1, d.used / d.limit));
}

export type Expiry =
  | { kind: 'none' }
  | { kind: 'hold'; days: number }
  | { kind: 'future'; days: number }
  | { kind: 'past'; days: number };

const DAY = 86_400_000;
/** Whole days left (rounded up, so "expires in 3 h" still reads 1 day). */
export function expiryOf(d: Pick<SubData, 'expire_at' | 'on_hold_days'>, now = Date.now()): Expiry {
  if (!d.expire_at) return d.on_hold_days ? { kind: 'hold', days: d.on_hold_days } : { kind: 'none' };
  const ms = new Date(d.expire_at).getTime() - now;
  if (Number.isNaN(ms)) return { kind: 'none' };
  if (ms <= 0) return { kind: 'past', days: Math.floor(-ms / DAY) };
  return { kind: 'future', days: Math.ceil(ms / DAY) };
}

export function knownStatus(s: string): Status {
  return (['active', 'disabled', 'limited', 'expired', 'on_hold'] as const).includes(s as Status) ? (s as Status) : 'disabled';
}

/** Colour of the usage ring. */
export function usageTone(d: Pick<SubData, 'status' | 'used' | 'limit' | 'expire_at' | 'on_hold_days'>, now = Date.now()): Tone {
  const st = knownStatus(d.status);
  if (st === 'disabled') return 'muted';
  if (st === 'expired' || st === 'limited') return 'danger';
  if (st === 'on_hold') return 'info';
  const r = usageRatio(d);
  const e = expiryOf(d, now);
  if ((r !== null && r >= 0.9) || (e.kind === 'future' && e.days <= 2)) return 'danger';
  if ((r !== null && r >= 0.75) || (e.kind === 'future' && e.days <= 7)) return 'warn';
  return 'accent';
}

/** Short labels for a link: protocol, then transport and security when they say something. */
export function linkChips(l: SubLink): string[] {
  const names: Record<string, string> = {
    vless: 'VLESS', vmess: 'VMess', trojan: 'Trojan', shadowsocks: 'Shadowsocks', hysteria: 'Hysteria2', hysteria2: 'Hysteria2',
    tuic: 'TUIC', shadowtls: 'ShadowTLS', anytls: 'AnyTLS', naive: 'NaiveProxy', mixed: 'Mixed', mtproto: 'MTProto', masque: 'MASQUE', wireguard: 'WireGuard', amneziawg: 'AmneziaWG', socks: 'SOCKS', http: 'HTTP',
  };
  const out = [names[l.protocol] ?? l.protocol.toUpperCase()];
  if (l.network && l.network !== 'tcp' && l.network !== 'udp') out.push(l.network.toUpperCase() === 'XHTTP' ? 'XHTTP' : l.network);
  if (l.security && l.security !== 'none') out.push(l.security === 'reality' ? 'Reality' : l.security.toUpperCase());
  return out;
}

/** Every share link, one per line ("copy all"). */
export function allLinks(links: SubLink[]) {
  return links.map((l) => l.uri).filter(Boolean).join('\n');
}

/** What the "last online" tile shows: online now (the server's flag, the
 * admin panel's rule), the last time seen, or never connected. */
export function onlineState(d: Pick<SubData, 'online' | 'last_online_at'>): 'online' | 'seen' | 'never' {
  if (d.online) return 'online';
  return d.last_online_at ? 'seen' : 'never';
}

/** A piece of the description: plain text, or a link the page may open. */
export type Piece = { text: string; href?: string };

// http(s) URLs, bare t.me / telegram.me links, and Telegram @handles (not
// the middle of an e-mail address: the @ must not follow a letter or digit).
const LINK_RE = /(https?:\/\/[^\s<>"']+)|((?:t|telegram)\.me\/[A-Za-z0-9_+/-]+[^\s<>"']*)|(^|[^\p{L}\p{N}_.@])@([A-Za-z][A-Za-z0-9_]{3,31})(?![A-Za-z0-9_@.])/gu;
const TRAIL_RE = /[.,;:!?،؛)\]}»'"]+$/u;

/**
 * Splits the description into text and links. Only http(s) hrefs are ever
 * produced; the text stays text (React escapes it).
 */
export function linkify(text: string): Piece[] {
  const out: Piece[] = [];
  const push = (p: Piece) => {
    if (!p.text) return;
    const last = out[out.length - 1];
    if (!p.href && last && !last.href) last.text += p.text;
    else out.push(p);
  };
  let at = 0;
  for (const m of text.matchAll(LINK_RE)) {
    const start = m.index ?? 0;
    push({ text: text.slice(at, start) });
    if (m[1] || m[2]) {
      const raw = m[1] || m[2];
      const tail = raw.match(TRAIL_RE)?.[0] ?? '';
      const link = raw.slice(0, raw.length - tail.length);
      const href = m[1] ? link : 'https://' + link;
      let ok = false;
      try {
        const u = new URL(href);
        ok = (u.protocol === 'https:' || u.protocol === 'http:') && !!u.hostname;
      } catch {
        ok = false;
      }
      push(ok ? { text: link, href } : { text: link });
      push({ text: tail });
    } else {
      push({ text: m[3] });
      push({ text: '@' + m[4], href: 'https://t.me/' + m[4] });
    }
    at = start + m[0].length;
  }
  push({ text: text.slice(at) });
  return out;
}
