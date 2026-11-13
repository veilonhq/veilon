# ShadowTLS, AnyTLS, NaiveProxy and Hysteria 2 port hopping

Three TLS-camouflage protocols served by sing-box, and port hopping for Hysteria 2 inbounds served by Xray.

| | Engine | Where it runs | Certificate |
|---|---|---|---|
| ShadowTLS (v3) | sing-box | main server only | none (borrows a real site's handshake) |
| AnyTLS | sing-box | main server only | panel certificate or an automatic self-signed one |
| NaiveProxy | sing-box | main server only | **a certificate from a real CA** |
| Hysteria 2 port hopping | Xray + nftables | main server and nodes | (Hysteria 2's own) |

sing-box hands every authenticated connection to Xray's loopback bridge with the user's identity, so traffic is counted per user, routing rules apply and IP limits and blocked IPs work like on the Xray protocols. These inbounds therefore need Xray running. They cannot be placed on nodes; when you have nodes the inbound form says so.

## ShadowTLS

**Inbounds → New inbound → ShadowTLS.** Observers see a normal TLS 1.3 handshake with a real website; inside it runs Shadowsocks 2022.

- **Handshake server:** a real site with TLS 1.3 (default `www.microsoft.com`, port 443). Clients use its name as SNI. Pick a large, reachable site that is not blocked where your users are.
- **Inner Shadowsocks cipher:** `2022-blake3-aes-128-gcm` (default) or `2022-blake3-aes-256-gcm`. Every user gets their own ShadowTLS password and their own Shadowsocks 2022 key; the server key is generated.

There is no standard share link for ShadowTLS, so users get it from the **sing-box** or **Clash (mihomo)** subscription.

## AnyTLS

**Inbounds → New inbound → AnyTLS.** TLS with padding that hides the size pattern of the inner TLS handshake.

- **Certificate:** pick one from **Certificates**, or leave it on the automatic self-signed one (links then pin it).
- **Padding scheme (optional):** one rule per line, the first one `stop=N`, for example:

  ```
  stop=8
  0=30-30
  1=100-400
  ```

  Empty uses sing-box's default. **Fill in the default to edit** writes the default so you can change it.

Links: `anytls://`, plus sing-box and Clash.

## NaiveProxy

**Inbounds → New inbound → NaiveProxy.** An HTTPS (HTTP/2 CONNECT) proxy whose clients use Chrome's network stack, so the connection looks like Chrome browsing a site.

- **It needs a certificate from a real CA.** NaiveProxy clients do not accept self-signed certificates, so the panel refuses an inbound without one. Get a Let's Encrypt certificate for a domain or for the server IP in **Certificates**, or upload one. A domain pointing at the server works best; use it as SNI.
- ALPN is fixed (`h2`, `http/1.1`).

Links: `naive+https://user:pass@host:port`, plus the sing-box subscription (the client needs a sing-box build with NaiveProxy, as in the official releases). Clash (mihomo) and Xray have no NaiveProxy client, so it is left out of those formats.

## Subscription formats

| | Share link | sing-box | Clash (mihomo) | Xray JSON |
|---|---|---|---|---|
| ShadowTLS | none | yes | yes (shadow-tls plugin v3) | no |
| AnyTLS | `anytls://` | yes | yes | no |
| NaiveProxy | `naive+https://` | yes | no | no |
| Hysteria 2 with hopping | `hysteria2://` with `mport` | `server_ports` and `hop_interval` | `ports` and `hop-interval` | `udphop` mask (Xray-core 26.10.21+ clients) |

Entries a format cannot express are left out of it; the user's other inbounds stay.

## Hysteria 2 port hopping

In the **connection** step of a Hysteria 2 inbound, fill in **Port hopping** with one or more UDP ranges (start and end, separated by a dash; several ranges separated by commas, at most 4), for example:

```
20000-30000
20000-30000,40000-40100
```

Ports must be between 1024 and 65535. **Hop interval** is the seconds between hops on the client (0 = the client's default, 30 s; otherwise 5-3600). Empty turns hopping off.

How it works: the server keeps listening on the inbound's own port. Veilon adds an nftables table `inet veilon_hop` with a `prerouting` rule that redirects every UDP port of the ranges to that port, and clients change the port every few seconds. The rule only touches incoming traffic, so the server's own connections are not affected, and the host firewall sees the inbound's port. The table is rewritten when an inbound changes, removed when the inbound is deleted or Xray is switched off, put back if something flushes it, and removed when the panel stops.

What you need to check on the server:

- **The cloud firewall must allow the range.** If your provider has its own firewall (security groups and the like), open the whole UDP range there, not only the inbound's port.
- **`nft` must be installed** (it is on current Debian and Ubuntu). Without it the panel logs a warning and clients only reach the inbound's own port.
- **Test it once on the real server.** The redirect was tested with real clients, but not on every kernel and firewall setup. After saving, connect a client and check that it keeps working for a few minutes while it hops, and `nft list table inet veilon_hop` shows the rule. Some setups (other nftables or iptables NAT rules, Docker, a listen address that is not on this server) can stop the redirect.

Rules the panel checks: on the same server a hop range may not cover another inbound's UDP port or overlap another inbound's ranges, and a new UDP inbound may not use a port inside a hop range (`409` with `field: "hop_ports"`). TCP inbounds may use ports inside a range.

### On nodes

Port hopping works on nodes too, but the node agent must be this release or newer. A node with an older agent gets no hop rule and its users' links carry no hop ports (they keep working on the inbound's own port). Update the node from **More → Nodes**.

## IP log note for ShadowTLS

sing-box hands a ShadowTLS connection to its inner Shadowsocks inbound under a new connection id, so the panel pairs each client address with the next user that signs in on that inbound. When two users from different addresses connect in the same instant, their addresses can be swapped in the IP log and the IP limit. This only affects ShadowTLS; traffic counting is exact.

## Telegram bot and API

In the Telegram bot you can create these inbounds and change the ShadowTLS handshake server and the Hysteria 2 hop ranges (**🔀 Port hopping**); the AnyTLS padding scheme and the hop interval are set in the web panel. In the API: `protocol` `shadowtls`, `anytls`, `naive`, and `settings.handshake_server`, `settings.handshake_port`, `settings.padding_scheme`, `settings.hop_ports`, `settings.hop_interval`. See [api-v1](../api-v1.md).
