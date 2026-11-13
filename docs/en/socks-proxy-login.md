# SOCKS inbound and custom proxy login

## SOCKS inbound

**Inbounds → New inbound → Utility → SOCKS** creates a plain SOCKS5 proxy (Mixed still serves SOCKS5 and HTTP on one port). It runs on Xray and can run on the main server or on nodes (**Runs on**).

- **Sign in with username and password** (default): every user signs in with their own login, and traffic, online state, IP and speed limits are per user, as on HTTP and Mixed.
- **No authentication:** anyone who reaches the port can use it. Users cannot be told apart, so the traffic is not counted for any user (it only shows in the server's total traffic), and users' data, IP and speed limits do not apply on this inbound. The inbound stays open while it is enabled, even when its users expire.
- **UDP:** allow SOCKS5 UDP ASSOCIATE (on by default).

Links: `socks://base64(user:pass)@host:port#name` (no authentication: `socks://host:port#name`), Clash type `socks5`, sing-box `socks` outbound version 5, and an Xray `socks` outbound in the JSON subscription.

## Custom proxy login per user

HTTP, Mixed and SOCKS inbounds sign in with a username and password. By default the username is `<id>.<username>` (e.g. `7.alice`) and the password is the user's generated password. In the user form, once at least one HTTP, Mixed or SOCKS inbound is selected for the user, a **Proxy login** block appears under **Access**:

- **Proxy username:** 3-32 Latin letters, digits, `.` `_` `-`. It must not start with digits and a dot (that form belongs to automatic logins). Unique across users (case-insensitive).
- **Proxy password:** 8-64 Latin letters, digits and `. _ - ~ ! @ # $ % ^ & * + =`, with an eye button to show it.
- Either field left empty uses the automatic value. If the proxy inbounds are removed from the user the block hides, but the stored values are kept.

The login applies to HTTP, Mixed and SOCKS only; other protocols (and MASQUE) are unchanged. The user's links and subscription use the new login right away, so the client has to refresh its subscription after a change.

Traffic and limits are still counted for the same user: on these inbounds Xray reports the user by the proxy username, and the panel maps it back to the user (traffic stats, online state, IP log, IP limit, speed limit, blocked IPs, torrent blocking, and routing rules that name the user). Nodes need this release's node agent; an older agent does not count the traffic of users with a custom proxy username on HTTP, Mixed and SOCKS inbounds.

API: `proxy_username` and `proxy_password` in `POST /users` and `PATCH /users/{id}` (empty string = automatic). A duplicate name answers `409` with `field: "proxy_username"`. Telegram bot: **Edit user → Proxy username / Proxy password** (`-` for automatic).

Database: schema 12 adds `users.proxy_username` and `users.proxy_password` (default empty) and a partial unique index on non-empty names; additive and safe to re-run, for SQLite and PostgreSQL.
