# Nodes

The panel needs only one VPS. Nodes are optional: each node is another server that runs the Xray inbounds you place on it, with the same users. The panel pushes the config to nodes, collects their traffic and health, and adds one link per node to users' subscriptions.

> **⚠️ Do not install the panel on a node server.** A node server runs only the node install command below. Installing the Veilon panel on the same server as a node causes conflicts (ports and Xray). Use a separate server for each node.

## Add a node

1. In the panel open **Nodes → Add node**. Enter a name, the node server's IP or domain (users connect to this address) and the panel link port (default `62050`).
2. The panel shows a one-line install command. Run it as root on the node server:

   ```bash
   bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) node --token TOKEN
   ```

   Copy the full command from the panel; `TOKEN` is unique to that node and holds its key, so do not share it.
3. The installer sets up the program, Xray and the `veilon-node` service. The node turns online a few seconds later.

The panel link port must be reachable from the main server. The node opens it in ufw or firewalld when they are active; open it in your provider's firewall too, along with the node's inbound ports.

## Choose where an inbound runs

Placement is set in the inbound form, not on the Nodes page. Once at least one node exists, the form shows **Runs on**:

- **Main server:** the inbound runs on the main server only (as before).
- **Nodes:** pick one or more nodes. The inbound runs only on those nodes; the main server does not listen on its port and leaves it out of its firewall rules.

Users' links of an inbound that runs on nodes point at the nodes only: one link per node, named `<inbound> · <node>`, with the node's address. Hosts apply on top of each node's link, and a host address wins.

The port is required: a new inbound starts with an empty port and the form asks for one.

**Inbounds assigned to a node in v0.3.1 or earlier** keep running on the main server too, so connected clients are not cut off, but their links now point at the node. Their form shows **Also run on the main server**; turn it off to stop the main server listening on that port. If all their nodes are switched off, links point at the main server again. Deleting a node puts inbounds left without a node back on the main server.

Xray inbounds (except TUN), AmneziaWG and MTProto (Telegram proxy) can run on nodes; TUIC cannot. AmneziaWG and MTProto need a v0.3.8 or newer node agent: the form marks an older node "needs update" and does not offer it for them, and an inbound already placed on such a node is skipped there (the node's form says so) until the node is updated. On the node they run like on the main server (the built-in AmneziaWG server, which needs `/dev/net/tun`, and mtg-multi, downloaded on first use); users' links and `.conf` files point at the node, their traffic counts like Xray's, online status uses the traffic rule (no open-connection list for these two), and the client IPs they report (AmneziaWG endpoints that send traffic, MTProto's live client addresses) count for IP limits, with the same nftables drop on the node.

## How it works

- The panel and node talk over mutual TLS; the node only accepts the panel that enrolled it.
- Users' traffic on nodes counts towards their data caps like traffic on the main server. Each node's traffic batch is counted exactly once, even when the panel or the node restarts.
- **Traffic by server:** the Dashboard history card and the Nodes page show how much users' traffic each server carried (main server, each node, removed nodes) and the total for all servers. A user's usage is the sum over all servers.
- **IP limits across nodes:** a user's IP limit counts the IPs seen on the main server and on all nodes together. When a user goes over the limit, the newest IPs are blocked for 3 minutes on the main server and on every node.
- **Speed limits** apply on nodes too. Each server shapes its own connections, so a user connected through two servers at once can reach the limit on each.
- Device (HWID) limits are checked by the panel on every subscription request, so they cover nodes as well.
- The Nodes page shows status (online, needs attention, unreachable, waiting for install, off), last contact, latency, Xray version, CPU load, memory, traffic and whether the config is in sync.
- With **Node is on** turned off, the node serves nothing and its links leave the subscriptions.
- **New install command** issues a new certificate; the node stays disconnected until the new command is run.

## Update a node

Each node card shows its agent version. When it is older than the main server's version, the card shows the new version and an **Update node** button; **Update all outdated nodes** at the top of the page updates every such node at once. Only sudo admins see the Nodes page.

What happens when you press it:

1. The main server asks the node, over the same mutual-TLS channel it already uses, to install the main server's own version. It sends the release file for the node's CPU (amd64 or arm64) from the same release source as the panel update (GitHub, or the feed set in `VEILON_UPDATE_API`) and its SHA-256 from the release's `checksums.txt`.
2. **Downloading**: the node downloads the file into its state directory.
3. **Verifying**: the node checks the SHA-256 and that the file runs and reports the expected version. On a mismatch nothing is changed.
4. **Restarting**: a helper outside the node service (a short `systemd-run` unit) keeps a backup of the current program (`/var/lib/veilon-node/update/backup/veilon`), replaces `/usr/local/bin/veilon` atomically and restarts `veilon-node`.
5. **Done**: the new agent reports its version to the main server. If the new version does not reconnect to the main server within 90 seconds, the helper restores the previous program and restarts the node again; the card then shows the failure and that the previous version was restored.

The node's certificate and enrollment are not touched. One update runs per node at a time, and a node can be asked again 30 seconds after the last request. The main server never downgrades a node (a node newer than the main server is left alone), refuses when the node's CPU architecture is unknown, and only updates to a release version. Every request and its outcome are in the audit log (`node.update.requested`, `node.update.ok`, `node.update.failed`). Times on the card are in the panel's time zone.

Agents older than v0.3.5 cannot update themselves. Their card says **Needs one manual update**: choose **New install command** and run it on the node server once (it installs the latest program and keeps the node's settings). From then on the button above works.

IP limits and speed limits on nodes need the node agent of v0.3.2 or newer. Older nodes keep serving users, but they do not report IPs or take bans and speed limits.

## Deleting a node or taking an inbound off it

An inbound that runs only on nodes goes back to the main server when its last node is deleted or drops it. If its port is already used on the main server (by another inbound, or reserved for the panel), the delete is refused with a message naming the inbound and the port, and a link to edit that inbound: change its port or run it on another node first, then delete the node.

## On the node server

```bash
veilon node status
veilon node uninstall          # add --purge to remove its state and the veilon binary too
```
