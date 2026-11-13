# Advanced inbound settings (Xray security and transport)

Every Xray inbound form has an **Advanced settings** block on the **Connection** step and on the **Security** step. It is closed by default and shows a count when something is set. Left untouched, the inbound gets exactly the config older panels wrote, so existing inbounds and links do not change. Only the sections that apply to the chosen protocol, transport and security are shown; anything that does not apply is dropped on save.

## Connection step

- **Sniffing details** (when sniffing is on): what to sniff (`http`, `tls`, `quic`, `fakedns`), **routing only** (the sniffed domain is used by routing rules, the target is not rewritten), **metadata only** (with `fakedns`), and **excluded domains**.
- **Socket and PROXY protocol** (VLESS, VMess, Trojan, Shadowsocks): accept PROXY protocol (when HAProxy or Nginx with `proxy_protocol` sits in front; direct connections stop working), TCP Fast Open, TCP congestion (`bbr`, `cubic`, `reno`), keepalive, `SO_MARK`, IPv6 only.
- **HTTP header obfuscation** on RAW (TCP) without TLS or Reality: hosts and paths.
- **WebSocket / HTTPUpgrade:** custom headers sent by clients; WebSocket heartbeat.
- **gRPC:** client User-Agent, idle and health check timeouts, initial window size, ping without streams.
- **XHTTP:** padding (`xPaddingBytes`), max size of each POST, max buffered POSTs, server stream-up seconds, no SSE header, **xmux** (client connection reuse; it goes into client outputs only; set `maxConcurrency` or `maxConnections`, not both) and **download through another address**, for example uploads straight to the server and downloads through a CDN (not with mode `stream-one`).
- **mKCP** (new transport `kcp`, UDP): packet header (`srtp`, `utp`, `wechat-video`, `dtls`, `wireguard`, `dns` with a domain), seed, MTU, TTI, uplink and downlink capacity, `cwndMultiplier`, `maxSendingWindow`. Reality is not available on mKCP. Xray 26 removed mKCP's buffer and congestion switches, so they are not offered; the header and seed are written as Xray `finalmask` (`mkcp-legacy`).
- **Shadowsocks:** UDP on or off on RAW, and Shadowsocks now also runs over WebSocket, gRPC, HTTPUpgrade, XHTTP and mKCP, with TLS.
- **Hysteria 2:** congestion control (`auto`, `bbr` which ignores the bandwidth a client declares, `brutal` and `force-brutal` with server speeds), UDP idle timeout, and **masquerade** (what a visitor without the password sees on the HTTP/3 port: a 404, another site through a reverse proxy, or fixed text). A masquerade that serves files from a folder is not offered.
- **TUIC:** 0-RTT handshake, heartbeat, UDP relay mode for clients.
- **Server-side masks:** TCP fragment (`tlshello` or a packet range, size, delay) and UDP noise (mKCP and Hysteria). Clients need nothing extra.
- **VLESS reverse proxy (bridge):** another server (the bridge, for example abroad) connects to this inbound and users of this inbound, and of other inbounds you pick, leave the internet through it. After saving, **Download bridge config** gives the bridge's Xray config: install Xray on the bridge and run `xray run -c veilon-bridge.json`. While the bridge is not connected those users have no internet; traffic never leaves this server directly.

## Security step

- **Fallbacks** (VLESS and Trojan on RAW with TLS or Reality, not with VLESS Encryption): connections that are not users (a browser, a probe) go to a port, `host:port` or socket path, matched by path, SNI and ALPN, with optional PROXY protocol `xver`. If the site behind a fallback speaks HTTP/1.1 only, set the inbound ALPN to `http/1.1`.
- **TLS:** minimum and maximum version, cipher suites, reject unknown SNI, **pin the certificate in client configs** (clients accept only this certificate; after a renewal they must update the subscription), and **ECH** (Encrypted Client Hello, hides the real SNI). Switching ECH on makes keys; set the public (outer) name and press **Generate keys** to remake them with that name. Clients get the ECH config list in links and JSON; old Xray clients and some apps have no ECH.
- **Reality:** **ML-DSA-65** (the server also signs its Reality certificate post-quantum and clients check it with `pqv`). Switching it on makes keys. The Reality target must send a large certificate chain (about 3,500 bytes or more, for example `dl.google.com`); targets with small certificates such as `www.speedtest.net` or `github.com` do not connect. Also minimum and maximum Xray client version, max clock difference, and upload and download rate limits for connections passed to the Reality target (never users).
- Reality client fingerprints are limited to `chrome`, `firefox` and `safari`; other fingerprints did not connect in tests with Xray 26.10.31.

## Clients

Links, Clash (mihomo), sing-box and the Xray JSON subscription carry what each client supports (`pqv`, `ech`, `pcs`, mKCP header and seed, XHTTP extras, TUIC options); the rest is server-side only or left out as before.

## Nodes

Advanced settings run on nodes too, through the normal config push.

## Xray-core versions

Some settings need a recent Xray-core on every server that runs the inbound (the main server and each chosen node):

| Setting | Xray-core |
| --- | --- |
| WebSocket heartbeat | 24.11.30 |
| XHTTP stream-up seconds | 25.1.30 |
| Reality fallback rate limit | 25.6.8 |
| Reality ML-DSA-65, TLS ECH | 25.7.26 |
| VLESS Encryption | 25.9.5 |
| VLESS reverse proxy | 25.9.11 |
| Hysteria 2, fragment / noise masks | 26.3.27 |
| mKCP | 26.6.1 |

- Saving is refused when a server is known to run an older Xray-core than a setting you add needs (the error names the server and the version). Settings the inbound already had are not checked again on edit, so an inbound can still be renamed, changed or moved off an old node.
- When a server turns out to be too old (a node reached later, or its Xray downgraded), only that inbound is left out on that server; the server's other inbounds keep working. It comes back on the next sync after Xray is updated.
- The inbound form warns before saving, the inbound card shows **old Xray**, the node card and node sheet list the left-out inbounds and what they need, and the Telegram bot shows the same on the inbound and node screens. The API reports it as `xray_needs` / `xray_too_old` on inbounds and `state.xray_too_old` on nodes.
- An unknown version (a node never reached, Xray not started yet) is let through.

## Telegram bot

Each inbound screen has **⚙️ Advanced settings**: a list of what is in use, sniffing for routing only, PROXY protocol, TCP Fast Open, reject unknown SNI, certificate pin, ECH and ML-DSA-65 on or off (the server makes the keys), Shadowsocks UDP, the reverse proxy on or off and the bridge config as a file. Other settings are in the web panel or the full JSON edit.

## API

The `advanced` object on inbounds, `POST /inbounds/mldsa65-keys`, `POST /inbounds/ech-keys` and `GET /inbounds/{id}/reverse-bridge`. [api-v1](../api-v1.md)
