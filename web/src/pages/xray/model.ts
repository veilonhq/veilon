/**
 * The Xray config template (server: xray.Template) and the plain form models
 * the editor uses for outbounds, rules and balancers. Outbounds, rules and
 * balancers stay native Xray objects; the forms read the fields they know and
 * write them back onto a copy, so keys the forms do not show (mux, sockopt,
 * proxySettings…) survive an edit. Keys starting with "_" are panel
 * metadata ("_disabled", "_name").
 */
export type Obj = Record<string, unknown>;

export interface XrayTemplate {
  log: { loglevel?: string; dnsLog?: boolean };
  dns?: Obj;
  outbounds: Obj[];
  routing: { domainStrategy?: string; rules: Obj[]; balancers: Obj[] };
  observatory?: Obj;
  burstObservatory?: Obj;
  block_private?: boolean;
}

export const RESERVED = ['direct', 'blocked'];
export const OUT_PROTOCOLS = ['vless', 'vmess', 'trojan', 'shadowsocks', 'socks', 'http', 'wireguard', 'freedom', 'blackhole'] as const;
export type OutProtocol = (typeof OUT_PROTOCOLS)[number];
export const SS_METHODS = ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305', 'aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305', 'xchacha20-ietf-poly1305', 'none'];
export const NETWORKS = ['raw', 'ws', 'grpc', 'httpupgrade', 'xhttp'] as const;
export const FINGERPRINTS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'random', 'randomized'];
export const TAG_RE = /^[A-Za-z0-9][A-Za-z0-9._@:-]{0,63}$/;

const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v ?? null)) as T;
const s = (v: unknown) => (typeof v === 'string' ? v : typeof v === 'number' ? String(v) : '');
const o = (v: unknown): Obj => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Obj) : {});
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : v === undefined || v === null || v === '' ? [] : [v]);
const list = (v: unknown): string[] => arr(v).map(s).filter(Boolean);
export const splitWords = (v: string) =>
  v
    .split(/[\n,]+/)
    .map((x) => x.trim())
    .filter(Boolean);

export const isDisabled = (x: Obj) => x._disabled === true;
export const tagOf = (x: Obj) => s(x.tag);

/* ---------------- outbounds ---------------- */

export interface OutForm {
  tag: string;
  protocol: OutProtocol;
  address: string;
  port: string;
  /** vless / vmess id */
  uuid: string;
  /** trojan, shadowsocks, socks/http password */
  password: string;
  method: string;
  flow: string;
  /** socks / http user */
  user: string;
  network: (typeof NETWORKS)[number];
  path: string;
  host: string;
  serviceName: string;
  mode: string;
  security: 'none' | 'tls' | 'reality';
  sni: string;
  fingerprint: string;
  alpn: string;
  publicKey: string;
  shortId: string;
  spiderX: string;
  insecure: boolean;
  /** freedom */
  domainStrategy: string;
  /** wireguard */
  secretKey: string;
  wgAddress: string;
  peerPublicKey: string;
  preSharedKey: string;
  endpoint: string;
  reserved: string;
  mtu: string;
}

export function emptyOut(protocol: OutProtocol = 'vless'): OutForm {
  return {
    tag: '', protocol, address: '', port: '443', uuid: '', password: '', method: SS_METHODS[0], flow: '', user: '',
    network: 'raw', path: '/', host: '', serviceName: '', mode: '', security: protocol === 'trojan' ? 'tls' : 'none', sni: '',
    fingerprint: 'chrome', alpn: '', publicKey: '', shortId: '', spiderX: '', insecure: false, domainStrategy: 'AsIs',
    secretKey: '', wgAddress: '10.0.0.2/32', peerPublicKey: '', preSharedKey: '', endpoint: '', reserved: '', mtu: '1420',
  };
}

export const hasStream = (p: OutProtocol) => p === 'vless' || p === 'vmess' || p === 'trojan';

/** Read an Xray outbound into the form model. */
export function outToForm(x: Obj): OutForm {
  const proto = (OUT_PROTOCOLS as readonly string[]).includes(s(x.protocol)) ? (s(x.protocol) as OutProtocol) : 'freedom';
  const f = emptyOut(proto);
  f.tag = tagOf(x);
  const st = o(x.settings);
  const srv = o(arr(st.vnext ?? st.servers)[0]);
  f.address = s(srv.address);
  f.port = s(srv.port) || f.port;
  const usr = o(arr(srv.users)[0]);
  switch (proto) {
    case 'vless':
    case 'vmess':
      f.uuid = s(usr.id);
      f.flow = s(usr.flow);
      break;
    case 'trojan':
      f.password = s(srv.password);
      break;
    case 'shadowsocks':
      f.password = s(srv.password);
      f.method = s(srv.method) || f.method;
      break;
    case 'socks':
    case 'http':
      f.user = s(usr.user);
      f.password = s(usr.pass);
      break;
    case 'freedom':
      f.domainStrategy = s(st.domainStrategy) || 'AsIs';
      break;
    case 'wireguard': {
      const peer = o(arr(st.peers)[0]);
      f.secretKey = s(st.secretKey);
      f.wgAddress = list(st.address).join(', ');
      f.peerPublicKey = s(peer.publicKey);
      f.preSharedKey = s(peer.preSharedKey);
      f.endpoint = s(peer.endpoint);
      f.reserved = arr(st.reserved).map(s).join(',');
      f.mtu = s(st.mtu) || '1420';
      break;
    }
  }
  const ss = o(x.streamSettings);
  const net = s(ss.network) === 'tcp' || !ss.network ? 'raw' : s(ss.network);
  f.network = ((NETWORKS as readonly string[]).includes(net) ? net : 'raw') as OutForm['network'];
  const ns = o(ss[`${f.network}Settings`]);
  f.path = s(ns.path) || '/';
  f.host = s(ns.host);
  f.mode = s(ns.mode);
  f.serviceName = s(ns.serviceName);
  if (f.network === 'grpc') f.mode = ns.multiMode ? 'multi' : '';
  const sec = s(ss.security);
  f.security = sec === 'tls' || sec === 'reality' ? sec : 'none';
  const ts = o(f.security === 'reality' ? ss.realitySettings : ss.tlsSettings);
  f.sni = s(ts.serverName);
  f.fingerprint = s(ts.fingerprint) || (f.security === 'reality' ? 'chrome' : '');
  f.alpn = list(ts.alpn).join(',');
  f.insecure = ts.allowInsecure === true;
  f.publicKey = s(ts.publicKey);
  f.shortId = s(ts.shortId);
  f.spiderX = s(ts.spiderX);
  return f;
}

/** Problems that make a form unsaveable, as message keys. */
export function outErrors(f: OutForm, otherTags: string[]): Partial<Record<keyof OutForm, string>> {
  const e: Partial<Record<keyof OutForm, string>> = {};
  if (!TAG_RE.test(f.tag)) e.tag = 'xc.err.tag';
  else if (otherTags.includes(f.tag) || f.tag === 'api' || f.tag.startsWith('inbound-')) e.tag = 'xc.err.tagUsed';
  const port = Number(f.port);
  const remote = f.protocol !== 'freedom' && f.protocol !== 'blackhole' && f.protocol !== 'wireguard';
  if (remote) {
    if (!f.address.trim()) e.address = 'xc.err.required';
    if (!Number.isInteger(port) || port < 1 || port > 65535) e.port = 'xc.err.port';
  }
  if ((f.protocol === 'vless' || f.protocol === 'vmess') && !f.uuid.trim()) e.uuid = 'xc.err.required';
  if ((f.protocol === 'trojan' || f.protocol === 'shadowsocks') && !f.password) e.password = 'xc.err.required';
  if (hasStream(f.protocol) && f.security === 'reality' && !f.publicKey.trim()) e.publicKey = 'xc.err.required';
  if (f.protocol === 'wireguard') {
    if (!f.secretKey.trim()) e.secretKey = 'xc.err.required';
    if (!f.peerPublicKey.trim()) e.peerPublicKey = 'xc.err.required';
    if (!/^[^\s]+:\d{1,5}$/.test(f.endpoint.trim())) e.endpoint = 'xc.err.endpoint';
    if (f.reserved.trim() && !/^\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}\s*$/.test(f.reserved)) e.reserved = 'xc.err.reserved';
  }
  return e;
}

/** Write the form onto (a copy of) the original outbound. */
export function formToOut(f: OutForm, base?: Obj): Obj {
  const x: Obj = clone(base ?? {}) ?? {};
  const sameProto = s(x.protocol) === f.protocol;
  x.tag = f.tag.trim();
  x.protocol = f.protocol;
  const port = Number(f.port);
  const prevSt = sameProto ? o(x.settings) : {};
  let st: Obj = {};
  switch (f.protocol) {
    case 'vless': {
      const u: Obj = { id: f.uuid.trim(), encryption: 'none' };
      if (f.flow) u.flow = f.flow;
      st = { vnext: [{ address: f.address.trim(), port, users: [u] }] };
      break;
    }
    case 'vmess':
      st = { vnext: [{ address: f.address.trim(), port, users: [{ id: f.uuid.trim(), security: 'auto' }] }] };
      break;
    case 'trojan':
      st = { servers: [{ address: f.address.trim(), port, password: f.password }] };
      break;
    case 'shadowsocks':
      st = { servers: [{ address: f.address.trim(), port, method: f.method, password: f.password }] };
      break;
    case 'socks':
    case 'http': {
      const srv: Obj = { address: f.address.trim(), port };
      if (f.user) srv.users = [{ user: f.user, pass: f.password }];
      st = { servers: [srv] };
      break;
    }
    case 'freedom':
      st = { ...prevSt };
      if (f.domainStrategy && f.domainStrategy !== 'AsIs') st.domainStrategy = f.domainStrategy;
      else delete st.domainStrategy;
      break;
    case 'blackhole':
      st = { ...prevSt };
      break;
    case 'wireguard': {
      const prevPeer = o(arr(prevSt.peers)[0]);
      const peer: Obj = { ...prevPeer, publicKey: f.peerPublicKey.trim(), endpoint: f.endpoint.trim() };
      if (!peer.allowedIPs) peer.allowedIPs = ['0.0.0.0/0', '::/0'];
      if (f.preSharedKey.trim()) peer.preSharedKey = f.preSharedKey.trim();
      else delete peer.preSharedKey;
      st = { ...prevSt, secretKey: f.secretKey.trim(), address: splitWords(f.wgAddress), peers: [peer] };
      const mtu = Number(f.mtu);
      if (mtu > 0) st.mtu = mtu;
      else delete st.mtu;
      if (f.reserved.trim()) st.reserved = f.reserved.split(',').map((n) => Number(n.trim()));
      else delete st.reserved;
      break;
    }
  }
  x.settings = st;
  if (hasStream(f.protocol)) {
    const prev = o(x.streamSettings);
    const ss: Obj = {};
    if (prev.sockopt) ss.sockopt = prev.sockopt;
    ss.network = f.network;
    const ns: Obj = {};
    switch (f.network) {
      case 'ws':
      case 'httpupgrade':
      case 'xhttp':
        ns.path = f.path || '/';
        if (f.host) ns.host = f.host;
        if (f.network === 'xhttp' && f.mode) ns.mode = f.mode;
        ss[`${f.network}Settings`] = ns;
        break;
      case 'grpc':
        ns.serviceName = f.serviceName;
        if (f.mode === 'multi') ns.multiMode = true;
        ss.grpcSettings = ns;
        break;
      default:
        if (prev.network === 'raw' || prev.network === 'tcp') {
          const keep = prev.rawSettings ?? prev.tcpSettings;
          if (keep) ss.rawSettings = keep;
        }
    }
    ss.security = f.security;
    if (f.security === 'tls') {
      const t: Obj = {};
      if (f.sni) t.serverName = f.sni;
      if (f.fingerprint) t.fingerprint = f.fingerprint;
      if (f.alpn) t.alpn = splitWords(f.alpn);
      if (f.insecure) t.allowInsecure = true;
      ss.tlsSettings = t;
    } else if (f.security === 'reality') {
      const r: Obj = { publicKey: f.publicKey.trim(), fingerprint: f.fingerprint || 'chrome' };
      if (f.sni) r.serverName = f.sni;
      if (f.shortId) r.shortId = f.shortId;
      if (f.spiderX) r.spiderX = f.spiderX;
      ss.realitySettings = r;
    }
    x.streamSettings = ss;
  } else if (f.protocol !== 'socks' && f.protocol !== 'http') {
    delete x.streamSettings;
  }
  return x;
}

/** "host:port" (or the endpoint) of an outbound for its card. */
export function outTarget(x: Obj): string {
  const st = o(x.settings);
  if (x.protocol === 'wireguard') return s(o(arr(st.peers)[0]).endpoint);
  const srv = o(arr(st.vnext ?? st.servers)[0]);
  if (!srv.address) return '';
  const a = s(srv.address);
  return `${a.includes(':') ? `[${a}]` : a}:${s(srv.port)}`;
}

/* ---------------- rules ---------------- */

export interface RuleForm {
  name: string;
  domains: string;
  geosite: string;
  ips: string;
  geoip: string;
  port: string;
  sourcePort: string;
  network: '' | 'tcp' | 'udp' | 'tcp,udp';
  protocols: string[];
  inbounds: string[];
  users: string;
  target: 'outbound' | 'balancer';
  outbound: string;
  balancer: string;
}

export const RULE_PROTOCOLS = ['http', 'tls', 'quic', 'bittorrent'];

export function emptyRule(): RuleForm {
  return { name: '', domains: '', geosite: '', ips: '', geoip: '', port: '', sourcePort: '', network: '', protocols: [], inbounds: [], users: '', target: 'outbound', outbound: 'direct', balancer: '' };
}

export function ruleToForm(r: Obj): RuleForm {
  const f = emptyRule();
  f.name = s(r._name) || s(r.ruleTag);
  const dom = list(r.domain);
  f.geosite = dom.filter((d) => d.startsWith('geosite:')).map((d) => d.slice(8)).join(', ');
  f.domains = dom.filter((d) => !d.startsWith('geosite:')).join('\n');
  const ip = list(r.ip);
  f.geoip = ip.filter((d) => d.startsWith('geoip:')).map((d) => d.slice(6)).join(', ');
  f.ips = ip.filter((d) => !d.startsWith('geoip:')).join('\n');
  f.port = s(r.port);
  f.sourcePort = s(r.sourcePort);
  const n = s(r.network);
  f.network = n === 'tcp' || n === 'udp' || n === 'tcp,udp' ? n : '';
  f.protocols = list(r.protocol);
  f.inbounds = list(r.inboundTag);
  f.users = list(r.user).join(', ');
  if (r.balancerTag) {
    f.target = 'balancer';
    f.balancer = s(r.balancerTag);
  } else f.outbound = s(r.outboundTag) || 'direct';
  return f;
}

/** True when the rule matches something (an empty rule would catch all traffic). */
export function ruleHasCondition(f: RuleForm) {
  return !!(f.domains.trim() || f.geosite.trim() || f.ips.trim() || f.geoip.trim() || f.port.trim() || f.sourcePort.trim() || f.network || f.protocols.length || f.inbounds.length || f.users.trim());
}

export const PORT_RE = /^\s*\d{1,5}(\s*-\s*\d{1,5})?(\s*,\s*\d{1,5}(\s*-\s*\d{1,5})?)*\s*$/;

export function formToRule(f: RuleForm, base?: Obj): Obj {
  const keep: Obj = {};
  for (const [k, v] of Object.entries(base ?? {})) {
    // keep fields the form does not edit (attrs, source, localPort…)
    if (!['_name', 'ruleTag', 'domain', 'ip', 'port', 'sourcePort', 'network', 'protocol', 'inboundTag', 'user', 'outboundTag', 'balancerTag', 'type'].includes(k)) keep[k] = clone(v);
  }
  const r: Obj = { ...keep };
  if (f.name.trim()) r._name = f.name.trim();
  const dom = [...splitWords(f.domains), ...splitWords(f.geosite).map((g) => (g.startsWith('geosite:') ? g : `geosite:${g}`))];
  if (dom.length) r.domain = dom;
  const ip = [...splitWords(f.ips), ...splitWords(f.geoip).map((g) => (g.startsWith('geoip:') ? g : `geoip:${g}`))];
  if (ip.length) r.ip = ip;
  if (f.port.trim()) r.port = f.port.replace(/\s+/g, '');
  if (f.sourcePort.trim()) r.sourcePort = f.sourcePort.replace(/\s+/g, '');
  if (f.network) r.network = f.network;
  if (f.protocols.length) r.protocol = [...f.protocols];
  if (f.inbounds.length) r.inboundTag = [...f.inbounds];
  const users = splitWords(f.users);
  if (users.length) r.user = users;
  if (f.target === 'balancer') r.balancerTag = f.balancer;
  else r.outboundTag = f.outbound;
  return r;
}

/* ---------------- balancers ---------------- */

export interface BalancerForm {
  tag: string;
  selector: string[];
  strategy: 'random' | 'roundRobin' | 'leastPing' | 'leastLoad';
  fallback: string;
}
export const STRATEGIES: BalancerForm['strategy'][] = ['random', 'roundRobin', 'leastPing', 'leastLoad'];

export function balancerToForm(b: Obj): BalancerForm {
  const st = s(o(b.strategy).type);
  return {
    tag: tagOf(b),
    selector: list(b.selector),
    strategy: (STRATEGIES as string[]).includes(st) ? (st as BalancerForm['strategy']) : 'random',
    fallback: s(b.fallbackTag),
  };
}

export function formToBalancer(f: BalancerForm, base?: Obj): Obj {
  const b: Obj = clone(base ?? {}) ?? {};
  b.tag = f.tag.trim();
  b.selector = [...f.selector];
  const prev = o(b.strategy);
  b.strategy = f.strategy === 'random' ? undefined : { ...(prev.type === f.strategy ? prev : {}), type: f.strategy };
  if (!b.strategy) delete b.strategy;
  if (f.fallback) b.fallbackTag = f.fallback;
  else delete b.fallbackTag;
  return b;
}

/** Outbound tags a balancer selector picks (prefix match, like Xray). */
export const selected = (selector: string[], tags: string[]) => tags.filter((t) => selector.some((p) => p && t.startsWith(p)));

/* ---------------- DNS ---------------- */

export interface DnsServerRow {
  address: string;
  domains: string;
}
export interface DnsForm {
  enabled: boolean;
  servers: DnsServerRow[];
  queryStrategy: 'UseIP' | 'UseIPv4' | 'UseIPv6';
  disableCache: boolean;
  hosts: string;
}

export function dnsToForm(d?: Obj): DnsForm {
  const dns = o(d);
  const servers: DnsServerRow[] = arr(dns.servers).map((x) =>
    typeof x === 'string' ? { address: x, domains: '' } : { address: s(o(x).address), domains: list(o(x).domains).join(', ') },
  );
  const hosts = Object.entries(o(dns.hosts))
    .map(([k, v]) => `${k} ${list(v).join(' ')}`)
    .join('\n');
  const q = s(dns.queryStrategy);
  return {
    enabled: !!d && Object.keys(dns).length > 0,
    servers,
    queryStrategy: q === 'UseIPv4' || q === 'UseIPv6' ? q : 'UseIP',
    disableCache: dns.disableCache === true,
    hosts,
  };
}

export function formToDns(f: DnsForm, base?: Obj): Obj | undefined {
  if (!f.enabled) return undefined;
  const d: Obj = clone(base ?? {}) ?? {};
  d.servers = f.servers
    .filter((r) => r.address.trim())
    .map((r) => {
      const doms = splitWords(r.domains);
      if (!doms.length) return r.address.trim();
      const prev = arr(o(base).servers).map(o).find((x) => s(x.address) === r.address.trim()) ?? {};
      return { ...prev, address: r.address.trim(), domains: doms };
    });
  if (f.queryStrategy !== 'UseIP') d.queryStrategy = f.queryStrategy;
  else delete d.queryStrategy;
  if (f.disableCache) d.disableCache = true;
  else delete d.disableCache;
  const hosts: Obj = {};
  for (const line of f.hosts.split('\n')) {
    const [k, ...v] = line.trim().split(/\s+/);
    if (k && v.length) hosts[k] = v.length === 1 ? v[0] : v;
  }
  if (Object.keys(hosts).length) d.hosts = hosts;
  else delete d.hosts;
  return d;
}

/* ---------------- whole template ---------------- */

/** Errors the server would reject, found locally so the admin sees them before Apply. */
export function templateProblems(t: XrayTemplate): string[] {
  const out: string[] = [];
  const tags = t.outbounds.map(tagOf);
  const bal = t.routing.balancers.map(tagOf);
  t.routing.rules.forEach((r, i) => {
    const target = s(r.outboundTag) || s(r.balancerTag);
    if (r.outboundTag && !tags.includes(s(r.outboundTag))) out.push(`#${i + 1}: ${target}`);
    if (r.balancerTag && !bal.includes(s(r.balancerTag))) out.push(`#${i + 1}: ${target}`);
  });
  return out;
}

/** Move item i by d (−1 up, +1 down). */
export function move<T>(a: T[], i: number, d: number): T[] {
  const j = i + d;
  if (j < 0 || j >= a.length) return a;
  const b = [...a];
  [b[i], b[j]] = [b[j], b[i]];
  return b;
}

/** Rename an outbound everywhere it is referenced (rules, balancer fallbacks). */
export function renameOutbound(t: XrayTemplate, from: string, to: string): XrayTemplate {
  if (from === to || !from) return t;
  return {
    ...t,
    routing: {
      ...t.routing,
      rules: t.routing.rules.map((r) => (r.outboundTag === from ? { ...r, outboundTag: to } : r)),
      balancers: t.routing.balancers.map((b) => (b.fallbackTag === from ? { ...b, fallbackTag: to } : b)),
    },
  };
}

/** Which rules and balancers use an outbound (blocks deleting it). */
export function outboundUsers(t: XrayTemplate, tag: string): number {
  return t.routing.rules.filter((r) => r.outboundTag === tag).length + t.routing.balancers.filter((b) => b.fallbackTag === tag).length;
}
