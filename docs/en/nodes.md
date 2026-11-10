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

Only Xray inbounds (except TUN) can run on nodes.

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

IP limits and speed limits on nodes need the node agent of v0.3.2 or newer. Older nodes keep serving users, but they do not report IPs or take bans and speed limits. To update a node, open it on the Nodes page, choose **New install command** and run that command on the node server: it installs the latest program and keeps the node's settings.

## On the node server

```bash
veilon node status
veilon node uninstall          # add --purge to remove its state and the veilon binary too
```
