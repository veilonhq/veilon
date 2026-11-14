/**
 * Per-user firewall entries, checked in the form the same way the server
 * does (panel/internal/userfw; both read panel/internal/userfw/testdata/
 * cases.json in their tests). The server's answer is final: it also
 * normalises what it stores.
 */

export type FwKind = 'domain' | 'full' | 'regexp' | 'ip';
export type FwReason = 'empty' | 'too_long' | 'bad_domain' | 'bad_regexp' | 'bad_ip' | 'too_broad' | 'unsupported' | 'too_many';

export const FW_MAX = 200;
const MAX_LEN = 253;

export type FwResult = { ok: true; kind: FwKind; value: string } | { ok: false; reason: FwReason };

const LABEL = /^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$/;
const PREFIX = /^[a-z][a-z0-9_-]*:/;
const HOST_PORT = /^([a-z0-9_.*-]+):([0-9]{1,5})$/;
const IPISH = /^[0-9.]+$|^[0-9a-f:]*:[0-9a-f:.]*$/;

function domain(s: string): string | null {
  let d = s.trim().toLowerCase();
  if (d.startsWith('*.')) d = d.slice(2);
  if (d.startsWith('.')) d = d.slice(1);
  if (d.endsWith('.')) d = d.slice(0, -1);
  if (!d || d.length > 253) return null;
  let allDigits = true;
  for (const l of d.split('.')) {
    if (!LABEL.test(l)) return null;
    if (/[^0-9]/.test(l)) allDigits = false;
  }
  return allDigits ? null : d;
}

function v4(s: string): number[] | null {
  const p = s.split('.');
  if (p.length !== 4) return null;
  const out: number[] = [];
  for (const x of p) {
    if (!/^[0-9]{1,3}$/.test(x) || (x.length > 1 && x[0] === '0')) return null;
    const n = Number(x);
    if (n > 255) return null;
    out.push(n);
  }
  return out;
}

/** 8 groups of an IPv6 address (embedded IPv4 allowed), or null. */
function v6(s: string): number[] | null {
  if (!s.includes(':') || s.includes('%')) return null;
  let str = s;
  const lastColon = str.lastIndexOf(':');
  const last = str.slice(lastColon + 1);
  if (last.includes('.')) {
    const q = v4(last);
    if (!q) return null;
    str = str.slice(0, lastColon + 1) + ((q[0] << 8) | q[1]).toString(16) + ':' + ((q[2] << 8) | q[3]).toString(16);
  }
  const parts = str.split('::');
  if (parts.length > 2) return null;
  const nums = (x: string) => {
    if (x === '') return [] as number[];
    const o: number[] = [];
    for (const g of x.split(':')) {
      if (!/^[0-9a-f]{1,4}$/.test(g)) return null;
      o.push(parseInt(g, 16));
    }
    return o;
  };
  const h = nums(parts[0]);
  const r = parts.length === 2 ? nums(parts[1]) : [];
  if (!h || !r) return null;
  if (parts.length === 1) return h.length === 8 ? h : null;
  if (h.length + r.length > 7) return null;
  return [...h, ...new Array<number>(8 - h.length - r.length).fill(0), ...r];
}

function isMapped(g: number[]): boolean {
  return g.slice(0, 5).every((x) => x === 0) && g[5] === 0xffff;
}

/** null = not an address at all; otherwise ok or a reason. */
function ipEntry(s: string): FwResult | null {
  let addr = s;
  let bits: number | null = null;
  if (s.includes('/')) {
    const [a, b] = s.split('/', 2);
    addr = a;
    if (!/^[0-9]{1,3}$/.test(b)) return IPISH.test(a) ? { ok: false, reason: 'bad_ip' } : null;
    bits = Number(b);
  }
  addr = addr.replace(/^\[|\]$/g, '');
  if (addr.includes('%')) return IPISH.test(addr.split('%')[0]) ? { ok: false, reason: 'bad_ip' } : null;
  const a4 = v4(addr);
  if (a4) {
    if (bits !== null && bits > 32) return { ok: false, reason: 'bad_ip' };
    if (bits !== null && bits < 8) return { ok: false, reason: 'too_broad' };
    return { ok: true, kind: 'ip', value: s };
  }
  const a6 = v6(addr);
  if (a6) {
    if (bits !== null && bits > 128) return { ok: false, reason: 'bad_ip' };
    if (isMapped(a6)) {
      if (bits !== null && bits - 96 < 8) return { ok: false, reason: bits - 96 < 0 ? 'bad_ip' : 'too_broad' };
    } else if (bits !== null && bits < 16) return { ok: false, reason: 'too_broad' };
    return { ok: true, kind: 'ip', value: s };
  }
  if (bits !== null && IPISH.test(addr)) return { ok: false, reason: 'bad_ip' };
  return null;
}

/** Checks one entry (the value is lightly normalised; the server makes it canonical). */
export function checkEntry(raw: string): FwResult {
  const s = raw.trim();
  if (!s) return { ok: false, reason: 'empty' };
  if (s.length > MAX_LEN) return { ok: false, reason: 'too_long' };
  let low = s.toLowerCase();
  if (low.startsWith('regexp:')) {
    const re = s.slice(7).trim();
    if (!re) return { ok: false, reason: 'bad_regexp' };
    try {
      new RegExp(re);
    } catch {
      return { ok: false, reason: 'bad_regexp' };
    }
    return { ok: true, kind: 'regexp', value: 'regexp:' + re };
  }
  if (low.startsWith('full:')) {
    const d = domain(low.slice(5));
    return d ? { ok: true, kind: 'full', value: 'full:' + d } : { ok: false, reason: 'bad_domain' };
  }
  if (low.startsWith('domain:')) {
    const d = domain(low.slice(7));
    return d ? { ok: true, kind: 'domain', value: d } : { ok: false, reason: 'bad_domain' };
  }
  if (low.includes('://')) {
    low = low.slice(low.indexOf('://') + 3);
    const cut = low.search(/[/?#]/);
    if (cut >= 0) low = low.slice(0, cut);
    const at = low.lastIndexOf('@');
    if (at >= 0) low = low.slice(at + 1);
    if (low.startsWith('[')) {
      const i = low.indexOf(']');
      if (i > 0) low = low.slice(1, i);
    } else if ((low.match(/:/g) ?? []).length === 1) low = low.slice(0, low.lastIndexOf(':'));
  }
  const ip = ipEntry(low);
  if (ip) return ip;
  if (low.includes(':')) {
    const m = HOST_PORT.exec(low);
    if (m) {
      low = m[1];
      const ip2 = ipEntry(low);
      if (ip2) return ip2;
    } else if (PREFIX.test(low)) return { ok: false, reason: 'unsupported' };
    else return { ok: false, reason: 'bad_domain' };
  }
  const d = domain(low);
  if (!d) return { ok: false, reason: IPISH.test(low) ? 'bad_ip' : 'bad_domain' };
  return { ok: true, kind: 'domain', value: d };
}

/** Splits typed or pasted text into entries (lines, spaces, commas, Persian commas, semicolons). */
export function splitEntries(text: string): string[] {
  return text.split(/[\s,،;؛]+/).filter(Boolean);
}

/** Kind of a stored (canonical) entry. */
export function entryKind(v: string): FwKind {
  if (v.startsWith('regexp:')) return 'regexp';
  if (v.startsWith('full:')) return 'full';
  const r = ipEntry(v);
  return r && r.ok ? 'ip' : 'domain';
}

/**
 * Adds typed text to a list: every valid entry not already there (by its
 * normalised form). The first refused entry stops the whole add.
 */
export function addEntries(list: string[], text: string): { list: string[]; added: number; error?: { entry: string; reason: FwReason } } {
  const have = new Set(list.map((x) => {
    const r = checkEntry(x);
    return r.ok ? r.value : x;
  }));
  const next = [...list];
  let added = 0;
  for (const e of splitEntries(text)) {
    const r = checkEntry(e);
    if (!r.ok) return { list, added: 0, error: { entry: e, reason: r.reason } };
    if (have.has(r.value)) continue;
    have.add(r.value);
    next.push(r.value);
    added++;
  }
  if (next.length > FW_MAX) return { list, added: 0, error: { entry: '', reason: 'too_many' } };
  return { list: next, added };
}
