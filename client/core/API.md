# veiloncore — gomobile API

Package `veiloncore` (module `github.com/veilonhq/veilon-client/core`, import path
`github.com/veilonhq/veilon-client/core/veiloncore`). Bound with
`gomobile bind -target=android` → Java/Kotlin class `veiloncore.Veiloncore`
(package-level funcs become static methods: `Veiloncore.parseText(...)`).

**Convention:** every structured value crosses the boundary as a **JSON string**.
Structs below are documented for their JSON shape; Kotlin should not use the
generated struct classes (fields with slices/pointers/maps are not bindable and
gomobile skips them). Go `int32`/`int64` → Kotlin `Int`/`Long`; `error` → thrown
`Exception`; `bool` → `Boolean`.

## Constants

| Name | Value / purpose |
|---|---|
| `Version` | `"1.0.0"` — core library version. |
| `DefaultResolvers` | Newline-separated built-in DNS resolver list (public + common Iranian). |
| `TypeVLESS…TypeSlipstream` | Profile `type` strings (see Profile types below). |
| `CoreSingbox`, `CoreXray`, `CoreAWG`, `CoreNone` | Engine names returned by `CoreFor`: `"sing-box"`, `"xray"`, `"amneziawg"`, `"dns-tunnel"`, `"ssh"`, `"none"`. |

## Package functions (bindable)

| Signature | Purpose |
|---|---|
| `CoreVersions() string` | JSON `{"veilon","singbox","xray"}` versions of bundled cores. |
| `ParseText(text string) string` | Parse pasted text / QR payload / subscription body → `ParseResult` JSON. |
| `ParseConf(text, name string) string` | Parse a WireGuard/AmneziaWG `.conf` → `Profile` JSON or `{"error":"..."}`. |
| `FetchSubscription(url, deviceJSON, proxyAddr string, timeoutMs int32) string` | Download + parse a subscription; `deviceJSON` = `Device`, `proxyAddr` = `"127.0.0.1:port"` or `""`; timeout ≤0 → 15000. Returns `SubResult` JSON. |
| `ParseUserInfoHeader(h string) string` | Parse a `subscription-userinfo` header → `UserInfo` JSON. |
| `ParseProfileLink(link string) string` | Parse one share link → `Profile` JSON or `{"error":"..."}`. |
| `NormalizeProfile(profileJSON string) string` | Profile **edit round-trip**: decode an edited `Profile` JSON, normalize it (type case, defaults, missing name/id) → canonical `Profile` JSON or `{"error":"..."}`. Unknown keys dropped. `id` kept when present; send `"id":""` to recompute it. Output is accepted by every function taking `profileJSON`. |
| `ProfileToLink(profileJSON string) string` | Re-export an (edited) profile → `{"link":"..."}` or `{"error":"..."}`. See "Share-link export" below. |
| `CoreFor(profileJSON string) string` | Engine that will carry the profile (`Core*` value), `""` on bad JSON. |
| `CheckProfile(profileJSON, settingsJSON string) string` | Validate the generated config; `""` = OK, otherwise the error text. |
| `BuildConfig(profileJSON, settingsJSON string) string` | Generated sing-box config (pretty JSON, sidecar ports are placeholders) or `{"error":...}`. Debug/export. |
| `Start(profileJSON, settingsJSON string, platform Platform) error` | Connect with a profile; closes any previous session. Throws on failure. |
| `Stop() error` | Disconnect. |
| `IsRunning() bool` | Whether a session is active. |
| `NetworkChanged()` | Call when the device switched networks; resets connections. |
| `URLTest(profileJSON, settingsJSON, url string, timeoutMs int32) int32` | Real delay in ms via an isolated instance; `-1` on failure. url `""` → gstatic generate_204; timeout ≤0 → 8000. |
| `URLTestError(profileJSON, settingsJSON, url string, timeoutMs int32) string` | Same as URLTest, returns `{"delay":n,"error":"..."}`. |
| `GetLogs(after int64) string` | JSON array of `LogLine` with `seq > after`. |
| `ClearLogs()` | Empty the log ring buffer. |
| `WriteLog(level, msg string)` | Add an app-side line to the log buffer. |
| `DefaultResolverList() string` | Returns `DefaultResolvers`. |

Kotlin names (gomobile lower-cases the first letter): `Veiloncore.parseProfileLink(String): String`,
`Veiloncore.normalizeProfile(String): String`, `Veiloncore.profileToLink(String): String`,

`Veiloncore.defaultResolverList(): String`.

## Callback interfaces (implement in Kotlin)

### `Platform` — implemented by the VpnService
| Method | Purpose |
|---|---|
| `OpenTun(optionsJSON String): Int` (`OpenTun(string) (int32, error)`) | Establish the VPN interface described by `TunRequest` JSON; return its fd (throw on failure). |
| `Protect(fd Int): Boolean` | `VpnService.protect(fd)` — exclude a socket from the VPN. |
| `OnLog(level Int, message String)` | Receive core log lines. |


## Exported Go-only helpers (not for Kotlin — return structs/pointers)
These are exported for tests/internal use; gomobile either skips them or binds
them awkwardly. Use the string wrappers above.
`ParseContent(body) *ParseResult`, `ParseWGConf(text, name) (*Profile, error)`,
`ParseLink(link) (*Profile, error)`,
`ParseUserInfo(h) UserInfo`, `DefaultSettings() Settings`, `CoreOf(p *Profile) Core`,
`ToLink(p *Profile) (string, error)` (use `ProfileToLink`),
type `Core` (string).

## JSON shapes

- **TunRequest** (arg of `OpenTun`): `mtu` int, `inet4Address` [cidr], `inet6Address` [cidr], `inet4Routes` [cidr], `inet6Routes` [cidr], `dnsServer` string, `includePackages` [pkg], `excludePackages` [pkg].
- **Settings** (`settingsJSON`): `mtu`, `remoteDns` (`tcp://`, `https://…/dns-query`, `tls://`, `udp://` or IP; via proxy), `directDns`, `disableQuic`, `bypassIran`, `bypassLan`, `blockAds`, `ipv6`, `proxyRules`/`directRules`/`blockRules` ([domain | `domain:` | `full:` | `keyword:` | `regexp:` | CIDR]), `perAppMode` (`off`/`include`/`exclude`), `perAppPackages`, `appPackage` (own package, always excluded), `logLevel`, `mixedPort` (0=off; 127.0.0.1 SOCKS5+HTTP), `assetDir` (dir with `geoip-ir.srs`, `geosite-ir.srs`, `geosite-category-ads-all.srs`), `stack` (`mixed`/`system`/`gvisor`), `tlsFragment`. Missing fields take `DefaultSettings()` (mtu 9000, remoteDns `https://1.1.1.1/dns-query`, directDns `8.8.8.8`, …).
- **Device** (`deviceJSON`): `hwid`, `os`, `osVersion`, `model`, `appVersion`.
- **UserInfo**: `upload`, `download`, `total`, `expire` (unix s), `present` bool.
- **ParseResult**: `profiles` [Profile], `errors` [string].
- **SubResult**: `profiles`, `errors`, `userInfo`, `title`, `updateHours`, `refused` (non-empty = panel refused this device), `supportUrl`, `webPageUrl`, `announce`, `fetchedAt` (unix), `status` (HTTP code).
- **LogLine**: `seq` int64, `time` (unix ms), `level`, `msg`.
- **KCPConfig**: `mtu`, `tti`, `uplinkCapacity`, `downlinkCapacity`, `cwndMultiplier`, `maxSendingWindow` (ints).
- **Profile** (stored by the app, passed back to Start): `id`, `name`, `type`, `server`, `port`, `uuid`, `password`, `username`, `method`, `flow`, `encryption`, `vmessSecurity`, `alterId`, `network` (tcp/ws/grpc/httpupgrade/xhttp/kcp/xdrive/h2/quic; links' `raw`→`tcp`, `mkcp`→`kcp`, `splithttp`→`xhttp`), `path`, `host`, `serviceName`, `mode`, `headerType`, `xhttpExtra`, `xPaddingBytes`, `authority`, `kcp` (KCPConfig), `seed`, `xdrive`, `finalMask`, `sockopt`, `mux`, `security` (none/tls/reality), `sni`, `alpn`[], `fp`, `insecure`, `pinSHA256`, `pinSPKI`, `ech`, `vcn`, `cipherSuites`, `pqv`, `pbk`, `sid`, `spx`, `obfsPassword`, `upMbps`, `downMbps`, `congestion`, `udpRelayMode`, `ports`, `plugin`, `pluginOpts`, `wg` (WGConfig), `secret` (MTProto — app opens `tg://` instead of connecting), `masquePath`, `dns` (DNSTunnelConfig), `ssh` (SSHConfig), `addressOverride`, `domainOverride`, `link`, `subId`.
  - **`addressOverride`** (string, optional, all types): replaces the address the client connects to at connect/test/config time; the stored link and `id` are unchanged. Format `host`, `host:port`, `[v6]` or `[v6]:port` (a port also replaces `port`). Proxy/VPN types (vless, vmess, trojan, shadowsocks, hysteria2, tuic, wireguard, amneziawg, http, socks, naive, masque, ssh incl. StunTLS): replaces `server` (and `ssh.host` for direct SSH/StunTLS). When the original server was a hostname, it is kept as TLS `sni` (TLS/Reality/hy2/tuic/naive/masque) and as the `host` header (ws/httpupgrade/xhttp/h2), and as `ssh.tlsSni`/`ssh.wsHost` for StunTLS — **only if those are empty**, so user-set SNI/Host always win. DNS tunnels (dnstt, noizdns, vaydns, slipstream): replaces `dns.resolvers` with the single `host:port` (port default 53; a `doh` transport falls back to `udp`). The SSH hop behind a DNS tunnel or NaiveProxy is never changed. Use it e.g. for SlipGate exports that carry the server's private IP (direct SSH/SOCKS/StunTLS).
  - **WGConfig**: `privateKey`, `peerPublicKey`, `presharedKey`, `address`[], `mtu`, `dns`[], `keepalive`, `reserved`[int], `allowedIPs`[], `awg` (AWG: `jc`,`jmin`,`jmax`,`s1`–`s4` int; `h1`–`h4`, `i1`–`i5` string).

### Transport / profile extension fields (Xray-era)
All optional, all strings unless noted; omitted when empty. "Raw JSON" fields hold
a **JSON document serialized as a string** (the app edits/validates it as text,
the core `json.Unmarshal`s it and passes it to Xray unchanged). Any of
`finalMask`, `sockopt`, `mux`, `xdrive`, `pqv`, `vcn`, network `kcp`/`xhttp`/`xdrive`,
or VLESS `encryption` ≠ `none` makes `CoreFor` return `xray` (sing-box has no
equivalent); shadowsocks with any transport/TLS/http header also goes to Xray.
`ech` and `cipherSuites` alone do not switch the core (sing-box ignores them).

| field | link key | shape / example | Xray mapping |
|---|---|---|---|
| `kcp` | `mtu`, `tti` (vless/trojan; vmess JSON `mtu`,`tti`) | object `{"mtu":1350,"tti":20,"uplinkCapacity":12,"downlinkCapacity":100,"cwndMultiplier":2,"maxSendingWindow":2097152}` (ints; 0/absent = Xray default) | `streamSettings.kcpSettings` (network `kcp`) |
| `seed` | `seed` (vmess: `path` when `net=kcp`) | `"s3cr3t"` | mKCP legacy obfuscation → finalmask UDP mask `{"type":"mkcp-legacy","settings":{"value":"<seed>"}}` when there is no `headerType` |
| `headerType` (kcp) | `headerType` (vmess `type`) | `none`/`srtp`/`utp`/`wechat-video`/`dtls`/`wireguard`/`dns` | finalmask UDP `{"type":"mkcp-legacy","settings":{"header":"wechat"}}` (`wechat-video`→`wechat`; `dns` uses `host` as `value`). A user `finalMask` that already holds an `mkcp-legacy` UDP mask wins (no duplicate). |
| `authority` | `authority` | `"auth.example.com"` | `grpcSettings.authority` (gRPC `:authority`) |
| `xPaddingBytes` | `x_padding_bytes` | `"100-1000"` | `xhttpSettings.xPaddingBytes` |
| `xhttpExtra` | `extra` | raw JSON `{"mode":"packet-up","scMaxEachPostBytes":"500000","headers":{"X-A":"1"},"noGRPCHeader":true}` | `xhttpSettings.extra` |
| `xdrive` | — (JSON subscription / manual edit only; not exportable) | raw JSON `{"service":"local","remoteFolder":"/x"}` (service, remoteFolder, secrets, template, s3, tuning) | `streamSettings.xdriveSettings` (network `xdrive`). VLESS only, with VLESS `encryption`; outbound `mux` forced `enabled:true` (default concurrency 8, user `mux` merged). |
| `finalMask` | `fm` | raw JSON `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}],"udp":[{"type":"mkcp-legacy","settings":{"header":"srtp"}}],"quicParams":{}}` | `streamSettings.finalmask` ("TCP/UDP masks" in some panels). Bad JSON → error; unknown mask type → `CheckProfile` error. |
| `sockopt` | — | raw JSON `{"tcpFastOpen":true,"tcpCongestion":"bbr","domainStrategy":"UseIPv4","mark":255,"interface":"wlan0"}` | `streamSettings.sockopt` |
| `mux` | — | raw JSON `{"enabled":true,"concurrency":8,"xudpConcurrency":16,"xudpProxyUDP443":"reject"}` | outbound `mux` |
| `ech` | `ech` | ECHConfigList (base64) or a DNS query spec, as Xray's `echConfigList` accepts | `tlsSettings.echConfigList` |
| `vcn` | `vcn` | `"e.example.com"` | `tlsSettings.verifyPeerCertByName` |
| `cipherSuites` | `cs` | `"TLS_AES_128_GCM_SHA256:TLS_CHACHA20_POLY1305_SHA256"` | `tlsSettings.cipherSuites` |
| `pqv` | `pqv` | base64 ML-DSA-65 public key | `realitySettings.mldsa65Verify` |
| `addressOverride` | — (local, never exported) | `"203.0.113.7"`, `"host:8443"`, `"[2001:db8::1]:443"` | replaces the dialed address (see Profile above) |

Example (JSON-subscription XHTTP profile as stored):
```json
{"id":"…","name":"xhttp-json","type":"vless","server":"198.51.100.22","port":443,
 "uuid":"…","encryption":"none","network":"xhttp","path":"/xh","host":"x.example.com",
 "mode":"stream-one","xPaddingBytes":"200-800","xhttpExtra":"{\"noGRPCHeader\":true}",
 "finalMask":"{\"tcp\":[{\"type\":\"fragment\",\"settings\":{\"packets\":\"tlshello\",\"length\":\"50-100\",\"delay\":\"5-10\"}}]}",
 "sockopt":"{\"mark\":255,\"tcpCongestion\":\"bbr\"}",
 "security":"tls","sni":"x.example.com","alpn":["h2"],"fp":"chrome"}
```
mKCP example: `{"type":"vmess","network":"kcp","kcp":{"mtu":1200,"tti":30},"headerType":"dtls","seed":"seed1",…}`.

### Cleartext VLESS/Trojan and Xray (`err.xrayCleartextPublicIP`)
Xray refuses to build VLESS (`encryption` none) or Trojan **without TLS/REALITY**
unless the server is a private/loopback/link-local IP or a private name
(`*.lan`, `*.local`, `*.localdomain`, `*.test`, `*.example`, `*.invalid`,
`*.internal`, `*.home.arpa`, `localhost`, dotless names). Public IPs **and
public domain names** are refused. The core therefore:
- routes such a profile to **sing-box** whenever it needs no Xray-only feature
  (tcp incl. http header, ws, grpc, httpupgrade, h2 …): `CoreFor` = `sing-box`;
- when it does need Xray (kcp, xhttp, xdrive, `finalMask`, `sockopt`, `mux`, `vcn`, `pqv`),
  `CheckProfile` / `URLTestError` return, and `Start` throws, exactly
  **`err.xrayCleartextPublicIP`** (no Xray error text, no panic). The check uses
  the effective address after `addressOverride`.
- App string to add (`lib/l10n/strings.dart`; `home_page` already translates `err.*`):
  - en: `This server uses an Xray-only transport without TLS to a public address, which Xray does not allow. Enable TLS/REALITY or use a private address.`
  - fa: `این سرور از ترنسپورت مخصوص Xray بدون TLS روی یک آدرس عمومی استفاده می کند که Xray اجازه نمی دهد. TLS/REALITY را فعال کنید یا از آدرس خصوصی استفاده کنید.`

### Editing profiles
The app edits the `Profile` JSON directly (any field above), then calls
`NormalizeProfile` to validate/canonicalize it, stores the result, and passes it
to `Start` / `URLTest` / `CheckProfile` / `BuildConfig`. `ProfileToLink` turns
the edited profile back into a share link. There is no separate "apply edit"
call; the JSON is the stable model (fields are only ever added, never renamed).

### Share-link export (`ProfileToLink`)
| type | link |
|---|---|
| vless, trojan | `vless://` / `trojan://` (std query: type, path, host, serviceName, mode, headerType, extra, x_padding_bytes, authority, seed, mtu, tti, fm, ech, vcn, cs, pqv, security, sni, alpn, fp, allowInsecure, pcs, pbk, sid, spx, flow, encryption; other `kcp` tuning, `sockopt` and `mux` are not carried by links; `network:"xdrive"` → `{"error"}`) |
| vmess | `vmess://` base64 v2rayN JSON |
| shadowsocks | SIP002 `ss://base64url(method:password)@host:port/?plugin=…` |
| hysteria2 | `hysteria2://` (port-hopping range in the authority) |
| tuic | `tuic://uuid:password@…` |
| wireguard / amneziawg | `wireguard://privkey@host:port?publickey=…&address=…` (+ `jc…i5` for AWG) |
| http | `http://` / `https://` |
| mtproto | `tg://proxy?server=…&port=…&secret=…` |
| masque | not exportable → `{"error"}` |

`addressOverride`, `id`, `subId` are local and never exported.
Every exported link re-imports (`ParseProfileLink`) to an equivalent profile.

### Profile types (`type`)
`vless`, `vmess`, `trojan`, `shadowsocks`, `hysteria2`, `tuic`, `wireguard`, `amneziawg`, `http`, `socks`, `naive` (import/share only: `Start`/`URLTest`/`CheckProfile` return "NaiveProxy is not supported in this build" unless built with `with_naive_outbound`), `mtproto`, `masque`, `ssh`, `dnstt`, `noizdns`, `vaydns`, `slipstream` (import only: connect returns "Slipstream is not supported in this build").


## 1.0.4: removed protocols and options

- Profile types `ssh` (incl. StunTLS), `dnstt`, `noizdns`, `vaydns`, `slipstream`, `soar`, `socks` and `naive` were removed (with `dns` / `ssh` / `domainOverride` profile keys, the DNS-tunnel, SSH-transport and resolver-scanner code, `StartScan` / `StopScan` / `DefaultResolverList` / `ScanListener`, and the dnstt / noizdns / vaydns modules).
- `ParseLink` / `ParseText`: `slipnet://`, `slipgate://` and `ssh://` links fail with `err.slipnetRemoved`; `socks://`, `socks5://`, `socks5h://` and `naive+…://` links with `err.typeRemoved` (in `ParseResult.errors` as `"<link>: <key>"`). Clash / sing-box / Xray JSON entries of those types are skipped.
- A stored profile of a removed type: `CheckProfile` / `Start` / `URLTest` return `err.typeRemoved`; `CoreFor` returns `""`.
- Settings `stack` / `experimentalStack` are accepted but ignored: the tun always uses gVisor.
