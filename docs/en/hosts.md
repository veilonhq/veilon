# Hosts

Hosts let one inbound appear as several links in users' subscriptions, for example through a CDN, another domain or another port.

1. Open **Inbounds** and press the hosts button on an inbound row.
2. **Add host** and fill only what should differ from the inbound: link name, address, port, SNI, Host header, path, ALPN and fingerprint. Empty fields keep the inbound's value.
3. Save. Every enabled host becomes its own link in the subscription of every user of that inbound.

With no hosts, links are built exactly as before. Turning a host off removes its link without deleting it.
