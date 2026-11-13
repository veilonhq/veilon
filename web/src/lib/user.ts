import type { User } from '@/api/types';

export type Tone = 'accent' | 'warn' | 'danger' | 'muted' | 'info';

export function usageRatio(u: Pick<User, 'data_limit' | 'used_traffic'>): number | null {
  if (!u.data_limit) return null;
  return Math.min(1, u.used_traffic / u.data_limit);
}

export function usageTone(u: User): Tone {
  if (u.status === 'disabled') return 'muted';
  if (u.status === 'expired' || u.status === 'limited') return 'danger';
  if (u.status === 'on_hold') return 'info';
  const r = usageRatio(u);
  if (r !== null && r > 0.85) return 'warn';
  return 'accent';
}

export interface ExpiryInfo {
  kind: 'none' | 'hold' | 'future' | 'past';
  ms: number;
  /** 0..1 of a nominal 30-day window, for small progress bars */
  ratio: number;
  tone: Tone;
}

export function expiryInfo(u: Pick<User, 'expire_at' | 'on_hold_days'>): ExpiryInfo {
  if (!u.expire_at) {
    if (u.on_hold_days) return { kind: 'hold', ms: u.on_hold_days * 86_400_000, ratio: 1, tone: 'info' };
    return { kind: 'none', ms: 0, ratio: 1, tone: 'accent' };
  }
  const ms = new Date(u.expire_at).getTime() - Date.now();
  if (ms <= 0) return { kind: 'past', ms: -ms, ratio: 0, tone: 'danger' };
  const days = ms / 86_400_000;
  return { kind: 'future', ms, ratio: Math.min(1, days / 30), tone: days < 3 ? 'danger' : days < 7 ? 'warn' : 'accent' };
}

/** Deterministic decorative barcode widths from a token. */
export function barcode(token: string, bars = 34): number[] {
  const out: number[] = [];
  let h = 2166136261;
  for (let i = 0; i < bars; i++) {
    h ^= token.charCodeAt(i % Math.max(1, token.length));
    h = Math.imul(h, 16777619);
    out.push(1 + (Math.abs(h) % 3));
  }
  return out;
}

export const USERNAME_RE = /^[a-zA-Z0-9_-]{3,32}$/;

/** Custom proxy login for HTTP, Mixed and SOCKS (mirrors users.ValidateProxyLogin). */
export const PROXY_USER_RE = /^[A-Za-z0-9._-]{3,32}$/;
export const PROXY_PASS_RE = /^[A-Za-z0-9._~!@#$%^&*+=-]{8,64}$/;

/** '' when valid (empty = automatic), else the problem: 'chars' | 'auto' | 'short'. */
export function proxyUserProblem(v: string): '' | 'chars' | 'auto' {
  if (!v) return '';
  if (!PROXY_USER_RE.test(v)) return 'chars';
  if (/^[0-9]+\./.test(v)) return 'auto';
  return '';
}
export function proxyPassProblem(v: string): '' | 'chars' | 'short' {
  if (!v) return '';
  if (v.length < 8 || v.length > 64) return 'short';
  return PROXY_PASS_RE.test(v) ? '' : 'chars';
}
export const GB = 1024 ** 3;
