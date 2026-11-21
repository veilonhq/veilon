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

// Hysteria 2 port hopping ranges are locked per server like ports,
// mirroring the server rule (checkHop in panel/internal/inbounds/hop.go):
// on a shared server (and overlapping listen address) one inbound's ranges
// may not overlap another inbound's ranges or cover another inbound's UDP
// port, and an inbound's UDP port may not lie in another inbound's ranges.

export type HopRange = [number, number];

/** Parses hop ranges like the server (ParseHopPorts); null when invalid. */
export function parseHopRanges(raw: string): HopRange[] | null {
  const v = raw
    .replace(/[۰-۹]/g, (c) => String(c.charCodeAt(0) - 0x6f0))
    .replace(/[٠-٩]/g, (c) => String(c.charCodeAt(0) - 0x660))
    .replace(/،/g, ',')
    .replace(/\s/g, '')
    .replace(/:/g, '-');
  if (!v) return [];
  const out: HopRange[] = [];
  for (const x of v.split(',').filter(Boolean)) {
    const m = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(x);
    if (!m) return null;
    const a = Number(m[1]);
    const b = m[2] ? Number(m[2]) : a;
    if (a < 1024 || b > 65535 || a > b) return null;
    out.push([a, b]);
  }
  return out.sort((x, y) => x[0] - y[0]);
}

export interface HopPlacement extends PortPlacement {
  remark?: string;
  listen?: string;
  /** hop ranges as stored ("" = none; only Hysteria 2 hops) */
  hop_ports?: string;
  /** the inbound listens on UDP */
  udp: boolean;
}

/** What a hop range clashes with, and where. */
export type HopClash = {
  field: 'hop' | 'port';
  kind: 'overlap' | 'covers' | 'inside';
  other: string;
  where: { server: 'main' } | { server: 'node'; nodeId: number; name: string };
};

const anyListen = (x?: string) => !x || x === '0.0.0.0' || x === '::';
const listenOverlap = (a?: string, b?: string) => anyListen(a) || anyListen(b) || a === b;
const rangesOf = (v: HopPlacement) => (v.protocol === 'hysteria' && v.hop_ports ? parseHopRanges(v.hop_ports) ?? [] : []);

/** The first hop clash of `v` with `others`, or null (see the rule above). */
export function hopClash(v: HopPlacement, others: HopPlacement[], nodes: PortNode[]): HopClash | null {
  if (v.protocol === 'tun') return null;
  const mine = rangesOf(v);
  if (mine.length === 0 && !v.udp) return null;
  const want = serversOf(v);
  const nodeName = (id: number) => nodes.find((n) => n.id === id)?.name ?? `#${id}`;
  const list = [...others].filter((o) => o.id !== v.id && o.protocol !== 'tun').sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
  for (const o of list) {
    const os = serversOf(o);
    let where: HopClash['where'] | null = null;
    if (want.main && os.main) where = { server: 'main' };
    else {
      const shared = want.nodes.find((id) => os.nodes.includes(id));
      if (shared !== undefined) where = { server: 'node', nodeId: shared, name: nodeName(shared) };
    }
    if (!where || !listenOverlap(o.listen, v.listen)) continue;
    const other = o.remark ?? '';
    const theirs = rangesOf(o);
    for (const r of mine) {
      if (o.udp && o.port >= r[0] && o.port <= r[1]) return { field: 'hop', kind: 'covers', other, where };
      for (const t of theirs) if (r[0] <= t[1] && t[0] <= r[1]) return { field: 'hop', kind: 'overlap', other, where };
    }
    if (v.udp) for (const t of theirs) if (v.port >= t[0] && v.port <= t[1]) return { field: 'port', kind: 'inside', other, where };
  }
  return null;
}
