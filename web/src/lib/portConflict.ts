// Ports are unique per server, mirroring the server rule (see
// panel/internal/inbounds/ports.go). The servers of an inbound are the main
// server when it runs there (no nodes, or serve_main) plus each of its
// nodes; two inbounds conflict only when they share a port and a server.

export interface PortPlacement {
  id?: number;
  port: number;
  protocol: string;
  serve_main?: boolean;
  node_ids?: number[];
}

export interface PortNode {
  id: number;
  name: string;
  api_port: number;
}

/** Where a port is taken: the main server or a named node. */
export type PortClash = { server: 'main'; other?: string } | { server: 'node'; nodeId: number; name: string; other?: string };

interface Servers {
  main: boolean;
  nodes: number[];
}

function serversOf(v: Pick<PortPlacement, 'serve_main' | 'node_ids'>): Servers {
  const nodes = [...new Set(v.node_ids ?? [])].sort((a, b) => a - b);
  return { main: !!v.serve_main || nodes.length === 0, nodes };
}

/**
 * The first server where `v.port` is already taken, or null. `prev` is the
 * stored inbound being edited: with the same port, only servers it did not
 * run on before are checked (so data stored before the rule stays
 * editable). `reservedMain` are ports the main server keeps for itself
 * (the panel port); each node keeps its agent port.
 */
export function portClash(v: PortPlacement, others: (PortPlacement & { remark?: string })[], nodes: PortNode[], reservedMain: number[], prev?: PortPlacement | null): PortClash | null {
  if (v.protocol === 'tun') return null;
  const want = serversOf(v);
  if (prev && prev.port === v.port && prev.protocol !== 'tun') {
    const had = serversOf(prev);
    want.main = want.main && !had.main;
    want.nodes = want.nodes.filter((id) => !had.nodes.includes(id));
  }
  if (!want.main && want.nodes.length === 0) return null;
  const nodeName = (id: number) => nodes.find((n) => n.id === id)?.name ?? `#${id}`;
  if (want.main && reservedMain.includes(v.port)) return { server: 'main' };
  for (const id of want.nodes) {
    const n = nodes.find((x) => x.id === id);
    if (n && n.api_port === v.port) return { server: 'node', nodeId: id, name: n.name };
  }
  const list = [...others].filter((o) => o.id !== v.id && o.protocol !== 'tun' && o.port === v.port).sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
  for (const o of list) {
    const os = serversOf(o);
    const other = o.remark;
    if (want.main && os.main) return { server: 'main', other };
    const shared = want.nodes.find((id) => os.nodes.includes(id));
    if (shared !== undefined) return { server: 'node', nodeId: shared, name: nodeName(shared), other };
  }
  return null;
}
