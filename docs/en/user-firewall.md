# Per-user firewall

Block domains and IP addresses for one user only. Open the user (**Users → edit**), go to the **Firewall** section, type an entry and press **Add** (or paste several, one per line or separated by commas). The list stays until you remove an entry, or press **Remove all**. Users with entries show a **Firewall: N blocked** badge in the list. The same list is in the Telegram bot (user screen → **🛡 Firewall**) and the API.

Veilon keeps no record of what users visit; the firewall only applies your own list.

## Accepted formats

| You type | Stored as | Blocks |
| --- | --- | --- |
| `example.com`, `domain:example.com`, `*.example.com`, `https://example.com/path` | `example.com` | the domain and every subdomain |
| `full:api.example.com` | `full:api.example.com` | exactly this name |
| `regexp:\.example\.(com\|net)$` | the same | names matching the regular expression |
| `1.2.3.4`, `2001:db8::1` | the same | one address |
| `10.0.0.0/8`, `2001:db8::/32` | the range, masked | an address range (CIDR) |

A single label such as `ir` blocks a whole top-level domain. Other prefixes (`geosite:`, `geoip:`, `keyword:`, `ext:` ...) are refused. Duplicates are dropped.

## Limits

- At most **200 entries** per user, each at most **253 characters**.
- The widest range is **/8 for IPv4** and **/16 for IPv6**. To cut a user off completely, disable the user instead.

## Where it applies

- **Xray and sing-box inbounds** (main server and nodes): every format. Xray sends matching traffic to a blackhole, sing-box rejects it.
- **AmneziaWG inbounds**: **IPv4 addresses and ranges only** (nftables reject per peer, table `inet veilon_userfw`). Domains and IPv6 entries have no effect there; the form says so when the user has an AmneziaWG inbound.
- **MTProto** is not covered.

## Things to know

- **An IP entry does not block traffic that reaches the site by its name.** When a client connects by domain name, Xray and sing-box match the name, not the address it resolves to. To block a site, add its domain too (the IP entry alone mainly catches connections made straight to the address, and AmneziaWG).
- **Saving restarts the cores briefly.** Every change rebuilds the config, so users on the server may see their connections drop for a moment.
- **Nodes need the node agent of this release (v0.3.13) for AmneziaWG entries.** Older agents keep working, but blocked IPs are not applied on that node's AmneziaWG inbounds; the node card and the bot's node screen warn about it. Xray and sing-box entries on nodes work through the normal config push.
- The AmneziaWG rules were tested in a test rig, not yet on every real kernel. After the first change, check `nft list table inet veilon_userfw` on the server.

## API

`GET/PUT/POST/DELETE /users/{id}/firewall` and the `firewall` field of the user. See [api-v1](../api-v1.md#per-user-firewall).
