import type { EngineName, InboundInput, InboundSettings, Protocol } from '@/api/types';

/** Protocol groups shown in the inbound wizard (order matters). */
export const PROTOCOL_GROUPS: { id: 'proxy' | 'quic' | 'vpn' | 'utility'; protocols: Protocol[] }[] = [
  { id: 'proxy', protocols: ['vless', 'vmess', 'trojan', 'shadowsocks'] },
  { id: 'quic', protocols: ['hysteria', 'tuic', 'masque'] },
  { id: 'vpn', protocols: ['wireguard', 'amneziawg', 'tun'] },
  { id: 'utility', protocols: ['http', 'mixed', 'tunnel', 'mtproto'] },
];

export const ALL_PROTOCOLS: Protocol[] = PROTOCOL_GROUPS.flatMap((g) => g.protocols);

export const PROTO_NAME: Record<Protocol, string> = {
  vless: 'VLESS',
  vmess: 'VMess',
  trojan: 'Trojan',
  shadowsocks: 'Shadowsocks',
  hysteria: 'Hysteria2',
  tuic: 'TUIC',
  masque: 'MASQUE',
  wireguard: 'WireGuard',
  amneziawg: 'AmneziaWG',
  tun: 'TUN',
  http: 'HTTP',
  mixed: 'Mixed',
  tunnel: 'Tunnel',
  mtproto: 'MTProto',
};

export const protoName = (p: string) => PROTO_NAME[p as Protocol] ?? p;

/** Short protocol tag for compact cards. */
const PROTO_SHORT: Partial<Record<Protocol, string>> = { shadowsocks: 'SS', hysteria: 'HY2', wireguard: 'WG', amneziawg: 'AWG', mtproto: 'MTP' };
export const protoShort = (p: string) => PROTO_SHORT[p as Protocol] ?? p.toUpperCase();

/** The process that serves a protocol; mirrors inbounds.Engine on the server. */
export function engineOf(p: Protocol): EngineName {
  if (p === 'tuic') return 'sing-box';
  if (p === 'mtproto') return 'mtg';
  if (p === 'amneziawg') return 'amneziawg';
  return 'xray';
}

export const ENGINE_NAME: Record<string, string> = {
  xray: 'Xray',
  'sing-box': 'sing-box',
  mtg: 'mtg',
  amneziawg: 'AmneziaWG',
};
export const engineName = (e: string) => ENGINE_NAME[e] ?? e;
export const ENGINES: EngineName[] = ['xray', 'sing-box', 'mtg', 'amneziawg'];
/**
 * Engines whose status the UI shows (dashboard core card, logs picker).
 * The other engines still run in the backend; they are only hidden here.
 */
export const VISIBLE_ENGINES: EngineName[] = ['xray'];

export const isV2 = (p: Protocol) => p === 'vless' || p === 'vmess' || p === 'trojan' || p === 'shadowsocks';
export const isQuic = (p: Protocol) => p === 'hysteria' || p === 'tuic' || p === 'masque';
export const isWG = (p: Protocol) => p === 'wireguard' || p === 'amneziawg';
export const hasUsers = (p: Protocol) => p !== 'tunnel' && p !== 'tun';
export const hasPort = (p: Protocol) => p !== 'tun';
export const sniffingApplies = (p: Protocol) => !(p === 'tun' || p === 'tunnel' || p === 'amneziawg' || p === 'mtproto');
/** protocols that need root-level network capability on the host */
export const needsNetAdmin = (p: Protocol) => p === 'tun' || p === 'amneziawg';

/** Which socket types an inbound listens on (mirrors the server's Transports). */
export function transportsOf(d: Pick<InboundInput, 'protocol' | 'network' | 'tls' | 'settings'>): { tcp: boolean; udp: boolean } {
  const s = d.settings;
  switch (d.protocol) {
    case 'vless':
    case 'vmess':
    case 'trojan':
      return { tcp: true, udp: d.network === 'xhttp' && !!d.tls?.alpn.includes('h3') };
    case 'shadowsocks':
      return { tcp: true, udp: true };
    case 'hysteria':
    case 'tuic':
    case 'wireguard':
    case 'amneziawg':
      return { tcp: false, udp: true };
    case 'masque': {
      const a = d.tls?.alpn?.length ? d.tls.alpn : ['h3'];
      return { tcp: a.includes('h2'), udp: a.includes('h3') };
    }
    case 'mixed':
      return { tcp: true, udp: !!s?.udp };
    case 'tunnel': {
      const n = s?.tunnel_network || 'tcp';
      return { tcp: n.includes('tcp'), udp: n.includes('udp') };
    }
    case 'tun':
      return { tcp: false, udp: false };
    default:
      return { tcp: true, udp: false };
  }
}

export function transportLabel(tr: { tcp: boolean; udp: boolean }) {
  if (tr.tcp && tr.udp) return 'TCP+UDP';
  if (tr.udp) return 'UDP';
  if (tr.tcp) return 'TCP';
  return '';
}

export const emptySettings = (): InboundSettings => ({
  obfs_password: '',
  up_mbps: 0,
  down_mbps: 0,
  congestion: '',
  private_key: '',
  public_key: '',
  address: '',
  dns: [],
  keepalive: 0,
  awg: null,
  mtu: 0,
  udp: false,
  target_address: '',
  target_port: 0,
  tunnel_network: '',
  tun_name: '',
  gateway: [],
  routes: [],
  faketls_domain: '',
  pool: [],
  path: '',
});

/** Fill settings fields an older server (or a partial object) may lack. */
export const withSettings = (s: Partial<InboundSettings> | null | undefined): InboundSettings => {
  const base = emptySettings();
  const out = { ...base, ...(s ?? {}) } as InboundSettings;
  for (const k of ['dns', 'gateway', 'routes', 'pool'] as const) if (!Array.isArray(out[k])) out[k] = [];
  return out;
};

/** Port a fresh inbound of this protocol gets by default. */
export function defaultPort(p: Protocol): number {
  const rnd = 20000 + Math.floor(Math.random() * 30000);
  switch (p) {
    case 'vless':
    case 'hysteria':
      return 443;
    case 'wireguard':
      return 51820;
    case 'amneziawg':
      return 51821;
    case 'http':
      return 8080;
    case 'mixed':
      return 1080;
    case 'tun':
      return 0;
    default:
      return rnd;
  }
}
