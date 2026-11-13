import type { InboundAdvanced, InboundInput, Protocol } from '@/api/types';

/** Mirrors of the server's advanced-settings rules (panel/internal/inbounds/advanced.go). */

export const SNIFF_DESTS = ['http', 'tls', 'quic', 'fakedns'] as const;
export const DEFAULT_SNIFF = ['http', 'tls', 'quic'];
export const TLS_VERSIONS = ['1.0', '1.1', '1.2', '1.3'];
export const TCP_CONGESTIONS = ['bbr', 'cubic', 'reno'];
export const KCP_HEADERS = ['none', 'srtp', 'utp', 'wechat-video', 'dtls', 'wireguard', 'dns'];
/** uTLS fingerprints clients can use with TLS. */
export const TLS_FPS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random', 'randomized', 'randomizednoalpn'];

const isV2 = (p: Protocol) => p === 'vless' || p === 'vmess' || p === 'trojan' || p === 'shadowsocks';

/** "n" or "a-b" (a <= b); empty is fine. */
export function rangeOk(s: string | undefined, min = 0, max = 2 ** 30): boolean {
  const v = (s ?? '').replace(/\s/g, '');
  if (!v) return true;
  const m = /^(\d{1,9})(?:-(\d{1,9}))?$/.exec(v);
  if (!m) return false;
  const a = Number(m[1]);
  const b = m[2] ? Number(m[2]) : a;
  return a <= b && a >= min && b <= max;
}

/** HTTP header names are tokens; values have no line breaks. */
export const headerNameOk = (k: string) => /^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,64}$/.test(k);

/** A fallback "dest": a port, host:port or an absolute unix socket path. */
export function fallbackDestOk(d: string): boolean {
  const s = d.trim();
  if (/^\d{1,5}$/.test(s)) return Number(s) >= 1 && Number(s) <= 65535;
  if (s.startsWith('/') || s.startsWith('@')) return s.length > 1 && s.length < 108 && !/\s/.test(s);
  const m = /^(.+):(\d{1,5})$/.exec(s);
  return !!m && Number(m[2]) >= 1 && Number(m[2]) <= 65535 && !/\s/.test(m[1]);
}

/** Where each section applies (matches the server). */
export function applies(d: Pick<InboundInput, 'protocol' | 'network' | 'security' | 'sniffing' | 'vless_encryption'>) {
  const p = d.protocol;
  const v2 = isV2(p);
  const tcpNet = d.network !== 'kcp';
  return {
    sniffing: d.sniffing,
    sockopt: v2,
    proxyProtocol: v2 && tcpNet,
    fallbacks: (p === 'vless' || p === 'trojan') && d.network === 'tcp' && (d.security === 'tls' || d.security === 'reality') && !d.vless_encryption?.enabled,
    tls: v2 && d.security === 'tls',
    reality: d.security === 'reality',
    tcpHeader: v2 && d.network === 'tcp',
    ws: v2 && (d.network === 'ws' || d.network === 'httpupgrade'),
    heartbeat: v2 && d.network === 'ws',
    grpc: v2 && d.network === 'grpc',
    xhttp: v2 && d.network === 'xhttp',
    kcp: v2 && d.network === 'kcp',
    hysteria: p === 'hysteria',
    tuic: p === 'tuic',
    ss: p === 'shadowsocks' && d.network === 'tcp',
    fragment: v2 && tcpNet,
    noise: (v2 && d.network === 'kcp') || p === 'hysteria',
    reverse: p === 'vless',
  };
}

const empty = (o: object | undefined | null): boolean => !o || Object.values(o).every((v) => v === undefined || v === '' || v === 0 || v === false || v === null || (Array.isArray(v) && !v.length) || (typeof v === 'object' && !Array.isArray(v) && empty(v)));

/** Drops what does not apply and empty sections; null when nothing is left (the old config). */
export function cleanAdvanced(d: InboundInput): InboundAdvanced | null {
  const a = d.advanced;
  if (!a) return null;
  const ok = applies(d);
  const out: InboundAdvanced = {};
  if (ok.sniffing && a.sniffing && !empty(a.sniffing)) out.sniffing = a.sniffing;
  if (ok.sockopt && a.sockopt) {
    const s = { ...a.sockopt };
    if (!ok.proxyProtocol) {
      s.accept_proxy_protocol = false;
      s.tcp_fast_open = false;
      s.keepalive_idle = 0;
      s.keepalive_interval = 0;
      s.congestion = '';
    }
    if (!empty(s)) out.sockopt = s;
  }
  if (ok.fallbacks && a.fallbacks?.length) out.fallbacks = a.fallbacks.filter((f) => f.dest.trim());
  if (!out.fallbacks?.length) delete out.fallbacks;
  if (ok.tls && a.tls && !empty({ ...a.tls, ech_config_list: '' })) out.tls = a.tls;
  if (ok.reality && a.reality && !empty({ ...a.reality, mldsa65_verify: '' })) out.reality = a.reality;
  if (ok.tcpHeader && a.tcp && a.tcp.type === 'http' && d.security === 'none') out.tcp = a.tcp;
  if (ok.ws && a.ws) {
    const w = { ...a.ws, heartbeat_period: ok.heartbeat ? a.ws.heartbeat_period : 0 };
    if (!empty(w)) out.ws = w;
  }
  if (ok.grpc && a.grpc && !empty(a.grpc)) out.grpc = a.grpc;
  if (ok.xhttp && a.xhttp && !empty(a.xhttp)) {
    const x = { ...a.xhttp };
    if (x.download && !x.download.address?.trim()) delete x.download;
    if (!empty(x)) out.xhttp = x;
  }
  if (ok.kcp && a.kcp && !empty({ ...a.kcp, header: a.kcp.header === 'none' ? '' : a.kcp.header })) out.kcp = a.kcp;
  if (ok.hysteria && a.hysteria && !empty(a.hysteria)) out.hysteria = a.hysteria;
  if (ok.tuic && a.tuic && !empty(a.tuic)) out.tuic = a.tuic;
  if (ok.ss && a.ss?.no_udp) out.ss = a.ss;
  if (a.masks) {
    const m = { fragment: ok.fragment && a.masks.fragment?.length ? a.masks.fragment : null, noise: ok.noise && a.masks.noise?.rand ? a.masks.noise : null };
    if (m.fragment || m.noise) out.masks = m;
  }
  if (ok.reverse && a.reverse?.enabled) out.reverse = a.reverse;
  return Object.keys(out).length ? out : null;
}

/** Number of advanced settings in use (shown on the collapsed section). */
export function advancedCount(a: InboundAdvanced | null | undefined): number {
  if (!a) return 0;
  return Object.entries(a).filter(([, v]) => (Array.isArray(v) ? v.length > 0 : v && !empty(v as object))).length;
}
