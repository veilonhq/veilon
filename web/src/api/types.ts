export type UserStatus = 'active' | 'disabled' | 'limited' | 'expired' | 'on_hold';
export type ResetStrategy = 'no_reset' | 'day' | 'week' | 'month';

/** Sub-admin permission flags (sudo admins hold all of them). */
export type Perm =
  | 'users_view'
  | 'users_create'
  | 'users_edit'
  | 'users_delete'
  | 'all_users'
  | 'inbounds'
  | 'settings'
  | 'certs'
  | 'logs'
  | 'groups';

export interface Admin {
  id: number;
  username: string;
  is_sudo: boolean;
  /** a disabled admin cannot sign in and its tokens are refused */
  disabled?: boolean;
  created_at?: string;
  /** granted flags; missing on old servers (treated as full access) */
  permissions?: Perm[];
  /** max users this admin may own, 0 = no cap */
  user_quota?: number;
  /** max bytes of data limits it may hand out, 0 = no cap */
  traffic_quota?: number;
  /** GET /admins only: users owned and bytes already allocated */
  users?: number;
  traffic_allocated?: number;
}

export interface AdminPatch {
  permissions?: Perm[];
  user_quota?: number;
  traffic_quota?: number;
  is_sudo?: boolean;
  disabled?: boolean;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  admin: Admin;
}

export type EngineName = 'xray' | 'sing-box' | 'mtg' | 'amneziawg';

export interface CoreInfo {
  name: EngineName | string;
  version: string;
  running: boolean;
  started_at: string | null;
  error?: string;
  /** false: the engine has no inbounds and is not running */
  enabled?: boolean;
  protocols?: string[];
  instances?: number;
  /** inbounds the engine had to leave out (e.g. TUN without CAP_NET_ADMIN) */
  warnings?: string[];
  /** switched off by an admin; stays off until switched on */
  stopped?: boolean;
  /** a running maintenance job ("install" | "geo") */
  busy?: string;
}

export interface SystemStats {
  cpu_percent: number;
  mem_used: number;
  mem_total: number;
  disk_used: number;
  disk_total: number;
  /** 0 when the host has no swap */
  swap_used: number;
  swap_total: number;
  cpu_cores: number;
  cpu_model: string;
  /** 1, 5 and 15 minute load average */
  load_avg: [number, number, number];
  uptime_sec: number;
  /** measured host network rate, bytes per second (averaged over net_interval_ms) */
  net_up_bps: number;
  net_down_bps: number;
  /** when the rates were measured (RFC 3339 UTC); a repeated value means no new measurement */
  net_sampled_at?: string | null;
  net_interval_ms?: number;
  users_total: number;
  users_active: number;
  users_online: number;
  users_expired: number;
  users_limited: number;
  users_disabled?: number;
  users_on_hold?: number;
  traffic_total_up: number;
  traffic_total_down: number;
  core: CoreInfo;
  /** every engine (xray, sing-box, mtg, amneziawg) */
  cores?: CoreInfo[];
  /** engines an admin switched off (they stay off across panel restarts) */
  stopped_cores?: string[];
}

export type TrafficRange = '24h' | '7d' | '30d';
/** One server's share of the traffic in a window (id 0 = main server). */
export interface ServerTraffic {
  id: number;
  name: string;
  removed?: boolean;
  up: number;
  down: number;
}
export interface TrafficPoint {
  t: string;
  up: number;
  down: number;
}

export interface User {
  id: number;
  username: string;
  status: UserStatus;
  data_limit: number;
  used_traffic: number;
  up: number;
  down: number;
  expire_at: string | null;
  on_hold_days: number | null;
  reset_strategy: ResetStrategy;
  /** private admin note (never shown to the user) */
  note: string;
  /** shown at the bottom of the user's subscription page ('' = none) */
  description: string;
  inbound_ids: number[];
  uuid: string;
  password: string;
  /** custom login for HTTP, Mixed and SOCKS inbounds ('' = automatic) */
  proxy_username?: string;
  proxy_password?: string;
  /** the user's own block list: domains, full:/regexp: names, IPs, CIDRs (canonical) */
  firewall?: string[];
  sub_token: string;
  sub_url: string;
  online: boolean;
  last_online_at: string | null;
  created_at: string;
  updated_at: string;
  /* per-user limits (0 / false / "" = off) */
  ip_limit: number;
  device_limit: number;
  require_hwid: boolean;
  /** Mbps, 0 = unlimited */
  speed_limit_down: number;
  speed_limit_up: number;
  /** counted usage = real usage x multiplier (1 = none) */
  traffic_multiplier: number;
  /** bytes per panel-time day, 0 = none */
  daily_limit: number;
  daily_used: number;
  block_torrent: boolean;
  /** "HH:MM" in panel time, empty = always */
  window_from: string;
  window_to: string;
  /** weekday bit mask, bit 0 = Sunday, 0 = every day */
  window_days: number;
  /** read-only: "window" | "daily" while temporarily kept out of the cores */
  suspend_reason: '' | 'window' | 'daily';
  group_id: number | null;
  admin_id: number | null;
}

/** The limit fields shared by users and group defaults. */
export type UserLimits = Pick<
  User,
  | 'ip_limit'
  | 'device_limit'
  | 'require_hwid'
  | 'speed_limit_down'
  | 'speed_limit_up'
  | 'traffic_multiplier'
  | 'daily_limit'
  | 'block_torrent'
  | 'window_from'
  | 'window_to'
  | 'window_days'
>;

export interface UserInput extends Partial<UserLimits> {
  username: string;
  data_limit: number;
  expire_at: string | null;
  on_hold_days: number | null;
  reset_strategy: ResetStrategy;
  note: string;
  description: string;
  inbound_ids: number[];
  group_id?: number | null;
  /** '' = automatic login on HTTP, Mixed and SOCKS inbounds */
  proxy_username?: string;
  proxy_password?: string;
  /** replaces the user's block list ([] clears it) */
  firewall?: string[];
}

export interface GroupDefaults extends Partial<UserLimits> {
  data_limit?: number;
  expire_days?: number;
  on_hold_days?: number;
  reset_strategy?: ResetStrategy;
}

export interface Group {
  id: number;
  name: string;
  color: string;
  ad_block: boolean;
  inbound_ids: number[];
  defaults: GroupDefaults;
  users: number;
  created_at: string;
}

export type GroupInput = Pick<Group, 'name' | 'color' | 'ad_block' | 'inbound_ids' | 'defaults'>;

export interface ConnIP {
  id: number;
  ip: string;
  country: string;
  first_seen: string;
  last_seen: string;
  count: number;
  blocked: boolean;
  live: boolean;
}

export interface ConnDevice {
  id: number;
  hwid: string;
  os: string;
  os_version: string;
  model: string;
  user_agent: string;
  ip: string;
  country: string;
  first_seen: string;
  last_seen: string;
  count: number;
  blocked: boolean;
}

export interface UserConnections {
  ips: ConnIP[];
  devices: ConnDevice[];
}

export type UserPatch = Partial<UserInput> & { status?: 'active' | 'disabled' };

export interface Paged<T> {
  items: T[];
  total: number;
}

export interface UserLink {
  inbound_id: number;
  remark: string;
  protocol?: Protocol;
  /** "" for protocols without a share-link standard (amneziawg, masque) */
  uri: string;
  /** WireGuard / AmneziaWG client config file */
  conf?: string;
  conf_name?: string;
  /** Server download URL for the .conf file (octet-stream attachment). */
  conf_url?: string;
}

export type BulkAction = 'delete' | 'disable' | 'enable' | 'reset_traffic' | 'extend' | 'set_group';

export type Protocol =
  | 'vless'
  | 'vmess'
  | 'trojan'
  | 'shadowsocks'
  | 'hysteria'
  | 'tuic'
  | 'masque'
  | 'wireguard'
  | 'amneziawg'
  | 'tun'
  | 'http'
  | 'mixed'
  | 'socks'
  | 'tunnel'
  | 'mtproto'
  | 'shadowtls'
  | 'anytls'
  | 'naive';
export type Network = 'tcp' | 'ws' | 'grpc' | 'httpupgrade' | 'xhttp' | 'kcp';
/** informational network of non-v2 protocols */
export type InfoNetwork = Network | 'udp' | 'quic' | 'h2' | 'tun' | 'tcp,udp';

export interface AwgParams {
  jc: number;
  jmin: number;
  jmax: number;
  s1: number;
  s2: number;
  h1: number;
  h2: number;
  h3: number;
  h4: number;
}

/** Protocol-specific options; fields that do not apply are reset by the server. */
export interface InboundSettings {
  obfs_password: string;
  up_mbps: number;
  down_mbps: number;
  congestion: '' | 'bbr' | 'cubic' | 'new_reno';
  private_key: string;
  public_key: string;
  address: string;
  dns: string[];
  keepalive: number;
  awg: AwgParams | null;
  mtu: number;
  udp: boolean;
  /** socks: no authentication */
  no_auth?: boolean;
  target_address: string;
  target_port: number;
  tunnel_network: '' | 'tcp' | 'udp' | 'tcp,udp';
  tun_name: string;
  gateway: string[];
  routes: string[];
  faketls_domain: string;
  pool: string[];
  path: string;
  /** hysteria: port hopping ranges ("20000-30000,40000"; "" = off) */
  hop_ports: string;
  /** hysteria: seconds between client hops (0 = client default) */
  hop_interval: number;
  /** shadowtls: the TLS 1.3 site whose handshake is borrowed */
  handshake_server: string;
  handshake_port: number;
  /** anytls: padding scheme lines ([] = sing-box default) */
  padding_scheme: string[];
}
export type Security = 'none' | 'tls' | 'reality';

export interface TlsSettings {
  cert_id: number | null;
  sni: string;
  alpn: string[];
  fingerprint: string;
  allow_insecure: boolean;
}

export interface RealitySettings {
  dest: string;
  server_names: string[];
  private_key: string;
  public_key: string;
  short_ids: string[];
  fingerprint: string;
  spider_x: string;
}

/** VLESS Encryption (Xray decryption/encryption). The server keeps the private key; clients get the public one. */
export interface VlessEnc {
  enabled: boolean;
  /** server authentication key: X25519, or ML-KEM-768 (post-quantum) */
  auth: 'x25519' | 'mlkem768';
  mode: 'native' | 'xorpub' | 'random';
  /** 0rtt: clients may resume with a ticket; 1rtt: full handshake every time */
  rtt: '0rtt' | '1rtt';
  /** ticket lifetime for 0rtt (seconds) */
  ticket_seconds: number;
  /** X25519 private key or ML-KEM-768 seed; '' = generated on save */
  private_key: string;
  /** read-only: the client key */
  public_key: string;
  /** read-only: the client "encryption" string ('' while off) */
  encryption?: string;
}

export interface VlessEncKeys {
  auth: VlessEnc['auth'];
  private_key: string;
  public_key: string;
  source: 'xray' | 'go';
}

/** Optional security/transport settings. Every unset field = the old behaviour. */
export interface InboundAdvanced {
  sniffing?: { dest_override?: string[]; route_only?: boolean; metadata_only?: boolean; domains_excluded?: string[] };
  sockopt?: {
    accept_proxy_protocol?: boolean;
    tcp_fast_open?: boolean;
    keepalive_interval?: number;
    keepalive_idle?: number;
    mark?: number;
    v6only?: boolean;
    congestion?: string;
  };
  fallbacks?: AdvFallback[];
  tls?: {
    min_version?: string;
    max_version?: string;
    cipher_suites?: string;
    reject_unknown_sni?: boolean;
    /** secret; the client list is derived */
    ech_server_keys?: string;
    ech_config_list?: string;
    pin_cert?: boolean;
  };
  reality?: {
    /** secret; the client verify key is derived */
    mldsa65_seed?: string;
    mldsa65_verify?: string;
    min_client_ver?: string;
    max_client_ver?: string;
    max_time_diff?: number;
    limit_fallback_upload?: AdvLimit;
    limit_fallback_download?: AdvLimit;
  };
  tcp?: { type: string; host?: string[]; path?: string[] };
  ws?: { headers?: Record<string, string>; heartbeat_period?: number };
  grpc?: { user_agent?: string; idle_timeout?: number; health_check_timeout?: number; permit_without_stream?: boolean; initial_windows_size?: number };
  xhttp?: {
    x_padding_bytes?: string;
    no_sse_header?: boolean;
    sc_max_each_post_bytes?: string;
    sc_max_buffered_posts?: number;
    sc_stream_up_server_secs?: string;
    xmux?: { max_concurrency?: string; max_connections?: string; c_max_reuse_times?: string; h_max_request_times?: string; h_max_reusable_secs?: string; h_keep_alive_period?: number };
    download?: { address: string; port?: number; sni?: string; host?: string };
  };
  kcp?: {
    mtu?: number;
    tti?: number;
    uplink_capacity?: number;
    downlink_capacity?: number;
    cwnd_multiplier?: number;
    max_sending_window?: number;
    header?: string;
    header_domain?: string;
    seed?: string;
  };
  hysteria?: {
    congestion?: '' | 'bbr' | 'brutal' | 'force-brutal';
    brutal_up_mbps?: number;
    brutal_down_mbps?: number;
    udp_idle_timeout?: number;
    masquerade?: { type: 'proxy' | 'string'; url?: string; rewrite_host?: boolean; content?: string; status_code?: number; headers?: Record<string, string> } | null;
  };
  tuic?: { zero_rtt?: boolean; heartbeat?: number; udp_relay_mode?: '' | 'native' | 'quic' };
  ss?: { no_udp?: boolean };
  masks?: { fragment?: { packets: string; length: string; delay?: string } | null; noise?: { rand: string; delay?: string } | null };
  reverse?: { enabled: boolean; uuid: string; route_self: boolean; route_inbound_ids?: number[] };
}
export interface AdvFallback {
  name?: string;
  alpn?: string;
  path?: string;
  dest: string;
  xver?: number;
}
export interface AdvLimit {
  after_bytes?: number;
  bytes_per_sec?: number;
  burst_bytes_per_sec?: number;
}
/** A generated server secret and its client half. */
export interface KeyPair {
  server: string;
  client: string;
  source: 'xray' | 'go';
}

export interface Transport {
  path: string;
  host: string;
  service_name: string;
  mode: string;
}

export interface Inbound {
  id: number;
  remark: string;
  enabled: boolean;
  protocol: Protocol;
  port: number;
  listen: string;
  network: InfoNetwork;
  security: Security;
  tls: TlsSettings | null;
  reality: RealitySettings | null;
  transport: Transport;
  flow: '' | 'xtls-rprx-vision';
  /** vless only (null for other protocols) */
  vless_encryption?: VlessEnc | null;
  /** optional security/transport settings (null = none) */
  advanced?: InboundAdvanced | null;
  ss_method: string;
  sniffing: boolean;
  address_override: string;
  settings: InboundSettings;
  user_count: number;
  /** number of hosts (link overrides) */
  host_count?: number;
  /** nodes that run the inbound ([] = the main server only) */
  node_ids?: number[];
  /** the main server runs it as well as its nodes (older assignments) */
  serve_main?: boolean;
  /** read-only: the main server runs it (no nodes, or serve_main) */
  on_main?: boolean;
  /** read-only: features that need a minimum Xray-core version */
  xray_needs?: XrayNeed[] | null;
  /** read-only: servers (node_id 0 = main) whose Xray-core is too old; the inbound is left out there */
  xray_too_old?: { node_id: number; version: string; needs: XrayNeed[] }[] | null;
  created_at: string;
}

/** A feature of an inbound and the Xray-core version it needs. */
export interface XrayNeed {
  feature: string;
  min: string;
}

/** A link override of an inbound: one share/subscription link per enabled host. */
export interface InboundHost {
  id?: number;
  remark: string;
  address: string;
  /** 0 = the inbound's port */
  port: number;
  sni: string;
  host: string;
  path: string;
  fingerprint: string;
  /** comma separated */
  alpn: string;
  enabled: boolean;
}

export type InboundInput = Omit<Inbound, 'id' | 'user_count' | 'host_count' | 'created_at' | 'on_main' | 'node_ids' | 'serve_main' | 'xray_needs' | 'xray_too_old'> & {
  node_ids: number[];
  serve_main: boolean;
};

export interface RealityKeys {
  private_key: string;
  public_key: string;
  short_id: string;
}

export interface WgKeys {
  private_key: string;
  public_key: string;
  awg: AwgParams;
}

export interface Cert {
  id: number;
  /** primary name (first identifier) */
  domain: string;
  /** every name or IP the certificate covers */
  identifiers?: string[];
  type?: 'domain' | 'ip';
  issuer: string;
  not_before?: string | null;
  not_after: string;
  auto_renew: boolean;
  source: 'acme' | 'upload' | 'self';
  /** ACME profile: 'shortlived' for IP certificates (about 6 days) */
  profile?: string;
  http_port?: number;
  /** next automatic renewal (ACME + auto_renew only) */
  renew_at?: string | null;
  last_error?: string;
  last_attempt_at?: string | null;
  /** used by the panel or an inbound: cannot be revoked or deleted */
  in_use?: boolean;
}

export interface PublicIPCandidate {
  ip: string;
  family: 'ipv4' | 'ipv6';
  sources: string[];
}
export interface PublicIP {
  ipv4: string;
  ipv6: string;
  candidates: PublicIPCandidate[];
}

/** POST /certs/acme. ip / ipv6: an address, 'auto' (detect), or '' (ipv6 only: none). */
export interface AcmeRequest {
  type: 'domain' | 'ip';
  domain?: string;
  ip?: string;
  ipv6?: string;
  email?: string;
  http_port?: number;
  use_for_panel?: boolean;
}

export interface Settings {
  panel_port: number;
  panel_path: string;
  panel_domain: string;
  panel_cert_id: number | null;
  sub_path: string;
  sub_domain: string;
  sub_title: string;
  sub_update_hours: number;
  server_address: string;
  timezone: string;
  language: 'fa' | 'en';
  /** block ad domains for every user */
  ad_block?: boolean;
  /** block BitTorrent for every user */
  block_torrent?: boolean;
}

export interface AuditEntry {
  id: number;
  admin: string;
  action: string;
  target: string;
  ip: string;
  created_at: string;
}

/** Live status of a remote node. */
export type NodeStatus = 'online' | 'degraded' | 'offline' | 'waiting' | 'disabled';
export interface NodeState {
  status: NodeStatus;
  error?: string;
  checked_at: string | null;
  version: string;
  xray_version: string;
  xray_running: boolean;
  synced: boolean;
  skipped: string[] | null;
  started_at: string | null;
  load1: number;
  /** 0..1 */
  mem_used: number;
  cpus: number;
  latency_ms: number;
  /** CPU architecture ('' from agents before v0.3.5) */
  arch: string;
  /** the agent updates itself from the panel (v0.3.5 and later) */
  can_update: boolean;
  /** the last agent update asked from this panel */
  update: NodeUpdate | null;
  /** capabilities the agent announced */
  caps?: string[] | null;
  /** the agent installs a chosen Xray-core release from the panel (older agents need an agent update first) */
  can_update_xray?: boolean;
  /** the last Xray-core install asked from this panel (same shape as an agent update) */
  xray_update?: NodeUpdate | null;
  /** inbounds the agent is too old to run (AmneziaWG / MTProto before v0.3.8) */
  needs_update?: { id: number; remark: string; protocol: Protocol }[] | null;
  /** inbounds left out because the node's Xray-core is too old for their settings */
  xray_too_old?: { id: number; remark: string; needs: XrayNeed[] }[] | null;
  /** the node runs AmneziaWG inbounds but its agent cannot apply users' firewall lists there (needs an agent update) */
  userfw_needs_update?: boolean;
}
export type NodeUpdatePhase = 'downloading' | 'verifying' | 'restarting' | 'done' | 'failed';
export interface NodeUpdate {
  target: string;
  from: string;
  phase: NodeUpdatePhase;
  error?: string;
  rolled_back?: boolean;
  started_at: string;
  updated_at: string;
  finished_at?: string | null;
  by?: string;
}
export interface NodeUpdateResult {
  id: number;
  name: string;
  ok: boolean;
  code?: string;
  message?: string;
}
/** A remote server running "veilon node" that serves chosen inbounds. */
export interface VNode {
  id: number;
  name: string;
  /** what clients connect to (also used in subscription links) */
  address: string;
  /** the agent port */
  api_port: number;
  enabled: boolean;
  up: number;
  down: number;
  last_seen_at: string | null;
  created_at: string;
  /** read-only: inbounds this node runs (chosen in each inbound's form) */
  inbound_ids: number[];
  state: NodeState;
  /** the main server's version when this node's agent is older ('' otherwise) */
  update_to: string;
}
export interface NodeInput {
  name: string;
  address: string;
  api_port: number;
  enabled: boolean;
}
export interface NodeInstall {
  token: string;
  command: string;
  port: number;
}

/* ---------- Telegram admin bot ---------- */
export interface TelegramNotify {
  traffic: boolean;
  expiry: boolean;
  nodes: boolean;
  update: boolean;
}
export interface TelegramStatus {
  running: boolean;
  username: string;
  error: string;
  since: string | null;
}
/** GET/PUT /telegram. The token itself never comes back: only token_set and a hint. */
export interface TelegramSettings {
  enabled: boolean;
  admins: number[];
  proxy: string;
  notify: TelegramNotify;
  token_set: boolean;
  token_hint: string;
  max_admins: number;
  status: TelegramStatus;
}
export interface TelegramInput {
  enabled?: boolean;
  admins?: number[];
  proxy?: string;
  notify?: TelegramNotify;
  /** "" or missing keeps the stored token */
  token?: string;
  clear_token?: boolean;
}
export interface TelegramTestResult {
  bot: { id: number; username: string; name: string };
  admins: { id: number; ok: boolean; error?: string }[];
}
