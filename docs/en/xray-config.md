# Xray config

**More → Xray config** edits the parts of the Xray core config that are not inbounds: outbounds, routing rules, balancers and DNS. Inbounds and users are still managed by the panel itself.

## Tabs

| Tab | What it does |
| --- | --- |
| **Basic** | Default outbound, blocking the server's private network (LAN, localhost), DNS query logging |
| **Outbounds** | Where traffic leaves the server. The first one is the default; `direct` and `blocked` always exist. Add freedom, blackhole, VLESS, VMess, Trojan, Shadowsocks, SOCKS, HTTP or WireGuard outbounds, or paste a share link |
| **Routing** | Rules matched top to bottom (domain, IP, port, network, protocol, inbound, user). Each rule sends traffic to an outbound or a balancer. Use the arrows to reorder |
| **Balancers** | Spread traffic across several outbounds: random, round robin, least ping or least load, with a fallback outbound |
| **DNS** | Custom core DNS servers (optionally only for some domains), query type, cache and static hosts. Off means Xray uses the system resolver |
| **Advanced JSON** | The same settings as raw JSON for anything the forms do not cover |

## Test and apply

- **Test** asks Xray itself to check the config; nothing changes on the server.
- **Apply** checks the config, then restarts Xray with it. If Xray rejects it, nothing is changed. If Xray does not stay up, the previous config is restored automatically.
- **Discard changes** drops every unsaved edit.

An outbound that a rule or balancer uses cannot be deleted until those are changed.
