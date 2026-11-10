# Nodes

The panel needs only one VPS. Nodes are optional: each node is another server that serves the Xray inbounds you pick, with the same users. The panel pushes the config to nodes, collects their traffic and health, and adds one link per node to users' subscriptions.

## Add a node

1. In the panel open **Nodes → Add node**. Enter a name, the node server's IP or domain (users connect to this address), the panel link port (default `62050`) and the inbounds it should serve.
2. The panel shows a one-line install command. Run it as root on the node server:

   ```bash
   bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) node --token TOKEN
   ```

   Copy the full command from the panel; `TOKEN` is unique to that node and holds its key, so do not share it.
3. The installer sets up the program, Xray and the `veilon-node` service. The node turns online a few seconds later.

The panel link port must be reachable from the main server. The node opens it in ufw or firewalld when they are active; open it in your provider's firewall too, along with the node's inbound ports.

## How it works

- The panel and node talk over mutual TLS; the node only accepts the panel that enrolled it.
- Only Xray inbounds (except TUN) run on nodes. They keep running on the main server too.
- Users' traffic on nodes counts towards their data caps like traffic on the main server.
- The Nodes page shows status (online, needs attention, unreachable, waiting for install, off), last contact, latency, Xray version, CPU load, memory, traffic and whether the config is in sync.
- With **Node is on** turned off, the node serves nothing and its links leave the subscriptions.
- **New install command** issues a new certificate; the node stays disconnected until the new command is run.

## On the node server

```bash
veilon node status
veilon node uninstall          # add --purge to remove its state and the veilon binary too
```

## Limits

- Per-user IP and device limits are enforced on the main server only, not on nodes.
