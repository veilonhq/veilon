# VLESS Encryption

VLESS Encryption is the VLESS protocol's own encryption layer in Xray (since Xray 25.9.5): a post-quantum ML-KEM-768 + X25519 key exchange that needs no TLS or Reality and works on every transport (TCP, WebSocket, gRPC, HTTPUpgrade and XHTTP). The server keeps the private key (the `decryption` string); clients only get the public key (the `encryption` string).

## Turning it on

In **Inbounds → New inbound, or edit a VLESS inbound → Security step**, switch on **VLESS Encryption**:

- **Server authentication key:** `X25519` (short key and links) or `ML-KEM-768` (post-quantum authentication; the client key is about 1,600 characters, so links get longer). The connection key exchange is post-quantum either way.
- **Traffic appearance:** `native` (raw VLESS packets, fastest), `xorpub` (like native, with the public key masked) or `random` (all traffic looks random).
- **Reconnects:** `0-RTT` (the server hands out tickets and clients reconnect without a full handshake; **ticket lifetime** in seconds, default 600, Xray picks between half and all of it each time) or `1-RTT` (a full handshake every time).
- **Keys:** switching it on makes a key. **Generate keys** makes a new pair with the Xray installed on the server (like `xray x25519` and `xray mlkem768`; the panel makes it itself when Xray is not available). You can also paste a server key or a whole `decryption` string (for example from `xray vlessenc`). Changing the key changes every user's link for that inbound.
- **Flow:** with encryption on, Vision (`xtls-rprx-vision`) can be chosen on every transport, even without TLS. Set it to `none` per inbound when a client or setup does not connect with Vision.

Switching it off keeps the keys; switching it back on brings the same links back.

## Clients and links

- `vless://` links carry `encryption=mlkem768x25519plus...` (`encryption=none` when off).
- Clash (mihomo) gets the `encryption` field, and the user's JSON subscription has `encryption` in the Xray outbound `users`.
- **sing-box** has no VLESS Encryption, so these inbounds are left out of sing-box output. Clash still has no XHTTP, as before.
- Clients need Xray 25.9.5 or newer (or a recent mihomo).

## Nodes

Encrypted inbounds run on nodes too (the node's Xray config gets the same `decryption` string); no new node agent is needed. The panel refuses to save when it knows the main server's or a chosen node's Xray is older than 25.9.5.

## Telegram bot

Each VLESS inbound screen has **🔐 VLESS Encryption**: on or off, X25519 or ML-KEM-768, native, xorpub or random, 0-RTT or 1-RTT, ticket lifetime, a new key (with a confirm) and Vision on or off.

## API

The `vless_encryption` field on VLESS inbounds and `POST /inbounds/vless-enc-keys` to make keys. [api-v1](../api-v1.md)
