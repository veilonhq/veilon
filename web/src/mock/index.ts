/**
 * Dev mock backend. Implements docs/api-v1.md in memory so the UI is fully
 * clickable without the Go panel. Loaded only when VITE_MOCK=1.
 */
import type { Admin, AuditEntry, GroupInput, Perm, Cert, ConnDevice, ConnIP, Group, Inbound, InboundSettings, Settings, User, UserConnections, UserStatus } from '@/api/types';
import { emptySettings, engineOf, withSettings } from '@/lib/protocols';

const GB = 1024 ** 3;
const DAY = 86_400_000;
const now = () => Date.now();
const iso = (ms: number) => new Date(ms).toISOString();

let seed = 1337;
const rnd = () => {
  seed = (seed * 16807) % 2147483647;
  return (seed - 1) / 2147483646;
};
const pick = <T,>(a: T[]) => a[Math.floor(rnd() * a.length)];
const hex = (n: number) => Array.from({ length: n }, () => Math.floor(rnd() * 16).toString(16)).join('');
const uuid = () => `${hex(8)}-${hex(4)}-4${hex(3)}-${pick(['8', '9', 'a', 'b'])}${hex(3)}-${hex(12)}`;
const b64 = (n: number) => {
  const c = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
  return Array.from({ length: n }, () => c[Math.floor(rnd() * c.length)]).join('');
};

const SERVER = 'edge.veilon.example';

/* ---------- data ---------- */
const HOUR = 3_600_000;
const PUBLIC_IP = { ipv4: '203.0.113.24', ipv6: '2001:db8:4f2a:12::1' };
/** Mirror of the panel's renewal rule: short-lived at 3 days of age, others when a third of the life remains. */
function renewAtOf(c: Cert): string | null {
  if (c.source !== 'acme' || !c.auto_renew) return null;
  const na = new Date(c.not_after).getTime();
  const nb = c.not_before ? new Date(c.not_before).getTime() : na - (c.profile === 'shortlived' ? 160 * HOUR : 90 * DAY);
  const life = na - nb;
  return iso(c.profile === 'shortlived' ? nb + Math.min(3 * DAY, life / 2) : na - life / 3);
}
function mkCert(c: Omit<Cert, 'identifiers' | 'type' | 'renew_at'> & { identifiers?: string[] }): Cert {
  const ids = c.identifiers ?? [c.domain];
  const out: Cert = { http_port: c.source === 'acme' ? 80 : undefined, last_error: '', last_attempt_at: null, profile: '', ...c, identifiers: ids, type: /^\d+\.\d+\.\d+\.\d+$/.test(ids[0]) || ids[0].includes(':') ? 'ip' : 'domain' };
  out.renew_at = renewAtOf(out);
  return out;
}
const certs: Cert[] = [
  mkCert({ id: 4, domain: 'www.bing.com', issuer: 'Veilon self-signed', not_before: iso(now() - 40 * DAY), not_after: iso(now() + 3600 * DAY), auto_renew: false, source: 'self' }),
  mkCert({ id: 1, domain: 'edge.veilon.example', issuer: "Let's Encrypt R11", not_before: iso(now() - 28 * DAY), not_after: iso(now() + 62 * DAY), auto_renew: true, source: 'acme' }),
  mkCert({
    id: 2, domain: 'cdn.veilon.example', issuer: "Let's Encrypt E6", not_before: iso(now() - 79 * DAY), not_after: iso(now() + 11 * DAY), auto_renew: true, source: 'acme',
    last_error: 'acme: authorization failed: 203.0.113.24: Timeout during connect (likely firewall problem)', last_attempt_at: iso(now() - 5 * HOUR),
  }),
  mkCert({
    id: 5, domain: PUBLIC_IP.ipv4, identifiers: [PUBLIC_IP.ipv4, PUBLIC_IP.ipv6], issuer: "Let's Encrypt YE1", not_before: iso(now() - 30 * HOUR), not_after: iso(now() + 130 * HOUR),
    auto_renew: true, source: 'acme', profile: 'shortlived',
  }),
  mkCert({ id: 3, domain: 'legacy.example.net', issuer: 'Sectigo RSA DV', not_before: iso(now() - 150 * DAY), not_after: iso(now() + 214 * DAY), auto_renew: false, source: 'upload' }),
];
function certInUse(id: number) {
  return settings.panel_cert_id === id || inbounds.some((i) => i.tls?.cert_id === id);
}
const certView = (c: Cert): Cert => ({ ...c, renew_at: renewAtOf(c), in_use: certInUse(c.id) });

const inbounds: Inbound[] = [
  {
    id: 1, remark: 'Reality · Main', enabled: true, protocol: 'vless', port: 443, listen: '', network: 'tcp', security: 'reality',
    tls: null,
    reality: { dest: 'www.microsoft.com:443', server_names: ['www.microsoft.com'], private_key: 'kD3Hq0J7l2YkqC6P4tE0rX9mFzN1uWbV8sA5gLhRjTo', public_key: 'Z2r9wQyF1pX7cL0vB3nM6kJ4hG8dS5aE2tU1iO9pRqw', short_ids: ['a1b2c3d4'], fingerprint: 'chrome', spider_x: '/' },
    transport: { path: '', host: '', service_name: '', mode: '' }, flow: 'xtls-rprx-vision', ss_method: '', sniffing: true, address_override: '', settings: emptySettings(), user_count: 0, created_at: iso(now() - 80 * DAY),
  },
  {
    id: 2, remark: 'CDN · WebSocket', enabled: true, protocol: 'vless', port: 8443, listen: '', network: 'ws', security: 'tls',
    tls: { cert_id: 2, sni: 'cdn.veilon.example', alpn: ['http/1.1'], fingerprint: 'chrome', allow_insecure: false }, reality: null,
    transport: { path: '/stream', host: 'cdn.veilon.example', service_name: '', mode: '' }, flow: '', ss_method: '', sniffing: true, address_override: 'cdn.veilon.example', settings: emptySettings(), user_count: 0, created_at: iso(now() - 64 * DAY),
  },
  {
    id: 3, remark: 'Trojan · gRPC', enabled: true, protocol: 'trojan', port: 2053, listen: '', network: 'grpc', security: 'tls',
    tls: { cert_id: 1, sni: 'edge.veilon.example', alpn: ['h2'], fingerprint: 'firefox', allow_insecure: false }, reality: null,
    transport: { path: '', host: '', service_name: 'tunnel', mode: 'gun' }, flow: '', ss_method: '', sniffing: true, address_override: '', settings: emptySettings(), user_count: 0, created_at: iso(now() - 40 * DAY),
  },
  {
    id: 4, remark: 'VMess · Legacy', enabled: false, protocol: 'vmess', port: 8080, listen: '', network: 'httpupgrade', security: 'none',
    tls: null, reality: null, transport: { path: '/up', host: '', service_name: '', mode: '' }, flow: '', ss_method: '', sniffing: false, address_override: '', settings: emptySettings(), user_count: 0, created_at: iso(now() - 120 * DAY),
  },
  {
    id: 5, remark: 'Shadowsocks 2022', enabled: true, protocol: 'shadowsocks', port: 1080, listen: '', network: 'tcp', security: 'none',
    tls: null, reality: null, transport: { path: '', host: '', service_name: '', mode: '' }, flow: '', ss_method: '2022-blake3-aes-128-gcm', sniffing: true, address_override: '', settings: emptySettings(), user_count: 0, created_at: iso(now() - 12 * DAY),
  },
  {
    id: 6, remark: 'XHTTP · Edge', enabled: true, protocol: 'vless', port: 2096, listen: '', network: 'xhttp', security: 'reality',
    tls: null,
    reality: { dest: 'www.cloudflare.com:443', server_names: ['www.cloudflare.com'], private_key: 'mN8bV3cX1zL0kJ9hG7fD5sA4qW2eR6tY8uI0oP1aSdF', public_key: 'Qw3eR5tY7uI9oP0aS2dF4gH6jK8lZ1xC3vB5nM7qWeR', short_ids: ['9f8e7d'], fingerprint: 'safari', spider_x: '/' },
    transport: { path: '/x', host: '', service_name: '', mode: 'auto' }, flow: '', ss_method: '', sniffing: true, address_override: '', settings: emptySettings(), user_count: 0, created_at: iso(now() - 3 * DAY),
  },
];

const st = (x: Partial<InboundSettings>): InboundSettings => ({ ...emptySettings(), ...x });
const noT = { path: '', host: '', service_name: '', mode: '' };
const quicTls = (alpn = ['h3']) => ({ cert_id: null, sni: 'www.bing.com', alpn, fingerprint: 'chrome', allow_insecure: true });
inbounds.push(
  { id: 7, remark: 'Hysteria2 · Fast lane', enabled: true, protocol: 'hysteria', port: 443, listen: '', network: 'udp', security: 'tls', tls: quicTls(), reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({ obfs_password: 'salamander-pass', up_mbps: 50, down_mbps: 200 }), user_count: 0, created_at: iso(now() - 9 * DAY) },
  { id: 8, remark: 'TUIC · Low latency', enabled: true, protocol: 'tuic', port: 8443, listen: '', network: 'udp', security: 'tls', tls: quicTls(), reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({ congestion: 'bbr' }), user_count: 0, created_at: iso(now() - 8 * DAY) },
  { id: 9, remark: 'MASQUE · HTTP/3 IP tunnel for laptops and long names', enabled: true, protocol: 'masque', port: 4433, listen: '', network: 'quic', security: 'tls', tls: quicTls(), reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({ pool: ['10.14.0.1/24', 'fd14::1/64'], mtu: 1280, path: '/.well-known/masque/ip/{target}/{ipproto}/' }), user_count: 0, created_at: iso(now() - 7 * DAY) },
  { id: 10, remark: 'WireGuard · Home', enabled: true, protocol: 'wireguard', port: 51820, listen: '', network: 'udp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({ private_key: 'OKr3cQ0b3oT2p0H0VnH4bW1m2y6Hc2oZf0bqk3m7k2Q=', public_key: 'Hc8Wb4m0m7hX4u9xG1oQ3pC5bN2lK6jT8rV0sA2dF3g=', address: '10.66.66.1/24', dns: ['1.1.1.1', '8.8.8.8'], mtu: 1420, keepalive: 25 }), user_count: 0, created_at: iso(now() - 6 * DAY) },
  { id: 11, remark: 'AmneziaWG · Stealth', enabled: true, protocol: 'amneziawg', port: 51821, listen: '', network: 'udp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: false, address_override: '', settings: st({ private_key: 'mE2kP7vV3s1L0aQ9xR4tY6uI8oP2aS4dF6gH8jK0lZ0=', public_key: 'Zx9Cv8Bn7Mm6Ll5Kk4Jj3Hh2Gg1Ff0Dd9Ss8Aa7Qq6W=', address: '10.67.12.1/24', dns: ['1.1.1.1'], mtu: 1420, keepalive: 25, awg: { jc: 5, jmin: 40, jmax: 70, s1: 86, s2: 31, h1: 1840273647, h2: 928374651, h3: 1203948576, h4: 384756102 } }), user_count: 0, created_at: iso(now() - 5 * DAY) },
  { id: 12, remark: 'TUN · Office subnets', enabled: false, protocol: 'tun', port: 0, listen: '', network: 'tun', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: false, address_override: '', settings: st({ tun_name: 'veilon-tun', gateway: ['10.77.0.1/24'], routes: ['10.10.0.0/16'], mtu: 1500 }), user_count: 0, created_at: iso(now() - 4 * DAY) },
  { id: 13, remark: 'HTTP proxy', enabled: true, protocol: 'http', port: 8080, listen: '', network: 'tcp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({}), user_count: 0, created_at: iso(now() - 3 * DAY) },
  { id: 14, remark: 'Mixed · SOCKS + HTTP', enabled: true, protocol: 'mixed', port: 1081, listen: '', network: 'tcp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: true, address_override: '', settings: st({ udp: true }), user_count: 0, created_at: iso(now() - 2 * DAY) },
  { id: 15, remark: 'SSH forward', enabled: true, protocol: 'tunnel', port: 2222, listen: '', network: 'tcp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: false, address_override: '', settings: st({ target_address: '10.0.0.5', target_port: 22, tunnel_network: 'tcp' }), user_count: 0, created_at: iso(now() - 2 * DAY) },
  { id: 16, remark: 'Telegram · MTProto', enabled: true, protocol: 'mtproto', port: 8444, listen: '', network: 'tcp', security: 'none', tls: null, reality: null, transport: noT, flow: '', ss_method: '', sniffing: false, address_override: '', settings: st({ faketls_domain: 'www.cloudflare.com' }), user_count: 0, created_at: iso(now() - 1 * DAY) },
);

const names = [
  'arash', 'nima', 'sara', 'parisa', 'kian', 'darya', 'mahsa', 'reza', 'ali', 'yasmin', 'omid', 'shirin', 'babak', 'leila', 'navid', 'sepideh', 'farhad', 'mina',
  'kaveh', 'nazanin', 'pouya', 'roya', 'saman', 'tara', 'hamed', 'elham', 'behnam', 'golnaz', 'majid', 'atena', 'milad', 'negar', 'soroush', 'bahar', 'arman', 'yalda',
  'office-hq', 'studio', 'family', 'travel', 'laptop', 'router-home', 'pixel', 'iphone', 'team-dev', 'lab',
];
const notes = ['', '', '', 'تمدید ماهانه', 'Telegram @kian_r', 'خانواده', 'VIP', 'trial', 'pays yearly', 'دفتر مرکزی'];

function mkUser(id: number): User {
  const base = names[(id - 1) % names.length];
  const username = id <= names.length ? base : `${base}${Math.floor(id / names.length) + 1}`;
  const limitGb = pick([0, 10, 20, 30, 50, 50, 100, 100, 200, 500]);
  const r = rnd();
  let status: UserStatus = 'active';
  let used = limitGb ? Math.floor(rnd() * limitGb * GB * 0.95) : Math.floor(rnd() * 380 * GB);
  let expire: string | null = iso(now() + Math.floor(rnd() * 60 + 1) * DAY + Math.floor(rnd() * DAY));
  let onHold: number | null = null;
  if (r < 0.08) {
    status = 'expired';
    expire = iso(now() - Math.floor(rnd() * 20 + 1) * DAY);
  } else if (r < 0.15 && limitGb) {
    status = 'limited';
    used = limitGb * GB;
  } else if (r < 0.2) {
    status = 'disabled';
  } else if (r < 0.25) {
    status = 'on_hold';
    expire = null;
    onHold = pick([7, 14, 30]);
    used = 0;
  } else if (r < 0.35) {
    expire = null;
  }
  const up = Math.floor(used * (0.08 + rnd() * 0.12));
  const created = now() - Math.floor(rnd() * 200 + 1) * DAY;
  const online = status === 'active' && rnd() < 0.38;
  const limits = mockLimits(id, status);
  const ib = inbounds.filter((i) => i.protocol !== 'tun' && i.protocol !== 'tunnel').filter((i) => i.enabled || rnd() < 0.3).filter(() => rnd() < 0.6).map((i) => i.id);
  const token = b64(22);
  return {
    id, username, status, data_limit: limitGb * GB, used_traffic: used, up, down: used - up,
    expire_at: expire, on_hold_days: onHold, reset_strategy: pick(['no_reset', 'no_reset', 'month', 'week']),
    note: pick(notes), inbound_ids: ib.length ? ib : [1], uuid: uuid(), password: b64(16), sub_token: token,
    sub_url: `https://${SERVER}/sub/${token}`,
    online: online && !limits.suspend_reason, last_online_at: status === 'on_hold' ? null : online ? iso(now()) : iso(now() - Math.floor(rnd() * 9 * DAY)),
    created_at: iso(created), updated_at: iso(created + DAY),
    ...limits,
  };
}

/** Deterministic per-user limits (own hash, so the main seed sequence is untouched). */
function mockLimits(id: number, status: UserStatus): Omit<User, keyof Omit<User, 'ip_limit' | 'device_limit' | 'require_hwid' | 'speed_limit_down' | 'speed_limit_up' | 'traffic_multiplier' | 'daily_limit' | 'daily_used' | 'block_torrent' | 'window_from' | 'window_to' | 'window_days' | 'suspend_reason' | 'group_id' | 'admin_id'>> {
  const h = (k: number) => ((id * 2654435761 + k * 40503) >>> 0) % 100;
  const daily = h(5) < 14 ? 2 * GB : 0;
  const win = h(7) < 10;
  const suspend: User['suspend_reason'] = status !== 'active' ? '' : win && h(8) < 50 ? 'window' : daily && h(9) < 40 ? 'daily' : '';
  return {
    ip_limit: h(1) < 30 ? [1, 2, 3][h(11) % 3] : 0,
    device_limit: h(2) < 22 ? [1, 2, 3][h(12) % 3] : 0,
    require_hwid: h(2) < 8,
    speed_limit_down: h(3) < 15 ? [10, 20, 50][h(13) % 3] : 0,
    speed_limit_up: h(3) < 15 ? [5, 10, 20][h(13) % 3] : 0,
    traffic_multiplier: h(4) < 10 ? [1.2, 1.5, 2][h(14) % 3] : 1,
    daily_limit: daily,
    daily_used: suspend === 'daily' ? daily : daily ? Math.floor((h(15) / 100) * daily) : 0,
    block_torrent: h(6) < 25,
    window_from: win ? '08:00' : '',
    window_to: win ? '18:00' : '',
    window_days: win ? 0b1011111 : 0, // bit 0 = Sunday; Friday (bit 5) off
    suspend_reason: suspend,
    group_id: h(10) < 20 ? 1 : h(10) < 32 ? 2 : null,
    admin_id: h(10) % 9 === 0 ? 3 : null,
  };
}

const groups: Group[] = [
  { id: 1, name: 'VIP', color: '#8b7cff', ad_block: true, inbound_ids: [1, 2, 6], defaults: { data_limit: 100 * GB, expire_days: 30, ip_limit: 3, traffic_multiplier: 1 }, users: 0, created_at: iso(now() - 50 * DAY) },
  { id: 2, name: 'خانواده', color: '#34c37b', ad_block: false, inbound_ids: [1, 5], defaults: { data_limit: 50 * GB, expire_days: 30, block_torrent: true }, users: 0, created_at: iso(now() - 30 * DAY) },
  { id: 3, name: 'آزمایشی', color: '#f2a93b', ad_block: false, inbound_ids: [1], defaults: { data_limit: 2 * GB, expire_days: 1, ip_limit: 1, device_limit: 1, speed_limit_down: 10, speed_limit_up: 5 }, users: 0, created_at: iso(now() - 10 * DAY) },
];

/** Connection log per user, generated on first read. */
const conns = new Map<number, UserConnections>();
let connSeq = 1;
function connsOf(u: User): UserConnections {
  let c = conns.get(u.id);
  if (c) return c;
  let x = (u.id * 7919) % 2147483647 || 1;
  const r = () => {
    x = (x * 48271) % 2147483647;
    return (x - 1) / 2147483646;
  };
  const countries = ['IR', 'IR', 'IR', 'DE', 'NL', 'TR', 'FI', 'AE'];
  const nIp = u.status === 'on_hold' ? 0 : 1 + Math.floor(r() * 5);
  const ips: ConnIP[] = Array.from({ length: nIp }, (_, i) => {
    const first = now() - Math.floor(r() * 40 + 1) * DAY - Math.floor(r() * DAY);
    const last = i === 0 && u.online ? now() - 20_000 : Math.min(now(), first + Math.floor(r() * 30) * DAY);
    return {
      id: connSeq++, ip: `${pick2(r, [5, 31, 37, 78, 91, 151, 185, 188])}.${Math.floor(r() * 255)}.${Math.floor(r() * 255)}.${1 + Math.floor(r() * 253)}`,
      country: pick2(r, countries), first_seen: iso(first), last_seen: iso(last), count: 1 + Math.floor(r() * 400), blocked: r() < 0.12, live: i === 0 && u.online,
    };
  });
  const models: Array<[string, string, string]> = [['Android', '14', 'Pixel 8'], ['Android', '13', 'Galaxy A54'], ['iOS', '17.5', 'iPhone 15'], ['Windows', '11', 'Desktop'], ['macOS', '14.4', 'MacBook Air']];
  const nDev = u.status === 'on_hold' ? 0 : Math.floor(r() * 4);
  const devices: ConnDevice[] = Array.from({ length: nDev }, () => {
    const [os, ver, model] = pick2(r, models);
    const first = now() - Math.floor(r() * 60 + 1) * DAY;
    const ip = ips.length ? pick2(r, ips) : null;
    return {
      id: connSeq++, hwid: `${hex2(r, 8)}-${hex2(r, 4)}-${hex2(r, 4)}`, os, os_version: ver, model, user_agent: `v2rayNG/1.9.${Math.floor(r() * 9)}`,
      ip: ip?.ip ?? '', country: ip?.country ?? '', first_seen: iso(first), last_seen: iso(Math.min(now(), first + Math.floor(r() * 50) * DAY)), count: 1 + Math.floor(r() * 120), blocked: r() < 0.1,
    };
  });
  c = { ips, devices };
  conns.set(u.id, c);
  return c;
}
function pick2<T>(r: () => number, a: T[]): T {
  return a[Math.floor(r() * a.length)];
}
function hex2(r: () => number, n: number) {
  return Array.from({ length: n }, () => Math.floor(r() * 16).toString(16)).join('');
}
const LIMIT_DEFAULTS = {
  ip_limit: 0, device_limit: 0, require_hwid: false, speed_limit_down: 0, speed_limit_up: 0, traffic_multiplier: 1, daily_limit: 0, daily_used: 0,
  block_torrent: false, window_from: '', window_to: '', window_days: 0, suspend_reason: '' as const, group_id: null, admin_id: null,
};

const users: User[] = Array.from({ length: 148 }, (_, i) => mkUser(i + 1));
let nextUserId = users.length + 1;

const ALL_PERMS: Perm[] = ['users_view', 'users_create', 'users_edit', 'users_delete', 'all_users', 'inbounds', 'settings', 'certs', 'logs', 'groups'];
const SUB_PERMS: Perm[] = ['users_view', 'users_create', 'users_edit', 'users_delete'];
const admins: Admin[] = [
  { id: 1, username: 'root', is_sudo: true, created_at: iso(now() - 90 * DAY), permissions: ALL_PERMS, user_quota: 0, traffic_quota: 0 },
  { id: 2, username: 'support', is_sudo: false, created_at: iso(now() - 40 * DAY), permissions: [...SUB_PERMS, 'all_users', 'logs'], user_quota: 0, traffic_quota: 0 },
  { id: 3, username: 'billing', is_sudo: false, created_at: iso(now() - 7 * DAY), permissions: SUB_PERMS, user_quota: 50, traffic_quota: 40 * 1024 * GB },
];
/** Admin view with usage, as GET /admins returns it. */
const adminFull = (a: Admin): Admin => {
  const own = users.filter((u) => (u.admin_id ?? 1) === a.id);
  return { ...a, permissions: a.is_sudo ? ALL_PERMS : a.permissions, users: own.length, traffic_allocated: own.reduce((x, u) => x + u.data_limit, 0) };
};
/** The signed-in mock admin (log in as "support" or "billing" to see a sub-admin). */
const SESSION_KEY = 'veilon-mock-admin';
const currentAdmin = () => admins.find((a) => a.username === sessionStorage.getItem(SESSION_KEY)) ?? admins[0];

const actions = ['user.create', 'user.update', 'user.delete', 'user.reset_traffic', 'user.revoke_sub', 'inbound.update', 'inbound.create', 'core.restart', 'settings.update', 'auth.login', 'cert.acme'];
const audit: AuditEntry[] = Array.from({ length: 87 }, (_, i) => {
  const a = pick(actions);
  return {
    id: 87 - i, admin: pick(['root', 'root', 'support', 'billing']), action: a,
    target: a.startsWith('user') ? pick(names) : a.startsWith('inbound') ? pick(inbounds).remark : a === 'cert.acme' ? 'cdn.veilon.example' : '—',
    ip: `${pick([5, 31, 37, 78, 91, 185])}.${Math.floor(rnd() * 255)}.${Math.floor(rnd() * 255)}.${Math.floor(rnd() * 255)}`,
    created_at: iso(now() - i * (DAY / 5) - Math.floor(rnd() * 3_600_000)),
  };
});

/** Mirrors the server rule: inbound ports and names (trimmed, case-insensitive) are unique. */
function mockInboundConflict(b: Pick<Inbound, 'remark' | 'port' | 'protocol'>, selfId: number, prev?: Inbound) {
  const name = (b.remark ?? '').trim().toLowerCase();
  if (!prev || prev.remark.trim().toLowerCase() !== name) {
    const o = inbounds.find((i) => i.id !== selfId && i.remark.trim().toLowerCase() === name);
    if (o) throw new HttpError(409, 'conflict', `name "${o.remark}" is already used by another inbound`);
  }
  if (b.protocol === 'tun' || (prev && prev.port === b.port && prev.protocol !== 'tun')) return;
  if (b.port === settings.panel_port) throw new HttpError(409, 'conflict', `port ${b.port} is used by the panel itself`);
  const o = inbounds.find((i) => i.id !== selfId && i.protocol !== 'tun' && i.port === b.port);
  if (o) throw new HttpError(409, 'conflict', `port ${b.port} is already used by inbound "${o.remark}"`);
}

let settings: Settings = {
  panel_port: 2087, panel_path: 'x7k2p9', panel_domain: 'edge.veilon.example', panel_cert_id: 1, sub_path: 'sub', sub_domain: '',
  sub_title: 'Veilon', sub_update_hours: 12, server_address: SERVER, timezone: 'Asia/Tehran', language: 'fa',
  ad_block: false, block_torrent: true,
};

let coreStarted = now() - 3 * DAY - 4 * 3_600_000;
let coreRunning = true;
const stoppedCores = new Set<string>();
let xrayVersion = '26.10.31';
let totalUp = 412 * GB;
let totalDown = 3.86 * 1024 * GB;
let lastUp = 2_400_000;
let lastDown = 11_000_000;

type MockLog = { seq: number; t: string | null; source: string; level: 'debug' | 'info' | 'warning' | 'error'; msg: string };
const logLines: MockLog[] = [];
let logSeq = 0;
function addLog(msg: string, source = 'xray', at = now()) {
  const level = /\[Error\]/.test(msg) ? 'error' : /\[Warning\]/.test(msg) ? 'warning' : 'info';
  logLines.push({ seq: ++logSeq, t: iso(at), source, level, msg });
}
function logLine() {
  const ts = '';
  const u = pick(users.filter((x) => x.online));
  const r = rnd();
  const ip = `${pick([5, 31, 37, 78, 91, 151, 185])}.${Math.floor(rnd() * 255)}.${Math.floor(rnd() * 255)}.${Math.floor(rnd() * 255)}`;
  if (r < 0.7) {
    const host = pick(['www.google.com', 'i.instagram.com', 'web.telegram.org', 'api.github.com', 'rr3---sn.googlevideo.com', 'open.spotify.com', 'x.com', 'www.wikipedia.org']);
    return `${ts}from ${ip}:${Math.floor(rnd() * 60000 + 1024)} accepted tcp:${host}:443 [${pick(inbounds).remark.split(' ')[0].toLowerCase()}-in >> direct] email: ${u?.username ?? 'arash'}`;
  }
  if (r < 0.85) return `${ts}[Info] [${Math.floor(rnd() * 4e9)}] proxy/vless/inbound: firstLen = ${Math.floor(rnd() * 1400)}`;
  if (r < 0.95) return `${ts}[Warning] [${Math.floor(rnd() * 4e9)}] app/dispatcher: default route for tcp:${ip}:${Math.floor(rnd() * 60000)}`;
  return `${ts}[Error] [${Math.floor(rnd() * 4e9)}] proxy/vless/inbound: invalid request from ${ip}: invalid user > not found`;
}
for (let i = 0; i < 120; i++) addLog(logLine(), 'xray', now() - (120 - i) * 1700 - Math.floor(rnd() * 900));
addLog('Veilon 0.2.2 listening on :2053', 'panel', now() - 210_000);

function addAudit(action: string, target: string) {
  audit.unshift({ id: (audit[0]?.id ?? 0) + 1, admin: 'root', action, target, ip: '127.0.0.1', created_at: iso(now()) });
}

function recount() {
  for (const ib of inbounds) ib.user_count = users.filter((u) => u.inbound_ids.includes(ib.id)).length;
}
recount();

function coresInfo() {
  const versions: Record<string, string> = { xray: xrayVersion, 'sing-box': '1.14.4', mtg: '1.15.0', amneziawg: '0.2.15' };
  return (['xray', 'sing-box', 'mtg', 'amneziawg'] as const).map((name) => {
    const ibs = inbounds.filter((i) => i.enabled && engineOf(i.protocol) === name);
    const enabled = name === 'xray' || ibs.length > 0;
    const warnings = name === 'xray' && inbounds.some((i) => i.protocol === 'tun' && i.enabled) ? ['TUN · Office subnets: CAP_NET_ADMIN is missing; inbound skipped'] : [];
    return {
      name, version: versions[name], running: enabled && coreRunning && !stoppedCores.has(name), started_at: enabled && coreRunning && !stoppedCores.has(name) ? iso(coreStarted) : null, enabled,
      stopped: stoppedCores.has(name),
      protocols: [...new Set(ibs.map((i) => i.protocol))], instances: name === 'mtg' ? ibs.length : enabled ? 1 : 0, warnings,
    };
  });
}

function stats() {
  lastUp = Math.max(200_000, lastUp + (rnd() - 0.5) * 900_000);
  lastDown = Math.max(1_000_000, lastDown + (rnd() - 0.48) * 3_600_000);
  if (lastDown > 26_000_000) lastDown = 20_000_000;
  totalUp += lastUp * 2;
  totalDown += lastDown * 2;
  return {
    cpu_percent: Math.round((18 + rnd() * 30) * 10) / 10,
    mem_used: Math.round((1.35 + rnd() * 0.3) * GB), mem_total: 4 * GB,
    disk_used: 23.4 * GB, disk_total: 80 * GB,
    swap_used: Math.round(0.21 * GB), swap_total: 2 * GB,
    cpu_cores: 2, cpu_model: 'Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz',
    load_avg: [0.42, 0.37, 0.31] as [number, number, number],
    uptime_sec: Math.floor((now() - (now() - 18 * DAY - 7_200_000)) / 1000),
    net_up_bps: Math.round(lastUp), net_down_bps: Math.round(lastDown), net_sampled_at: iso(now()), net_interval_ms: 3000,
    users_total: users.length,
    users_active: users.filter((u) => u.status === 'active').length,
    users_online: coreRunning ? users.filter((u) => u.online).length : 0,
    users_expired: users.filter((u) => u.status === 'expired').length,
    users_limited: users.filter((u) => u.status === 'limited').length,
    users_disabled: users.filter((u) => u.status === 'disabled').length,
    users_on_hold: users.filter((u) => u.status === 'on_hold').length,
    traffic_total_up: Math.round(totalUp), traffic_total_down: Math.round(totalDown),
    core: coresInfo()[0],
    cores: coresInfo(),
    stopped_cores: [...stoppedCores],
  };
}

function traffic(range: string) {
  const n = range === '24h' ? 48 : range === '7d' ? 84 : 60;
  const step = range === '24h' ? 1_800_000 : range === '7d' ? 7_200_000 : 12 * 3_600_000;
  const pts = [];
  for (let i = n - 1; i >= 0; i--) {
    const t = Math.floor(now() / step) * step - i * step;
    const h = new Date(t).getHours();
    const diurnal = 0.45 + 0.55 * Math.sin(((h - 9) / 24) * Math.PI * 2) ** 2;
    const scale = step / 1000;
    const down = Math.round((6_000_000 + rnd() * 5_000_000) * diurnal * scale);
    pts.push({ t: iso(t), up: Math.round(down * (0.12 + rnd() * 0.06)), down });
  }
  return { points: pts, bucket_sec: step / 1000 };
}

function linksFor(u: User) {
  return inbounds
    .filter((i) => u.inbound_ids.includes(i.id) && i.protocol !== 'tun' && i.protocol !== 'tunnel')
    .map((i) => {
      const host = i.address_override || SERVER;
      const tag = encodeURIComponent(`${i.remark} · ${u.username}`);
      const ss = withSettings(i.settings);
      let uri = '';
      let conf: string | undefined;
      let conf_name: string | undefined;
      switch (i.protocol) {
        case 'vless': {
          const q = new URLSearchParams({ type: i.network, security: i.security });
          if (i.flow) q.set('flow', i.flow);
          if (i.reality) {
            q.set('pbk', i.reality.public_key);
            q.set('sni', i.reality.server_names[0]);
            q.set('sid', i.reality.short_ids[0]);
            q.set('fp', i.reality.fingerprint);
          }
          if (i.tls) q.set('sni', i.tls.sni);
          if (i.transport.path) q.set('path', i.transport.path);
          uri = `vless://${u.uuid}@${host}:${i.port}?${q}#${tag}`;
          break;
        }
        case 'trojan':
          uri = `trojan://${u.password}@${host}:${i.port}?security=${i.security}&type=${i.network}&serviceName=${i.transport.service_name}#${tag}`;
          break;
        case 'vmess':
          uri = 'vmess://' + btoa(JSON.stringify({ v: '2', ps: `${i.remark} · ${u.username}`, add: host, port: i.port, id: u.uuid, net: i.network, path: i.transport.path, tls: '' }));
          break;
        case 'shadowsocks':
          uri = `ss://${btoa(`${i.ss_method}:${u.password}`)}@${host}:${i.port}#${tag}`;
          break;
        case 'hysteria':
          uri = `hysteria2://${u.password}@${host}:${i.port}?sni=www.bing.com&obfs=salamander&obfs-password=${ss.obfs_password}&pinSHA256=AB12#${tag}`;
          break;
        case 'tuic':
          uri = `tuic://${u.uuid}:${u.password}@${host}:${i.port}?congestion_control=${ss.congestion}&alpn=h3&sni=www.bing.com#${tag}`;
          break;
        case 'http':
          uri = `http://${u.username}:${u.password}@${host}:${i.port}#${tag}`;
          break;
        case 'mixed':
          uri = `socks://${btoa(`${u.username}:${u.password}`)}@${host}:${i.port}#${tag}`;
          break;
        case 'mtproto':
          uri = `tg://proxy?server=${host}&port=${i.port}&secret=ee${hex(32)}7777772e636c6f7564666c6172652e636f6d`;
          break;
        case 'wireguard':
        case 'amneziawg': {
          const a = ss.awg;
          const addr = ss.address.replace(/\.1\/\d+$/, `.${(u.id % 250) + 2}/32`);
          conf =
            `[Interface]\nPrivateKey = ${b64(43)}=\nAddress = ${addr}\nDNS = ${ss.dns.join(', ')}\nMTU = ${ss.mtu}\n` +
            (a ? `Jc = ${a.jc}\nJmin = ${a.jmin}\nJmax = ${a.jmax}\nS1 = ${a.s1}\nS2 = ${a.s2}\nH1 = ${a.h1}\nH2 = ${a.h2}\nH3 = ${a.h3}\nH4 = ${a.h4}\n` : '') +
            `\n[Peer]\nPublicKey = ${ss.public_key}\nPresharedKey = ${b64(43)}=\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = ${host}:${i.port}\nPersistentKeepalive = ${ss.keepalive}\n`;
          conf_name = `${i.protocol === 'wireguard' ? 'WG' : 'AWG'}-${u.username}.conf`;
          uri = i.protocol === 'wireguard' ? `wireguard://${encodeURIComponent(b64(43) + '=')}@${host}:${i.port}?publickey=${encodeURIComponent(ss.public_key)}&address=${encodeURIComponent(addr)}&mtu=${ss.mtu}#${tag}` : '';
          break;
        }
        case 'masque':
          uri = '';
          break;
      }
      return { inbound_id: i.id, remark: i.remark, protocol: i.protocol, uri, conf, conf_name };
    });
}

function pickLimits(b: Partial<User>): Partial<User> {
  const keys = ['ip_limit', 'device_limit', 'require_hwid', 'speed_limit_down', 'speed_limit_up', 'traffic_multiplier', 'daily_limit', 'block_torrent', 'window_from', 'window_to', 'window_days', 'group_id'] as const;
  const out: Record<string, unknown> = {};
  for (const k of keys) if (b[k] !== undefined) out[k] = b[k];
  return out as Partial<User>;
}

/* ---------- router ---------- */
class HttpError extends Error {
  constructor(public status: number, public code: string, msg: string) {
    super(msg);
  }
}
const notFound = () => new HttpError(404, 'not_found', 'Not found');
const byId = <T extends { id: number }>(arr: T[], id: number) => {
  const x = arr.find((a) => a.id === id);
  if (!x) throw notFound();
  return x;
};

function sortUsers(list: User[], sort: string) {
  const desc = sort.startsWith('-');
  const key = sort.replace(/^-/, '');
  const val = (u: User): number | string => {
    switch (key) {
      case 'username':
        return u.username;
      case 'used_traffic':
        return u.used_traffic;
      case 'expire_at':
        return u.expire_at ? new Date(u.expire_at).getTime() : Number.MAX_SAFE_INTEGER;
      default:
        return new Date(u.created_at).getTime();
    }
  };
  return [...list].sort((a, b) => {
    const va = val(a),
      vb = val(b);
    const c = va < vb ? -1 : va > vb ? 1 : 0;
    return desc ? -c : c;
  });
}

type Body = Record<string, unknown>;

function route(method: string, path: string, q: URLSearchParams, body: Body): unknown {
  const seg = path.split('/').filter(Boolean);
  const id = Number(seg[1]);

  if (path === '/auth/login' && method === 'POST') {
    const { username, password } = body as { username: string; password: string };
    if (!username || !password || password === 'wrong') throw new HttpError(401, 'invalid_credentials', 'Invalid credentials');
    const known = admins.find((a) => a.username === username);
    sessionStorage.setItem(SESSION_KEY, known ? known.username : admins[0].username);
    return { token: 'mock.' + b64(32), expires_at: iso(now() + DAY), admin: known ?? { id: 1, username, is_sudo: true } };
  }
  if (path === '/auth/me' && method === 'PATCH') {
    const { username, current_password } = body as { username: string; current_password: string };
    if (!current_password || current_password === 'wrong') throw new HttpError(400, 'invalid_credentials', 'current password is incorrect');
    const name = (username ?? '').trim();
    if (!/^[a-zA-Z0-9_.-]{3,32}$/.test(name)) throw new HttpError(400, 'validation_error', 'username must be 3-32 chars of letters, digits, _ . -');
    if (admins.some((a, i) => i !== 0 && a.username === name)) throw new HttpError(409, 'conflict', 'username is already taken');
    admins[0] = { ...admins[0], username: name };
    return { token: 'mock.' + b64(32), expires_at: iso(now() + DAY), admin: admins[0] };
  }
  if (path === '/auth/me') return adminFull(currentAdmin());
  if (path === '/auth/password') return { ok: true, token: 'mock.' + b64(32), expires_at: iso(now() + DAY) };

  if (path === '/system/stats') return stats();
  if (path === '/system/traffic') return traffic(q.get('range') || '24h');
  if (path === '/core/restart') {
    coreRunning = true;
    coreStarted = now();
    addAudit('core.restart', '—');
    addLog('[Warning] core: Xray 25.10.15 started');
    addLog('core restarted by root', 'panel');
    return { ok: true };
  }
  if (path === '/core/start' || path === '/core/stop') {
    const on = path === '/core/start';
    const scope = q.get('scope') || String((body as { scope?: string } | undefined)?.scope ?? '') || (on ? 'all' : 'xray');
    if (scope !== 'xray' && scope !== 'all') throw new HttpError(400, 'validation_error', 'scope must be "xray" or "all"');
    const names = scope === 'xray' ? ['xray'] : ['xray', 'sing-box', 'mtg', 'amneziawg'];
    const changed = names.filter((n) => stoppedCores.has(n) === on);
    for (const n of changed) (on ? stoppedCores.delete(n) : stoppedCores.add(n));
    coreStarted = now();
    addAudit(on ? 'core.start' : 'core.stop', changed.length ? `${scope}: ${changed.join(', ')}` : scope);
    const cores = coresInfo();
    return { ok: true, scope, changed, status: cores[0], cores, stopped_cores: [...stoppedCores] };
  }
  if (path === '/core/xray') {
    return { status: coresInfo()[0], stopped_cores: [...stoppedCores], job: null, geo: [{ name: 'geoip.dat', size: 19_800_000, updated_at: iso(now() - 9 * DAY) }, { name: 'geosite.dat', size: 9_100_000, updated_at: iso(now() - 9 * DAY) }] };
  }
  if (path === '/core/xray/releases') {
    const tags = ['v26.11.9', 'v26.10.31', 'v26.10.21', 'v26.9.30', 'v26.9.9', 'v26.7.28', 'v26.7.11', 'v26.6.27'];
    return { releases: tags.map((tag, i) => ({ tag, prerelease: false, published_at: iso(now() - (i * 12 + 1) * DAY) })), fetched_at: iso(now()), stale: false };
  }
  if (path === '/core/xray/install') {
    const v = String((body as { version?: string } | undefined)?.version ?? '');
    xrayVersion = v.replace(/^v/, '');
    coreStarted = now();
    addAudit('core.install', 'xray ' + v);
    return { ok: true, job: { kind: 'install', version: v, running: false, started_at: iso(now()), finished_at: iso(now()) } };
  }
  if (path === '/core/xray/geo') {
    addAudit('core.geo_update', 'xray');
    return { ok: true, job: { kind: 'geo', running: false, started_at: iso(now()), finished_at: iso(now()) } };
  }
  if (path === '/core/logs' && q.get('core') && !['xray', 'all', 'panel'].includes(q.get('core')!)) {
    const c = q.get('core')!;
    const t0 = new Date().toISOString();
    const msgs = ['started', `serving ${inbounds.filter((i) => engineOf(i.protocol) === c).length} inbound(s)`, 'stats ok'];
    return { lines: msgs.map((m) => `${t0} ${m}`), entries: msgs.map((msg, i) => ({ seq: i + 1, t: t0, source: c, level: 'info', msg })) };
  }
  if (path === '/core/logs') {
    const n = 1 + Math.floor(rnd() * 4);
    for (let i = 0; i < n; i++) addLog(logLine());
    while (logLines.length > 2000) logLines.shift();
    const src = q.get('core') || 'all';
    const list = logLines.filter((e) => src === 'all' || e.source === src).sort((a, b) => (a.t ?? '').localeCompare(b.t ?? '') || a.seq - b.seq).slice(-Number(q.get('lines') || 200));
    return { lines: list.map((e) => `${e.t} ${e.msg}`), entries: list };
  }
  if (path === '/core/config' && q.get('core') === 'mtg') return '[secret]\n# one section per MTProto inbound\n';
  if (path === '/core/config') {
    return {
      log: { loglevel: 'warning' },
      inbounds: inbounds.filter((i) => i.enabled).map((i) => ({ tag: `in-${i.id}`, port: i.port, protocol: i.protocol, streamSettings: { network: i.network, security: i.security } })),
      outbounds: [{ protocol: 'freedom', tag: 'direct' }, { protocol: 'blackhole', tag: 'block' }],
      routing: { rules: [{ type: 'field', ip: ['geoip:private'], outboundTag: 'block' }] },
    };
  }

  if (seg[0] === 'users') {
    if (path === '/users' && method === 'GET') {
      const s = (q.get('search') || '').toLowerCase();
      const st = q.get('status') || '';
      const gid = Number(q.get('group_id') || 0);
      const me = currentAdmin();
      const scope = me.is_sudo || me.permissions?.includes('all_users') ? null : me.id;
      let list = users.filter(
        (u) =>
          (scope === null || u.admin_id === scope) &&
          (!s || u.username.includes(s) || u.note.toLowerCase().includes(s)) &&
          (!st || (st === 'online' ? u.online : u.status === st)) &&
          (!gid || (gid === -1 ? u.group_id === null : u.group_id === gid)),
      );
      list = sortUsers(list, q.get('sort') || '-created_at');
      const page = Number(q.get('page') || 1),
        per = Number(q.get('per_page') || 20);
      return { items: list.slice((page - 1) * per, page * per), total: list.length };
    }
    if (path === '/users' && method === 'POST') {
      const b = body as Partial<User>;
      if (!b.username || !/^[a-zA-Z0-9_-]{3,32}$/.test(b.username)) throw new HttpError(422, 'invalid_username', 'Invalid username');
      if (users.some((u) => u.username === b.username)) throw new HttpError(409, 'username_taken', 'Username already exists');
      const token = b64(22);
      const u: User = {
        id: nextUserId++, username: b.username, status: b.on_hold_days ? 'on_hold' : 'active', data_limit: b.data_limit ?? 0, used_traffic: 0, up: 0, down: 0,
        expire_at: b.expire_at ?? null, on_hold_days: b.on_hold_days ?? null, reset_strategy: b.reset_strategy ?? 'no_reset', note: b.note ?? '',
        inbound_ids: b.inbound_ids ?? [], uuid: uuid(), password: b64(16), sub_token: token, sub_url: `https://${SERVER}/sub/${token}`,
        online: false, last_online_at: null, created_at: iso(now()), updated_at: iso(now()),
        ...LIMIT_DEFAULTS,
        ...pickLimits(b),
      };
      users.push(u);
      recount();
      addAudit('user.create', u.username);
      return u;
    }
    if (path === '/users/bulk') {
      const { action, ids, days, group_id } = body as { action: string; ids: number[]; days?: number; group_id?: number };
      if (action === 'set_group' && group_id && !groups.some((g) => g.id === group_id)) throw new HttpError(400, 'validation_error', 'group_id does not exist');
      for (const uid of ids) {
        const idx = users.findIndex((u) => u.id === uid);
        if (idx < 0) continue;
        const u = users[idx];
        if (action === 'delete') users.splice(idx, 1);
        if (action === 'disable') u.status = 'disabled';
        if (action === 'enable') u.status = 'active';
        if (action === 'set_group') u.group_id = group_id || null;
        if (action === 'reset_traffic') Object.assign(u, { used_traffic: 0, up: 0, down: 0, status: u.status === 'limited' ? 'active' : u.status });
        if (action === 'extend' && u.status === 'on_hold' && u.on_hold_days) u.on_hold_days += days ?? 30;
        if (action === 'extend' && u.expire_at) {
          u.expire_at = iso(Math.max(new Date(u.expire_at).getTime(), now()) + (days ?? 30) * DAY);
          if (u.status === 'expired') u.status = 'active';
        }
      }
      recount();
      addAudit(`user.bulk_${action}`, `${ids.length} users`);
      return { ok: true, affected: ids.length };
    }
    const u = byId(users, id);
    if (seg.length === 2) {
      if (method === 'GET') return u;
      if (method === 'DELETE') {
        users.splice(users.indexOf(u), 1);
        recount();
        addAudit('user.delete', u.username);
        return { ok: true };
      }
      if (method === 'PATCH') {
        if (body.traffic_multiplier !== undefined) body.traffic_multiplier = Math.round(Number(body.traffic_multiplier || 1) * 10) / 10;
        if (body.group_id !== undefined && body.group_id !== null && !groups.some((g) => g.id === body.group_id)) throw new HttpError(400, 'validation_error', 'group_id does not exist');
        Object.assign(u, body, { updated_at: iso(now()) });
        if (u.daily_limit === 0 || u.daily_used < u.daily_limit) u.suspend_reason = u.suspend_reason === 'daily' ? '' : u.suspend_reason;
        if (!u.window_from && !u.window_days && u.suspend_reason === 'window') u.suspend_reason = '';
        if (body.expire_at && new Date(body.expire_at as string).getTime() > now() && u.status === 'expired') u.status = 'active';
        recount();
        addAudit('user.update', u.username);
        return u;
      }
    }
    if (seg[2] === 'reset-traffic') {
      Object.assign(u, { used_traffic: 0, up: 0, down: 0, status: u.status === 'limited' ? 'active' : u.status });
      addAudit('user.reset_traffic', u.username);
      return u;
    }
    if (seg[2] === 'revoke-sub') {
      const token = b64(22);
      Object.assign(u, { sub_token: token, sub_url: `https://${SERVER}/sub/${token}`, uuid: uuid(), password: b64(16) });
      addAudit('user.revoke_sub', u.username);
      return u;
    }
    if (seg[2] === 'links') return { links: linksFor(u), sub_url: u.sub_url };
    if (seg[2] === 'connections') return connsOf(u);
    if (seg[2] === 'ips') {
      const c = connsOf(u);
      if (method === 'POST') {
        const { ip, blocked } = body as { ip: string; blocked: boolean };
        const row = c.ips.find((x) => x.ip === ip);
        if (row) row.blocked = blocked;
        else c.ips.push({ id: connSeq++, ip, country: '', first_seen: iso(now()), last_seen: iso(now()), count: 0, blocked, live: false });
        addAudit(blocked ? 'user.ip_block' : 'user.ip_unblock', `${u.username} ${ip}`);
        return { ok: true };
      }
      if (method === 'DELETE') {
        c.ips = c.ips.filter((x) => x.id !== Number(seg[3]));
        return { ok: true };
      }
    }
    if (seg[2] === 'devices') {
      const c = connsOf(u);
      const d = byId(c.devices, Number(seg[3]));
      if (method === 'PATCH') {
        d.blocked = !!(body as { blocked: boolean }).blocked;
        addAudit(d.blocked ? 'user.device_block' : 'user.device_unblock', u.username);
        return { ok: true };
      }
      if (method === 'DELETE') {
        c.devices = c.devices.filter((x) => x.id !== d.id);
        addAudit('user.device_delete', u.username);
        return { ok: true };
      }
    }
  }

  if (seg[0] === 'groups') {
    const view = (g: Group) => ({ ...g, users: users.filter((u) => u.group_id === g.id).length });
    if (path === '/groups' && method === 'GET') return groups.map(view);
    const check = (b: Partial<GroupInput>, selfId: number) => {
      if (b.name !== undefined) {
        const name = b.name.trim();
        if (!name || name.length > 48) throw new HttpError(400, 'validation_error', 'group name must be 1-48 characters');
        if (groups.some((g) => g.id !== selfId && g.name === name)) throw new HttpError(409, 'conflict', 'a group with this name already exists');
      }
      if (b.color !== undefined && !/^#[0-9a-f]{6}$/i.test(b.color)) throw new HttpError(400, 'validation_error', 'color must be #rrggbb');
    };
    if (path === '/groups' && method === 'POST') {
      const b = body as Partial<GroupInput>;
      check({ color: '#8b7cff', ...b, name: b.name ?? '' }, 0);
      const g: Group = { id: Math.max(0, ...groups.map((x) => x.id)) + 1, name: (b.name ?? '').trim(), color: (b.color ?? '#8b7cff').toLowerCase(), ad_block: !!b.ad_block, inbound_ids: b.inbound_ids ?? [], defaults: b.defaults ?? {}, users: 0, created_at: iso(now()) };
      groups.push(g);
      addAudit('group.create', g.name);
      return view(g);
    }
    const g = byId(groups, id);
    if (method === 'PATCH') {
      const b = body as Partial<GroupInput>;
      check(b, g.id);
      Object.assign(g, b, b.name ? { name: b.name.trim() } : {});
      addAudit('group.update', g.name);
      return view(g);
    }
    if (method === 'DELETE') {
      groups.splice(groups.indexOf(g), 1);
      users.forEach((u) => u.group_id === g.id && (u.group_id = null));
      addAudit('group.delete', g.name);
      return { ok: true };
    }
  }

  if (seg[0] === 'inbounds') {
    if (path === '/inbounds/reality-keys') return { private_key: b64(43), public_key: b64(43), short_id: hex(8) };
    if (path === '/inbounds/wireguard-keys') {
      let s1 = 15 + Math.floor(rnd() * 135);
      let s2 = 15 + Math.floor(rnd() * 135);
      if (s1 + 56 === s2) s2++;
      const h = () => 5 + Math.floor(rnd() * 2147483000);
      return { private_key: b64(43) + '=', public_key: b64(43) + '=', awg: { jc: 3 + Math.floor(rnd() * 6), jmin: 40, jmax: 70, s1, s2, h1: h(), h2: h(), h3: h(), h4: h() } };
    }
    if (path === '/inbounds' && method === 'GET') return inbounds;
    if (path === '/inbounds' && method === 'POST') {
      const b = body as unknown as Inbound;
      if (!b.remark?.trim()) throw new HttpError(400, 'validation_error', 'remark is required (max 128 chars)');
      if (b.protocol === 'tunnel' && !b.settings?.target_address) throw new HttpError(400, 'validation_error', 'settings.target_address is required (where to forward)');
      mockInboundConflict(b, 0);
      const ib: Inbound = { ...b, id: Math.max(0, ...inbounds.map((i) => i.id)) + 1, user_count: 0, created_at: iso(now()) };
      inbounds.push(ib);
      addAudit('inbound.create', ib.remark);
      return ib;
    }
    const ib = byId(inbounds, id);
    if (method === 'GET') return ib;
    if (method === 'PATCH') {
      mockInboundConflict({ ...ib, ...(body as Partial<Inbound>) }, ib.id, ib);
      Object.assign(ib, body);
      addAudit('inbound.update', ib.remark);
      return ib;
    }
    if (method === 'DELETE') {
      inbounds.splice(inbounds.indexOf(ib), 1);
      users.forEach((u) => (u.inbound_ids = u.inbound_ids.filter((x) => x !== ib.id)));
      addAudit('inbound.delete', ib.remark);
      return { ok: true };
    }
  }

  if (seg[0] === 'certs') {
    if (path === '/certs/public-ip') {
      return {
        ...PUBLIC_IP,
        candidates: [
          { ip: PUBLIC_IP.ipv4, family: 'ipv4', sources: ['api.ipify.org', 'icanhazip.com', 'checkip.amazonaws.com'] },
          { ip: '198.51.100.7', family: 'ipv4', sources: ['ifconfig.me'] },
          { ip: PUBLIC_IP.ipv6, family: 'ipv6', sources: ['api.ipify.org', 'icanhazip.com'] },
        ],
      };
    }
    if (method === 'GET' && path === '/certs') return certs.map(certView);
    if (method === 'GET' && id) return certView(byId(certs, id));
    const nextId = () => Math.max(0, ...certs.map((x) => x.id)) + 1;
    if (path === '/certs/self-signed') {
      const domain = (body as { domain: string }).domain?.trim();
      if (!domain) throw new HttpError(400, 'validation_error', 'domain is required');
      const c = mkCert({ id: nextId(), domain, issuer: 'Veilon self-signed', not_before: iso(now()), not_after: iso(now() + 3650 * DAY), auto_renew: false, source: 'self' });
      certs.push(c);
      addAudit('cert.self_signed', domain);
      return certView(c);
    }
    if (path === '/certs/acme') {
      const b = body as { type?: string; domain?: string; ip?: string; ipv6?: string; email?: string; use_for_panel?: boolean };
      const email = (b.email ?? '').trim();
      if (email && !/^[^@\s]+@[^@\s]+$/.test(email)) throw new HttpError(400, 'validation_error', 'email is not valid');
      let ids: string[];
      let profile = '';
      if ((b.type ?? 'domain') === 'ip') {
        const ip4 = !b.ip || b.ip === 'auto' ? PUBLIC_IP.ipv4 : b.ip.trim();
        const ip6 = b.ipv6 === 'auto' ? PUBLIC_IP.ipv6 : (b.ipv6 ?? '').trim();
        for (const v of [ip4, ip6].filter(Boolean)) {
          if (!/^[\d.]+$|^[0-9a-f:]+$/i.test(v)) throw new HttpError(400, 'validation_error', `"${v}" is not an IP address`);
          if (/^(10\.|127\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(v)) throw new HttpError(400, 'validation_error', `${v} is not a public IP address; a public CA cannot validate it`);
        }
        ids = [ip4, ip6].filter(Boolean);
        profile = 'shortlived';
      } else {
        const d = (b.domain ?? '').trim().toLowerCase();
        if (!/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(d)) throw new HttpError(400, 'validation_error', 'a valid domain name is required');
        if (d.startsWith('fail.')) throw new HttpError(502, 'acme_failed', `acme: authorization for ${d} failed: connection refused on port 80`);
        ids = [d];
      }
      const short = profile === 'shortlived';
      const c = mkCert({
        id: nextId(), domain: ids[0], identifiers: ids, issuer: short ? "Let's Encrypt YE2" : "Let's Encrypt R10", not_before: iso(now()),
        not_after: iso(now() + (short ? 160 * HOUR : 90 * DAY)), auto_renew: true, source: 'acme', profile,
      });
      certs.push(c);
      addAudit('cert.acme', ids.join(','));
      if (b.use_for_panel) settings = { ...settings, panel_cert_id: c.id, panel_domain: short ? settings.panel_domain : ids[0] };
      return certView(c);
    }
    if (path === '/certs/upload') {
      const domain = (body as { domain: string }).domain || 'uploaded.example.com';
      const c = mkCert({ id: nextId(), domain, issuer: 'Custom CA', not_before: iso(now()), not_after: iso(now() + 365 * DAY), auto_renew: false, source: 'upload' });
      certs.push(c);
      addAudit('cert.upload', domain);
      return certView(c);
    }
    const c = byId(certs, id);
    if (method === 'PATCH') {
      const v = (body as { auto_renew?: boolean }).auto_renew;
      if (typeof v !== 'boolean') throw new HttpError(400, 'validation_error', 'nothing to change (auto_renew)');
      c.auto_renew = v;
      addAudit('cert.update', c.domain);
      return certView(c);
    }
    if (seg[2] === 'renew') {
      if (c.source !== 'acme') throw new HttpError(400, 'validation_error', 'only ACME certificates can be renewed');
      const short = c.profile === 'shortlived';
      Object.assign(c, { not_before: iso(now()), not_after: iso(now() + (short ? 160 * HOUR : 90 * DAY)), last_error: '', last_attempt_at: iso(now()) });
      addAudit('cert.renew', c.domain);
      return certView(c);
    }
    if (seg[2] === 'revoke' || method === 'DELETE') {
      if (certInUse(c.id)) throw new HttpError(409, 'conflict', 'certificate is in use');
      if (seg[2] === 'revoke' && c.source !== 'acme') throw new HttpError(400, 'validation_error', 'only ACME certificates can be revoked');
      certs.splice(certs.indexOf(c), 1);
      addAudit(seg[2] === 'revoke' ? 'cert.revoke' : 'cert.delete', c.domain);
      return { ok: true };
    }
  }

  if (path === '/settings') {
    if (method === 'PATCH') {
      settings = { ...settings, ...(body as Partial<Settings>) };
      addAudit('settings.update', '—');
    }
    return settings;
  }

  if (seg[0] === 'admins') {
    if (!currentAdmin().is_sudo) throw new HttpError(403, 'forbidden', 'sudo admin required');
    if (method === 'GET') return admins.map(adminFull);
    if (method === 'POST') {
      const b = body as { username: string; is_sudo: boolean; permissions?: Perm[]; user_quota?: number; traffic_quota?: number };
      if (admins.some((a) => a.username === b.username)) throw new HttpError(409, 'username_taken', 'Admin already exists');
      if ((b.user_quota ?? 0) < 0 || (b.traffic_quota ?? 0) < 0) throw new HttpError(400, 'validation_error', 'quotas must be >= 0');
      const perms = b.is_sudo ? ALL_PERMS : b.permissions ? ALL_PERMS.filter((p) => b.permissions!.includes(p)) : SUB_PERMS;
      const a: Admin = { id: Math.max(...admins.map((x) => x.id)) + 1, username: b.username, is_sudo: !!b.is_sudo, created_at: iso(now()), permissions: perms, user_quota: b.is_sudo ? 0 : b.user_quota ?? 0, traffic_quota: b.is_sudo ? 0 : b.traffic_quota ?? 0 };
      admins.push(a);
      return adminFull(a);
    }
    if (method === 'PATCH') {
      const a = byId(admins, id);
      const b = body as { permissions?: Perm[]; user_quota?: number; traffic_quota?: number; is_sudo?: boolean };
      if (b.is_sudo === false && a.is_sudo) {
        if (a.id === currentAdmin().id) throw new HttpError(400, 'validation_error', 'you cannot remove your own sudo access');
        if (admins.filter((x) => x.is_sudo).length <= 1) throw new HttpError(400, 'validation_error', 'cannot demote the last sudo admin');
      }
      if ((b.user_quota ?? 0) < 0 || (b.traffic_quota ?? 0) < 0) throw new HttpError(400, 'validation_error', 'quotas must be >= 0');
      if (b.permissions) a.permissions = ALL_PERMS.filter((p) => b.permissions!.includes(p));
      if (b.user_quota !== undefined) a.user_quota = b.user_quota;
      if (b.traffic_quota !== undefined) a.traffic_quota = b.traffic_quota;
      if (b.is_sudo !== undefined) a.is_sudo = b.is_sudo;
      addAudit('admin.update', a.username);
      return adminFull(a);
    }
    if (method === 'DELETE') {
      const a = byId(admins, id);
      admins.splice(admins.indexOf(a), 1);
      return { ok: true };
    }
  }

  if (path === '/audit') {
    const page = Number(q.get('page') || 1);
    const per = Number(q.get('per_page') || 50);
    return { items: audit.slice((page - 1) * per, page * per), total: audit.length };
  }

  throw notFound();
}

export function mockTransport(apiBase: string) {
  return async (url: string, init: RequestInit): Promise<Response> => {
    const u = new URL(url, window.location.origin);
    const path = u.pathname.slice(u.pathname.indexOf(apiBase) + apiBase.length) || '/';
    const method = (init.method || 'GET').toUpperCase();
    const body = init.body ? JSON.parse(String(init.body)) : {};
    const fast = path === '/system/stats' || path === '/core/logs';
    await new Promise((r) => setTimeout(r, fast ? 60 : 180 + Math.random() * 320));
    if (init.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
    // ACME takes a while for real (HTTP-01 validation): long enough to see the progress state
    if (path === '/certs/acme') await new Promise((r) => setTimeout(r, 4200));
    if (/^\/certs\/\d+\/renew$/.test(path)) await new Promise((r) => setTimeout(r, 2200));
    try {
      const data = route(method, path, u.searchParams, body);
      return new Response(JSON.stringify(data), { status: 200, headers: { 'content-type': 'application/json' } });
    } catch (e) {
      const err = e instanceof HttpError ? e : new HttpError(500, 'internal', (e as Error).message);
      return new Response(JSON.stringify({ error: { code: err.code, message: err.message } }), { status: err.status, headers: { 'content-type': 'application/json' } });
    }
  };
}
