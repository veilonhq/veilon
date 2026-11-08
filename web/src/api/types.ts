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
  net_up_bps: number;
  net_down_bps: number;
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
}

export type TrafficRange = '24h' | '7d' | '30d';
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
  note: string;
  inbound_ids: number[];
  uuid: string;
  password: string;
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
  inbound_ids: number[];
  group_id?: number | null;
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
  | 'tunnel'
  | 'mtproto';
export type Network = 'tcp' | 'ws' | 'grpc' | 'httpupgrade' | 'xhttp';
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
  target_address: string;
  target_port: number;
  tunnel_network: '' | 'tcp' | 'udp' | 'tcp,udp';
  tun_name: string;
  gateway: string[];
  routes: string[];
  faketls_domain: string;
  pool: string[];
  path: string;
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
  ss_method: string;
  sniffing: boolean;
  address_override: string;
  settings: InboundSettings;
  user_count: number;
  created_at: string;
}

export type InboundInput = Omit<Inbound, 'id' | 'user_count' | 'created_at'>;

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
