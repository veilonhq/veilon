# Veilon API v1

Base: `/<panel_path>/api/v1` (panel_path is random, set at install; the web UI is served at `/<panel_path>/`). JSON everywhere. Auth: `Authorization: Bearer <jwt>`.
Errors: `{"error": {"code": "string_code", "message": "human text"}}` with proper HTTP status.
Times: RFC3339 UTC. Bytes: integers. `0` limit = unlimited.

## Auth
- `POST /auth/login` `{username, password}` → `{token, expires_at, admin:{id,username,is_sudo}}` (rate-limited: 5 fails / 10 min / IP → 429)
- `GET /auth/me` → admin
- `PATCH /auth/me` `{username, current_password}` → `{token, expires_at, admin}` — the signed-in admin renames themself. Requires the current password (`400 invalid_credentials` if wrong); username must be 3-32 chars of letters, digits, `_ . -` (`400 validation_error`) and unused by another admin (`409 conflict`). Audit-logged as `auth.username_changed` (`old -> new`). Existing tokens stay valid; the fresh token carries the new username.
- `POST /auth/password` `{current_password, new_password}` → `{ok, token, expires_at}` (tokens issued before the change stop working, so the response carries a fresh token)

## System
- `GET /system/stats` → `{cpu_percent, mem_used, mem_total, disk_used, disk_total, swap_used, swap_total, cpu_cores, cpu_model, load_avg:[1m,5m,15m], uptime_sec, net_up_bps, net_down_bps, net_sampled_at, net_interval_ms, users_total, users_active, users_online, users_expired, users_limited, users_disabled, users_on_hold, traffic_total_up, traffic_total_down, core:{name:"xray", version, running, started_at, error?, enabled, protocols:[], instances, warnings:[], stopped}, cores:[Core], stopped_cores:[name]}`. `cores` lists every engine (`xray`, `sing-box`, `mtg`, `amneziawg`) with the same shape (the web UI currently shows only `xray`; the other engines keep running); `enabled:false` means the engine has no inbounds and is not running; `warnings` lists inbounds the engine had to leave out (e.g. TUN without CAP_NET_ADMIN). Sizes are bytes; `swap_total` is 0 when the host has no swap; `cpu_cores` is the logical CPU count; `cpu_model` may be empty. `net_up_bps`/`net_down_bps` are the host's measured network rate in bytes per second over the window of `net_interval_ms` that ended at `net_sampled_at` (RFC3339 UTC, `null` before the second sample); the sampler runs every few seconds, so polling faster returns the same sample again (same `net_sampled_at`).
- `GET /system/stats` also returns `traffic_up_bps`/`traffic_down_bps`: the counted user-traffic rate (bytes per second over the last 30 s). `traffic_total_up`/`traffic_total_down` are the sum of all users' `up`/`down`, the same numbers the users list shows.
- `GET /system/traffic?range=24h|7d|30d[&bucket=5m|15m|1h|6h|1d][&server=all|main|<node id>]` → `{points:[{t, up, down}], bucket_sec, range, timezone, total_up, total_down, servers:[{id, name, removed?, up, down}]}` (zero-filled, oldest first; default buckets: 24h→15 min, 7d→1 h, 30d→6 h). Points are the counted user traffic per bucket, on all servers by default; `server=main` charts the main server only and `server=<node id>` one node. `servers` is every server's share of the same window (`id` 0 is the main server; `removed:true` marks a deleted node) and always sums exactly to the all-servers total. Buckets are aligned to the panel `timezone` (Settings) and restart at every local midnight, also across daylight-saving changes. `400 bad_request` when the bucket does not fit the range or `server` is not `all`, `main` or a node id.
- `POST /core/restart` (restarts every engine), `GET /core/logs?core=xray|sing-box|mtg|amneziawg|panel|all&lines=200` → `{entries:[{seq, t, source, level, msg}], lines:[string]}` (default core `xray`, `lines` max 2000 = everything a source keeps). `panel` is the panel's own log; `all` merges every engine and the panel by time (last `lines` entries). `t` is the RFC3339 UTC time the line was written (the engine's own timestamp when the line has one, else when the panel read it; banner lines without one take the next line's time; `null` when unknown), `level` is `debug|info|warning|error`, `msg` is the line without its timestamp, `seq` orders lines across sources (0 when unknown). `lines` is the same as `"<t> <msg>"` text for older clients.
- Xray core control (permission `settings`):
  - `GET /core/xray` → `{status:Core, job:Job|null, geo:[{name, size, updated_at}], stopped_cores:[name]}`
  - `POST /core/stop`, `POST /core/start` with `scope` = `xray|all` (query `?scope=` or JSON body `{scope}`) → `{ok, scope, changed:[name], status:Core, cores:[Core], stopped_cores:[name]}`. `xray` switches only Xray-core; `all` switches every engine (`xray`, `sing-box`, `amneziawg`, `mtg`). Default scope: `xray` for stop, `all` for start, so a plain start brings back every core that was switched off. `changed` lists the engines whose state actually changed (engines already in the wanted state are left alone); `400 validation_error` for any other scope. The choice is stored per engine: a stopped core stays off across panel restarts and `POST /core/restart` (`stopped:true` in its Core) until it is started again. Engines that hand traffic to Xray (TUIC) need it running. Audit-logged as `core.stop` / `core.start` with `<scope>: <changed engines>`.
  - Which cores are off: `stopped_cores` in `GET /system/stats`, `GET /core/xray` and the start/stop answers, and `stopped:true` on each Core in `cores`.
  - `GET /core/xray/releases[?refresh=1]` → `{releases:[{tag, prerelease, published_at}], fetched_at, stale, rate_limited_until?, error?}`: official Xray-core releases that have an asset for this server's architecture, cached for 1 h. When GitHub rate-limits, the cached list is served with `stale:true`; without a cache the answer is `429 rate_limited` with `rate_limited_until`.
  - `POST /core/xray/install` `{version:"v26.10.31"}` → `202 {ok, job}`: background job; the download is checked against the release digest, the new binary must pass a version and config test, then it replaces the old one (kept as `.bak`) and Xray restarts; if it does not stay up, the old binary is restored (`job.rolled_back:true`). `409 busy` while another job runs, `400 bad_request` for an unknown version.
  - `POST /core/xray/geo` → `202 {ok, job}`: updates `geoip.dat` and `geosite.dat` (checksum-verified, same verify and rollback).
  - Job: `{kind:"install"|"geo", version?, running, error?, rolled_back?, started_at, finished_at?}`. Poll `GET /core/xray` until `running` is false.
- `GET /core/config?core=…` → generated config of that engine (read-only, for debugging): JSON for xray / sing-box / amneziawg, TOML (`text/plain`) for mtg (one section per MTProto inbound).

## Users
User object:
```
{ id, username, status: "active"|"disabled"|"limited"|"expired"|"on_hold",
  data_limit, used_traffic, up, down, expire_at|null, on_hold_days|null,
  reset_strategy: "no_reset"|"day"|"week"|"month",
  note, inbound_ids:[int], uuid, password, proxy_username, proxy_password, sub_token, sub_url,
  online: bool, last_online_at|null, created_at, updated_at }
```
- `GET /users?search=&status=&page=1&per_page=20&sort=-created_at` → `{items:[User], total}`
  (`search` matches username or note; `status` also accepts `online`; `sort` fields: `created_at`, `username`, `used_traffic`, `data_limit`, `expire_at`, `last_online_at`, `id`, prefix `-` for descending; `per_page` max 500)
- `POST /users` `{username, data_limit, expire_at|null, on_hold_days|null, reset_strategy, note, inbound_ids}` → `201` User
  (`proxy_username` / `proxy_password` (optional, both kinds of request): a custom login for HTTP, Mixed and SOCKS inbounds; `""` = automatic (`<id>.<username>` and `password`). Username 3-32 of letters, digits, `. _ -`, not starting with digits and a dot, unique across users case-insensitively (`409` with `field:"proxy_username"`); password 8-64 of letters, digits and `. _ - ~ ! @ # $ % ^ & * + =`.)
  (`on_hold_days` with no `expire_at` creates an `on_hold` user whose period starts at first connection; if both are given, `expire_at` wins)
- `GET /users/{id}`, `PATCH /users/{id}` (same fields + status active/disabled; only fields present are changed, `null` clears nullable fields), `DELETE /users/{id}` → `{ok}`
- `POST /users/{id}/reset-traffic`, `POST /users/{id}/revoke-sub` (new sub_token + new uuid/password)
- `GET /users/{id}/links` → `{links:[{inbound_id, remark, protocol, uri, conf?, conf_name?, conf_url?}], sub_url}` — `uri` is `""` for protocols without a share-link standard (amneziawg, masque); WireGuard/AmneziaWG links carry the client config file in `conf` (with `conf_name`, e.g. `WG.conf`) and `conf_url`, a direct download of the same file (see Subscription). Tunnel and TUN inbounds have no client links.
- `POST /users/bulk` `{action:"delete"|"disable"|"enable"|"reset_traffic"|"extend", ids:[], days?}` → `{ok, affected}`
  (`extend` adds `days` to `expire_at` — from now if already expired — or to `on_hold_days` for on-hold users; users without expiry are unchanged)

## Inbounds
Inbound object:
```
{ id, remark, enabled,
  protocol: "vless"|"vmess"|"trojan"|"shadowsocks"|"hysteria"|"tuic"|"wireguard"|"amneziawg"
           |"http"|"mixed"|"socks"|"tunnel"|"tun"|"mtproto"|"masque",
  port, listen, network, security: "none"|"tls"|"reality",
  tls: { cert_id|null, sni, alpn:[], fingerprint, allow_insecure:false } | null,
  reality: { dest, server_names:[], private_key, public_key, short_ids:[], fingerprint, spider_x } | null,
  transport: { path, host, service_name, mode },
  flow: ""|"xtls-rprx-vision", ss_method, sniffing: bool,
  settings: Settings, address_override: "", user_count, host_count,
  node_ids:[int], serve_main: bool, on_main: bool /* read-only */, created_at }

Settings (protocol-specific; fields that do not apply are reset to zero values):
{ obfs_password, up_mbps, down_mbps,            // hysteria (Salamander obfs, client bandwidth hints)
  congestion,                                   // tuic: bbr|cubic|new_reno
  private_key, public_key, address, dns:[], keepalive, awg|null,   // wireguard / amneziawg
  mtu,                                          // wireguard, amneziawg, masque, tun
  udp,                                          // mixed, socks: allow SOCKS5 UDP
  no_auth,                                      // socks: no authentication (traffic not counted per user)
  target_address, target_port, tunnel_network,  // tunnel: tcp|udp|tcp,udp
  tun_name, gateway:[], routes:[],              // tun
  faketls_domain,                               // mtproto
  pool:[], path }                               // masque
awg = { jc, jmin, jmax, s1, s2, h1, h2, h3, h4 }
```
Engines: Xray-core serves vless, vmess, trojan, shadowsocks, hysteria (Hysteria 2), wireguard, http, mixed, socks, tunnel, tun and masque; sing-box serves tuic; mtg-multi serves mtproto (one process per inbound); the built-in AmneziaWG sidecar (`veilon awg-run`, amneziawg-go) serves amneziawg. sing-box and mtg-multi are downloaded from their official GitHub releases into the data dir on first use (`VEILON_SINGBOX_VERSION`, default `1.14.4`; `VEILON_MTG_VERSION`, default `1.15.0`; `latest` is allowed).

- `GET /inbounds` → `[Inbound]`, `POST /inbounds` → `201` Inbound, `GET|PATCH|DELETE /inbounds/{id}`
  - PATCH merges the body onto the stored inbound field by field (nested `tls`/`reality`/`transport`/`settings` objects merge too; `null` clears `tls`/`reality`).
  - `port` is required on create for every protocol except TUN (`400 validation_error` without it).
  - **Placement (where the inbound runs).** `node_ids` are the nodes that run the inbound (`[]` = the main server only). `serve_main` says whether the main server runs it as well; `on_main` (read-only) is `true` when the main server runs it (no nodes, or `serve_main`). A create or PATCH that sends `node_ids` without `serve_main` places the inbound on those nodes only (`serve_main:false`, or `true` when `node_ids` is empty). Send `serve_main:true` with `node_ids` to keep the main server too. An inbound with no nodes always has `serve_main:true`. Xray inbounds except TUN, AmneziaWG and MTProto can run on nodes (`400 validation_error` otherwise; an unknown node id too). AmneziaWG / MTProto on a node whose agent is known to lack the capability (agents before v0.3.8; nodes already running the inbound are not re-checked) answer `409` with `code:"node_agent_old"`, `field:"nodes"`, `node_id`, `node_name` and `protocol`. When the main server does not run an inbound, its Xray, firewall rules and IP-limit ports leave it out.
  - Links of an inbound on nodes point at the nodes only: one per enabled node, named `<remark> · <node name>`, with the node `address`, in every format (share links, base64, Clash, sing-box, Xray JSON). Hosts apply on top of each node's link and a host `address` wins. When all its nodes are disabled, an inbound with `serve_main:true` gives its main-server link again. Inbounds assigned to nodes before schema 8 (v0.3.1 and earlier) keep `serve_main:true`. Deleting a node sets inbounds left without nodes back to the main server.
  - Defaults: `enabled:true`, `sniffing:true`, `network:"tcp"`, `security:"none"`; ws/httpupgrade/xhttp path `/`; grpc `service_name:"grpc"`; xhttp `mode:"auto"` (`auto|packet-up|stream-up|stream-one`); grpc `mode` may be `gun|multi`.
  - `network`, `transport`, `flow`, `ss_method` and `reality` only apply to vless/vmess/trojan/shadowsocks. For the other protocols `network` is informational: `udp` (hysteria, tuic, wireguard, amneziawg), `quic`/`h2` (masque), `tun`, the tunnel's network, or `tcp`.
  - Reality: `dest` without port gets `:443`; empty `server_names` defaults to the dest host; empty `private_key` is generated; `public_key` is always derived from `private_key`; empty `short_ids` gets one random id; `fingerprint` defaults to `chrome`, `spider_x` to `/`. Reality is allowed for vless/trojan over tcp/grpc/xhttp.
  - `flow:"xtls-rprx-vision"` requires vless + tcp + tls/reality. Shadowsocks requires security `none` and network `tcp`; `ss_method` ∈ `aes-128-gcm, aes-256-gcm, chacha20-ietf-poly1305, 2022-blake3-aes-128-gcm, 2022-blake3-aes-256-gcm` (2022 server keys are generated automatically).
  - `tls.cert_id` must reference an existing certificate; empty `tls.sni` defaults to the certificate domain. With `tls.allow_insecure:true` share links also carry `pcs` (SHA-256 pin of the certificate) because current Xray-core clients no longer accept `allowInsecure`.
  - hysteria, tuic and masque always use TLS (`security` is forced to `tls`, ALPN `h3`; masque accepts `h3` and/or `h2`). Without `tls.cert_id` a self-signed certificate for `tls.sni` (default `www.bing.com`) is created (or reused) and `allow_insecure` is set: links then pin the certificate (`pinSHA256` in hysteria2://, `fingerprint` in Clash, `certificate_public_key_sha256` in sing-box, `pinnedPeerCertSha256` in Xray JSON). `http` may use `security:"tls"` with a certificate (HTTPS proxy); every other new protocol is `none`.
  - wireguard / amneziawg: empty `private_key` is generated, `public_key` is derived; `address` is the server tunnel address with prefix (IPv4, /16-/30, default random `10.x.y.1/24`), `mtu` 1280-1500 (1420), `dns` defaults to `1.1.1.1, 8.8.8.8`. Every user linked to the inbound gets a peer automatically (key pair, preshared key and the next free address of the subnet). amneziawg `awg` is generated when empty (Jc 3-8, Jmin 40, Jmax 70, random S1/S2 with S1+56≠S2, distinct H1-H4 > 4). Subnets of WG inbounds must not overlap.
  - tunnel forwards every connection on `port` to `target_address:target_port` (`tunnel_network` tcp by default). tun has no port: it creates the interface `tun_name` (default `veilon-tun`) with `gateway` addresses (default `10.77.0.1/24`) and adds `routes` (specific subnets, never a default route) so that traffic routed into it leaves through the core. TUN needs CAP_NET_ADMIN; without it the inbound is skipped and reported in the core's `warnings`.
  - mtproto: FakeTLS (`ee`) secrets are derived per user from the user's password and `faketls_domain` (default `www.cloudflare.com`).
  - masque: `pool` holds at most one IPv4 and one IPv6 prefix (default `10.14.0.1/24`, `fd14::1/64`), `mtu` 1280+, `path` defaults to `/.well-known/masque/ip/{target}/{ipproto}/`.
  - **Unique name; port unique per server.** No two inbounds may share a `remark` (compared trimmed and case-insensitive). Ports are unique per server: the servers of an inbound are the main server when it runs there (`on_main`) plus each of its `node_ids`, and two inbounds conflict only when they share a `port` and at least one server (whatever the protocol, transport or listen address; TUN has no port). So `443` on the main server and `443` on a node-only inbound are fine; two inbounds on `443` that both run on the main server, or on the same node, are not; an inbound on nodes 1 and 2 conflicts if either node already has the port. Reserved: on the main server the panel port and the internal core API ports; on a node its agent `api_port` (e.g. `62050`). Create and update answer `409` with `{"error":{"code":"conflict","message":"Port 443 is already used on node Lightsail","field":"port","inbound":"Other","port":443,"server":"node","node_id":2,"node_name":"Lightsail"}}`; for the main server `"server":"main"` (message `Port 443 is already used on the main server`, no `node_id`/`node_name`). `inbound` is the inbound holding the port, `""` for a reserved port. A name conflict is `{"error":{"code":"conflict","message":"…","field":"remark","inbound":"Main"}}`. On update a value the inbound already had is not re-checked, so inbounds stored before a rule existed stay editable: with the same `port` only servers the inbound did not run on before are checked (a changed `port`, added `node_ids` or `serve_main` turned on are checked).
- `POST /inbounds/reality-keys` → `{private_key, public_key, short_id}`
- `POST /inbounds/wireguard-keys` → `{private_key, public_key, awg:{jc,jmin,jmax,s1,s2,h1,h2,h3,h4}}`

### Traffic statistics per engine
- Xray (all its protocols, including wireguard per-peer and http/mixed per account): exact, from Xray's stats API.
- tuic: sing-box hands each authenticated user's traffic to a loopback inbound of Xray (`bridge-singbox`), so it is counted (and routed) by Xray exactly like the rest. TUIC therefore needs Xray running.
- mtproto: per user from mtg-multi's `/stats` (cumulative counters).
- amneziawg: per peer from amneziawg-go (`rx_bytes`/`tx_bytes`, reported every 10 s).
- tunnel and tun have no users; their traffic counts as server traffic only.

## Certificates (TLS)
Certificate view (every endpoint below returns this shape):
```
{ id, domain,                 // first identifier, for display
  identifiers: ["example.com"] | ["203.0.113.7", "2001:db8::7"],
  type: "domain"|"ip", source: "acme"|"upload"|"self", issuer,
  profile: ""|"shortlived",   // ACME profile ("shortlived" for IP certificates)
  not_before, not_after,      // RFC 3339
  auto_renew: bool,
  http_port,                  // ACME only: local HTTP-01 listener port (default 80)
  renew_at: time|null,        // next automatic renewal (ACME + auto_renew)
  last_error: "", last_attempt_at: time|null,
  in_use: bool }              // used by the panel or an inbound
```
- `GET /certs` → `[view]`
- `GET /certs/{id}` → view
- `GET /certs/public-ip` → `{ipv4, ipv6, candidates:[{ip, family:"ipv4"|"ipv6", sources:["api.ipify.org","interface",...]}]}`. Detection asks several external checkers over IPv4 and IPv6 and reads local interfaces; it can be wrong behind NAT, so UIs must show the result and let the admin confirm or type another address before issuing an IP certificate.
- `POST /certs/acme` `{type, domain, ip, ipv6, email, http_port, use_for_panel}` → `201` view
  - `type:"domain"` (default when `domain` is set): Let's Encrypt certificate for `domain`, valid 90 days, renewed when 30 days remain.
  - `type:"ip"`: Let's Encrypt certificate for an IP address with the `shortlived` profile (about 160 hours). `ip` is an IPv4 or IPv6 address, or empty/`"auto"` to use the detected one; `ipv6` optionally adds an IPv6 address (or `"auto"`) to the same certificate. Renewed automatically every ~3 days; failed renewals are retried hourly (domain certificates: every 6 h).
  - `email` is optional (ACME account contact). `http_port` (default 80) is the local port of the HTTP-01 listener; the CA always connects to port 80, so another value only makes sense when port 80 is forwarded to it. When the panel itself listens with plain HTTP on that port it answers the challenge.
  - `use_for_panel:true` also makes it the panel certificate (`panel_cert_id`); the panel listener restarts with HTTPS ~1 s after the response. For a domain certificate `panel_domain` is set to it; for an IP certificate `server_address` is set to the IP when empty.
  - Errors: `400 validation_error` (bad domain/IP, private IP, bad email); `502 acme_failed` with the CA's message (for example port 80 unreachable).
- `PATCH /certs/{id}` `{auto_renew}` → view (turn automatic renewal of an ACME certificate on or off)
- `POST /certs/{id}/renew` → view (renews now with the same identifiers and profile); `502 acme_failed` on CA errors
- `POST /certs/{id}/revoke` → `{ok}`: revokes an ACME certificate at the CA and deletes it. `400` for non-ACME certificates, `409 conflict` if used by the panel or an inbound.
- `POST /certs/self-signed` `{domain}` → `201` view (10-year ECDSA self-signed certificate, `source:"self"`)
- `POST /certs/upload` `{domain, cert_pem, key_pem}` → `201` view (`domain` may be empty: taken from the certificate)
- `DELETE /certs/{id}` → `{ok}`; `409 conflict` if used by the panel or an inbound

Renewed certificates are hot-reloaded: the panel and running inbounds pick them up without a restart.

## Settings
- `GET /settings`, `PATCH /settings` → `{panel_port, panel_path, panel_domain, panel_cert_id, sub_path, sub_domain, sub_title, sub_update_hours, server_address, timezone, language:"fa"|"en"}` (`timezone` is an IANA name such as `Asia/Tehran`, default `UTC`; it sets the day, week and month boundaries of resets, daily caps and time windows, and the traffic history buckets)
  - PATCH is partial. `panel_cert_id: null` (or `0`) disables HTTPS. Changing `panel_port` or `panel_cert_id` restarts the panel listener ~1 s after the response; a `panel_path` change applies immediately (the UI must navigate to the new path).
  - Client address used in links: inbound `address_override` → `server_address` → `panel_domain` → request host.
  - `sub_url` host: `sub_domain` → `panel_domain` → `server_address` → request host. The panel port is appended unless it is the scheme default or the host already includes a port (use `sub.example.com:443` behind a reverse proxy). Scheme is `https` when `panel_cert_id` is set.

## Admins (sudo only)
- `GET /admins` → `[{id, username, is_sudo, created_at}]`, `POST /admins` `{username,password,is_sudo}` → `201`, `DELETE /admins/{id}` (cannot delete yourself or the last sudo admin: `400 validation_error`; an admin who still owns users: `409 conflict`, move or delete their users first; the CLI `veilon admin delete <username>` follows the same rules)

## Audit
- `GET /audit?page=&per_page=50` → `{items:[{id, admin, action, target, ip, created_at}], total}` (newest first)

## Subscription (public, no auth)
- `GET /<sub_path>/{sub_token}` → base64 v2ray links by default; `?format=singbox|clash|v2ray|json` or detect by User-Agent.
  (`json` = array of complete Xray client configs, one per link. User-Agent: clash/mihomo/stash → clash; sing-box/SFA/SFI/SFM/SFT → singbox; anything else → v2ray. Per-format support: v2ray links — every protocol with a share-link standard (vless, vmess, trojan, ss, `hysteria2://`, `tuic://`, `wireguard://`, `http(s)://`, `socks://` for mixed and socks, `tg://proxy` for mtproto); singbox — vless/vmess/trojan/ss (not xhttp), hysteria2, tuic, http, socks, wireguard (as an endpoint); clash — the same plus amneziawg (`amnezia-wg-option`), without trojan over plain TCP; json (Xray) — everything except tuic, amneziawg and mtproto, plus masque, which only exists here. Unsupported entries are skipped per format.)
  Headers: `subscription-userinfo: upload=..; download=..; total=..; expire=<unix>`, `profile-title: base64:<b64>`, `profile-update-interval: <hours>`, `content-disposition`.
- **Browser page.** The same URL opened in a web browser shows the user's subscription page (usage, days left, expiry, last online, the subscription link with copy and QR, every config with copy and QR, "copy all", the refresh interval), dark or light, Persian (RTL, Persian calendar, panel time zone) or English. A request gets the page only when its `Accept` contains `text/html` **and** its User-Agent starts with `Mozilla/` and names no known client or tool (v2rayNG, v2rayN, v2rayTun, V2Box, Hiddify, Streisand, Shadowrocket, sing-box/SFA/SFI/SFM/SFT, clash/mihomo/Stash, Happ, NekoBox/NekoRay, Karing, FoxRay, Quantumult, Surge, Loon, curl, wget, okhttp, Go-http-client, python, HTTPie, Postman). Every other request gets the subscription exactly as before (same body and headers). The page has no `subscription-userinfo`/`profile-*` headers; it sends `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Vary: Accept, User-Agent`. Disabled, expired, limited and on-hold users see the page with their status. Without a web build (placeholder UI) browsers get the subscription.
  - `?format=raw` → the subscription even in a browser (the format then follows the User-Agent, as for any client); an explicit `?format=v2ray|clash|singbox|json` also always returns that format.
  - `?format=html` → the page whatever the headers.
  - `?format=status` → the page data as JSON (`Cache-Control: no-store`): `{username, status, up, down, used, limit, expire_at|null, on_hold_days?, last_online_at|null, online, sub_url, title, update_hours, timezone, links:[{name, protocol, network?, security?, uri?, conf_url?}]}` — the user's own usage and configs only, no admin or panel data (the same data is inlined in the page). `online` uses the admin panel's rule (see Extensions: Online), on the main server or a node.
  - `?format=live` → only the moving parts, for the open page to refresh every few seconds without rendering the configs: `{status, up, down, used, limit, expire_at|null, on_hold_days?, last_online_at|null, online}` (`Cache-Control: no-store`). Neither the page nor this JSON counts as a device for the HWID limit (like `/info`).
  - The page loads its files from `/<sub_path>/assets/…` and `/<sub_path>/favicon.svg` (the panel path never appears); nothing else of the web build is reachable there.
- `GET /<sub_path>/{sub_token}/info` → HTML-free JSON `{username, status, used, limit, expire_at, links}` (for a future user page).
- `GET /<sub_path>/{sub_token}/conf/{inbound_id}` (optionally ending in `.conf`) → the WireGuard / AmneziaWG client config of that inbound as `application/octet-stream` with `Content-Disposition: attachment; filename="NAME.conf"`, so Android browsers save it as `NAME.conf` and not `.conf.txt`. `404` when the user has no such config.

## Limits, connection log, groups and admin permissions (API preview)
Implemented in the backend (schema 5) and covered by unit tests; the web UI covers user limits, groups, admin permissions and the global blocking settings (`ad_block`, `block_torrent` in `/settings`), but none of it is verified on a production server yet. The UI hides pages and actions the signed-in admin lacks permission for, reading `permissions` from `GET /auth/me`. Everything defaults to off: with no limit set, nothing changes for a user.
- Extra User fields (`GET`/`PATCH /users/{id}`, `POST /users`; `0`/`false`/`""` = off): `ip_limit` (concurrent client IPs), `device_limit` and `require_hwid` (HWID devices, read from the `X-Hwid` subscription header), `speed_limit_down` / `speed_limit_up` (Mbps), `traffic_multiplier` (counted = real × multiplier, `1` = none), `daily_limit` / `daily_used` (bytes), `block_torrent`, `window_from` / `window_to` (`HH:MM`) and `window_days` (weekday bit mask) for an allowed time window, `group_id`, and read-only `suspend_reason`, `admin_id`.
- IP limits are enforced with nftables (`table inet veilon_guard`, timed bans on inbound ports only). Speed limits are per connection: the connections of a limited user (client IP and port from the access log) get the user's conntrack mark (`table inet veilon_shape`), download is shaped by `tc` on the default interface on that mark and upload is policed by nftables on it, so other users behind the same IP are never slowed. AmneziaWG users are limited by their peer's tunnel address: download by an HTB tree (handle `7a:`) on the inbound's `vawg<id>` interface, upload by one named nftables limit per user (`table inet veilon_awgshape`); MTProto has no speed limits. The guard runs only for users that have a limit; when `nft` or `tc` are missing it logs one warning and does nothing.
- `GET /users/{id}/connections` → `{ips:[{id, ip, country, first_seen, last_seen, count, blocked, live}], devices:[{id, hwid, os, os_version, model, user_agent, ip, country, first_seen, last_seen, count, blocked}]}`
- `POST /users/{id}/ips` `{ip, blocked}`, `DELETE /users/{id}/ips/{ip_id}`, `PATCH /users/{id}/devices/{device_id}` `{blocked}`, `DELETE /users/{id}/devices/{device_id}`
- Groups: `GET|POST /groups`, `PATCH|DELETE /groups/{id}`; Group `{id, name, color, ad_block, inbound_ids, defaults, users, created_at}`.
- Admins: `PATCH /admins/{id}` also accepts `permissions` (`users_view, users_create, users_edit, users_delete, all_users, inbounds, settings, certs, logs, groups`), `user_quota` and `traffic_quota`. Sub-admins created before permissions existed keep full access; a new sub-admin without `permissions` gets the four `users_*` flags and sees only its own users.

## Xray config (sudo only)
The panel builds the Xray config from the inbounds plus an admin-editable template (log, DNS, outbounds, routing rules, balancers, observatory). Inbounds keep the tags `inbound-<id>`; the outbound tags `direct` and `blocked` are reserved.
- `GET /xray/template` → `{template, custom, inbounds:[{id, tag, remark, protocol}], reserved:["direct","blocked"]}`. `custom:false` means the built-in default is in use.
- Template: `{log:{loglevel, dnsLog}, dns:{...}, outbounds:[{...}], routing:{domainStrategy, rules:[{...}], balancers:[{...}]}, observatory:{...}, burstObservatory:{...}, block_private}` (Xray JSON objects as Xray documents them; `block_private` adds the private-IP block rule).
- `POST /xray/template/test` `{template}` → `TemplateResult` without applying: the full config is generated and checked with `xray run -test`.
- `PUT /xray/template` `{template}` → `{result:TemplateResult, template}`. The template is tested first; only a passing template is saved and Xray restarted. If Xray does not stay up, the previous template is restored (`rolled_back:true`). `409 busy` while another change runs. Audit-logged as `xray.config`.
- `TemplateResult` = `{ok, tested, applied, rolled_back, error?, warnings:[string]}`.
- `POST /xray/outbounds/parse` `{link, tag}` → `{outbound}`: turns a share link (`vless://`, `vmess://`, `trojan://`, `ss://`, ...) into an outbound object for the template. `400 validation_error` for a link it cannot read.

## Hosts (permission `inbounds`)
Hosts change how an inbound appears in subscription links (address, port, SNI and more) without touching the server side. An inbound with enabled hosts gives one link per host, in order; without hosts it gives its normal link.
- `GET /inbounds/{id}/hosts` → `[Host]`
- `PUT /inbounds/{id}/hosts` `{hosts:[HostInput]}` → `[Host]` (replaces the whole list; order is kept). Audit-logged as `inbound.hosts`.
- Host: `{id, inbound_id, remark, address, port, sni, host, path, fingerprint, alpn, enabled, sort}`. HostInput is the same without `id`, `inbound_id` and `sort`; `enabled` defaults to `true`. Empty fields (and `port: 0`) keep the inbound's own value. `address` and `sni` must be valid host names or IPs, `fingerprint` one of the uTLS names.

## In-panel updates (sudo only)
A background check reads the GitHub releases of this project one minute after the panel starts and then every 6 hours (stable releases only; builds without a release version do not check). There is no check-now endpoint by design.
- `GET /system/update` → `{current, available:{tag, name, notes, url, published_at}|null, can_update, checked_at, next_check_at, status:UpdateStatus|null}`. `checked_at` is the last successful check (automatic or manual); `next_check_at` is the next automatic check (1 minute after start, then every 6 hours; `null` when automatic checks are off, e.g. in a development build). `can_update:false` when the root updater unit (`veilon-updater.path`) is not installed; run `sudo veilon update` once on the server.
- `POST /system/update` `{tag}` → `202 {ok, status}`. The root updater downloads the binary, checks it against `checksums.txt`, backs up the binary and the database to `/root/backups`, installs, restarts and waits for `/health`; if the new version does not come up healthy, the old binary is restored. `409 busy` while an update runs, `400 validation_error` for a tag that is not the available release. Audit-logged as `panel.update`.
- `POST /system/update/check` → `200` with the same shape as `GET /system/update` plus `cached:bool`. Asks the release server right away (20 s timeout). At most one real request per 30 seconds: a check inside that window returns the last outcome with `cached:true`. The automatic 6-hour schedule is not moved; a scheduled check that falls inside the window reuses the manual answer. Errors (the last cached answer is kept): `502 update_check_failed` (release server unreachable), `503 rate_limited` (GitHub rate limit), `504 timeout`; the body is `{"error":{code, message, detail}, "retry_after":<seconds>}` (`retry_after` is `0` when a new attempt can run now). Audit-logged as `panel.update_check` (`ok` or `failed`; cached answers are not logged).
- UpdateStatus: `{tag, from, phase, running, message?, error?, backup_dir?, started_at, updated_at, finished_at?}`; `phase` is `queued`, `download`, `verify`, `backup`, `install`, `restart`, `health`, then `done`, `rolled_back` or `failed`. The status survives the restart; poll `GET /system/update` (and `GET /health` while the panel restarts).

## Nodes (sudo only)
A node is another server running `veilon node`. The panel pushes the Xray config of the inbounds placed on it (inbound `node_ids`, see Inbounds) over mutual TLS and collects its traffic and the client IPs it sees; each such inbound gives one subscription link per node. Default agent port `62050`. Do not install the panel itself on a node server.
- `GET /nodes` → `[Node]`
- `POST /nodes` `{name, address, api_port, enabled, inbound_ids?}` → `201 {node:Node, install:Install}`. `inbound_ids` is **deprecated**: it still assigns inbounds but leaves their `serve_main` as it is (so the main server keeps running them); place inbounds with `node_ids` on the inbound instead. `409 conflict` for a duplicate name, or when the assigned inbounds would share a port on the node or take its `api_port` (only newly assigned inbounds, or all of them when `api_port` changes, are checked), `400 validation_error` for a bad address or port.
- `PATCH /nodes/{id}` (partial, same fields) → `Node`; `DELETE /nodes/{id}` → `204`. An inbound that runs only on nodes goes back to the main server when its last node is deleted, or drops it through `inbound_ids`; when its port is taken on the main server (another inbound running there, another inbound moving with it, or a reserved port) the delete or PATCH is refused before anything changes: `409 {"error":{"code":"main_port_conflict","message":"Inbound \"B\" would move back to the main server, but port 443 is already used there by inbound \"A\"; …","field":"port","port":443,"server":"main","inbound":"A","moving_inbound":"B","moving_inbound_id":7}}` (`inbound` is `""` for a reserved port).
- `POST /nodes/{id}/install` → `Install`: issues a new client certificate and a fresh one-time install command (the old certificate stops working).
- `POST /nodes/sync` → `202`: runs a sync round now.
- `POST /nodes/{id}/update` → `202 Node` (with `state.update`): asks the node to update its agent to the main server's version. The main server reads the release with its own tag from the panel's release source (GitHub, or `VEILON_UPDATE_API`), picks `veilon-linux-<arch>` for the node's architecture and sends the URL and the SHA-256 from `checksums.txt` over the agent channel. The node downloads, verifies, backs up its binary, swaps it, restarts `veilon-node` (through a `systemd-run` helper) and restores the previous binary if the new version does not reconnect within 90 s. Errors (`{"error":{code, message}}`): `409 agent_unsupported` (agent older than v0.3.5: run `POST /nodes/{id}/install` once), `409 arch_unknown`, `409 not_release` (the main server or the node does not run a release version), `409 up_to_date`, `409 downgrade` (node newer than the main server), `409 in_progress` (one update per node at a time), `409 offline`, `429 rate_limited` (30 s between requests per node), `502 release_unavailable` (release, asset or checksum missing), `502 node_refused`; `404` for an unknown node. Audit-logged as `node.update.requested`, then `node.update.ok` or `node.update.failed` (with the reason).
- `POST /nodes/update-outdated` → `200 {started, results:[{id, name, ok, code?, message?}]}`: the same for every node whose last seen agent version is older than the main server (nodes on the current version are skipped; refused ones carry the error code above).
- Install: `{token, command, port}`; `command` is the installer one-liner with `node --token <token>` to run as root on the node server.
- Node: `{id, name, address, api_port, enabled, up, down, last_seen_at, created_at, inbound_ids, state, update_to}`; `update_to` is the main server's version when the node's agent is older (`""` otherwise); state: `{status, error?, checked_at, version, xray_version, xray_running, synced, skipped:[string], started_at, load1, mem_used, cpus, latency_ms, arch, can_update, update, caps:[string], needs_update:[{id, remark, protocol}]|null}`; `caps` are the capabilities the agent announced (`update`, `online`, and from v0.3.8 `amneziawg` and `mtproto`, which run AmneziaWG / MTProto inbounds on the node); `needs_update` lists the node's AmneziaWG / MTProto inbounds its agent is too old to run (skipped until the agent is updated); `arch` is the node's CPU architecture and `can_update` says whether its agent updates itself (both empty/false for agents older than v0.3.5); `update` is the last agent update asked from this panel since it started, `null` when none: `{target, from, phase, error?, rolled_back?, started_at, updated_at, finished_at?, by}` with `phase` `downloading`, `verifying`, `restarting`, then `done` or `failed` (`rolled_back:true` when the previous version was restored) with `status` one of `online`, `degraded` (reachable, but Xray is down or rejected the config), `offline`, `waiting` (never reached; agent not installed yet) and `disabled`. `skipped` lists inbounds the node cannot run. `inbound_ids` (read) lists the inbounds placed on the node. `up`/`down` are the users' counted usage the node carried, all time (multipliers applied, counted like `servers`; reset once by the schema 9 upgrade, when they stopped being raw inbound counters); its share of a window is in `GET /system/traffic` → `servers`.
- Users' usage is the sum over the main server and all nodes; each node batch is counted exactly once (the batch id commits with the counters). A user's `ip_limit` counts the union of IPs on all servers, and the newest IPs over the limit are blocked for 3 minutes on the main server and on every node; speed limits are shaped on every server for its own connections. Both need node agents of v0.3.2 or newer; older nodes keep serving but neither report IPs nor apply bans and speed limits.
- Audit actions: `node.create`, `node.update` (an edit of the node), `node.delete`, `node.install`, `node.update.requested`, `node.update.ok`, `node.update.failed` (agent updates).

## Extensions (implemented beyond the original contract)
- `GET /health` (no auth) → `{status:"ok", version, schema}`.
- Every timestamp is RFC3339 UTC and may carry fractional seconds (e.g. `2026-11-06T16:19:17.594Z`).
- Error codes in use: `bad_request`, `validation_error`, `unauthorized`, `invalid_credentials`, `forbidden`, `not_found`, `conflict`, `rate_limited`, `acme_failed`, `internal`. Unknown endpoints/methods return `404 not_found` as JSON.
- Non-sudo admins can use the endpoints their permissions allow (see above), never `/admins`.
- Online = at least one open connection on the main server's Xray or a node's (Xray's online list, `statsUserOnline`, read every 5 s; Xray 26.3 or newer, node agent v0.3.6 or newer), with about 8 s of grace so one missed read does not flicker. Engines without a connection list (AmneziaWG, MTProto, older Xray, node agents up to v0.3.5) and users reaching Xray through a loopback hop (TUIC via sing-box) count as online while their client sent traffic in the last 60 s. `last_online_at` is the moment the user was last seen online; it is stored when the user comes online or goes offline and at most every 60 s while online (the API shows the newer in-memory value).
- The UI is served from `/<panel_path>/` with SPA fallback to `index.html`. The server injects into `<head>`: `<base href="/<panel_path>/">` and `window.__VEILON__ = {basePath:"/<panel_path>/", apiBase:"/<panel_path>/api/v1"}`. Build the UI with relative asset URLs (Vite `base: './'`).
