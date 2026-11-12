import { AlertTriangle, ArrowLeft, ArrowRight, Check, KeyRound, Loader2, Lock, ShieldCheck, ShieldOff, Shuffle, Sparkles } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { genRealityKeys, genWgKeys, useCerts, useInbounds, useNodes, useSaveInbound, useSettings } from '@/api/hooks';
import { ApiError } from '@/api/client';
import { portClash, type PortClash } from '@/lib/portConflict';
import type { AwgParams, Inbound, InboundInput, InboundSettings, Network, Protocol, Security, VNode } from '@/api/types';
import { Sheet } from '@/components/Overlay';
import { Select } from '@/components/Select';
import { Field, Switch } from '@/components/misc';
import { useI18n, type MessageKey } from '@/i18n';
import {
  PROTOCOL_GROUPS,
  MASQUE_PATH,
  SS_METHODS,
  emptyReality,
  emptySettings,
  emptyTls,
  inboundDefaults,
  nodeCapable,
  nodeTooOld,
  portHint,
  quicTls,
  engineName,
  engineOf,
  hasPort,
  hasUsers,
  isQuic,
  isV2,
  isWG,
  needsNetAdmin,
  protoName,
  sniffingApplies,
  transportLabel,
  transportsOf,
  withSettings,
} from '@/lib/protocols';
import { toast } from '@/store/feedback';
import { ProtoGlyph } from './ProtoGlyph';
import s from './Inbounds.module.css';

const NETWORKS: Network[] = ['tcp', 'ws', 'grpc', 'httpupgrade', 'xhttp'];
const SECURITIES: Security[] = ['none', 'tls', 'reality'];
const FPS = ['chrome', 'firefox', 'safari', 'edge', 'ios', 'android', 'random', 'randomized'];
// Reality only works with these client fingerprints.
const REALITY_FPS = ['chrome', 'firefox', 'safari'];
const ALPNS = ['h3', 'h2', 'http/1.1'];

type StepId = 'protocol' | 'connection' | 'security' | 'keys' | 'review';
const STEP_LABEL: Record<StepId, MessageKey> = {
  protocol: 'inb.step.protocol',
  connection: 'inb.step.connection',
  security: 'inb.step.security',
  keys: 'inb.step.keys',
  review: 'inb.step.review',
};
function stepsFor(p: Protocol): StepId[] {
  if (isV2(p) || isQuic(p) || p === 'http') return ['protocol', 'connection', 'security', 'review'];
  if (isWG(p)) return ['protocol', 'connection', 'keys', 'review'];
  return ['protocol', 'connection', 'review'];
}

const defaults = inboundDefaults;

export const realityAllowed = (d: Pick<InboundInput, 'protocol' | 'network'>) =>
  (d.protocol === 'vless' || d.protocol === 'trojan') && (d.network === 'tcp' || d.network === 'grpc' || d.network === 'xhttp');

function toInput(ib: Inbound): InboundInput {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { id, user_count, created_at, on_main, host_count, ...rest } = ib;
  return { ...rest, node_ids: rest.node_ids ?? [], serve_main: rest.serve_main ?? true, settings: withSettings(rest.settings) };
}

/** Strip fields that do not apply to the protocol before sending. */
function toBody(d0: InboundInput): InboundInput {
  // Placement: no nodes (or a protocol a node cannot run) = main server.
  const d = !nodeCapable(d0.protocol) || !d0.node_ids.length ? { ...d0, node_ids: [], serve_main: true } : d0;
  const p = d.protocol;
  if (isV2(p)) {
    const r = d.reality && !REALITY_FPS.includes(d.reality.fingerprint) ? { ...d.reality, fingerprint: 'chrome' } : d.reality;
    return { ...d, reality: r, settings: emptySettings() };
  }
  const b: InboundInput = { ...d, network: 'tcp', transport: { path: '', host: '', service_name: '', mode: '' }, flow: '', ss_method: '', reality: null };
  if (isQuic(p)) b.security = 'tls';
  else if (p === 'http') {
    if (b.security !== 'tls') b.tls = null;
    if (b.security === 'reality') b.security = 'none';
  } else {
    b.security = 'none';
    b.tls = null;
  }
  if (!sniffingApplies(p)) b.sniffing = false;
  if (!hasPort(p)) {
    b.port = 0;
    b.listen = '';
  }
  return b;
}

/* ---------- validation helpers (mirror the server) ---------- */
const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const HOST = /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$/;
const IFACE = /^[a-zA-Z][a-zA-Z0-9_-]{0,14}$/;
const isIP = (x: string) => IPV4.test(x) || (x.includes(':') && /^[0-9a-fA-F:.]+$/.test(x));
function cidr(x: string): { v4: boolean; bits: number } | null {
  const m = /^(.+)\/(\d{1,3})$/.exec(x.trim());
  if (!m) return null;
  const bits = Number(m[2]);
  if (IPV4.test(m[1])) return bits <= 32 ? { v4: true, bits } : null;
  if (m[1].includes(':') && /^[0-9a-fA-F:]+$/.test(m[1])) return bits <= 128 ? { v4: false, bits } : null;
  return null;
}
const between = (n: number, min: number, max: number) => Number.isFinite(n) && n >= min && n <= max;

/** Comma separated list field that keeps what the user is typing. */
function CsvInput({ value, onChange, placeholder, invalid }: { value: string[]; onChange: (v: string[]) => void; placeholder?: string; invalid?: boolean }) {
  const [text, setText] = useState(value.join(', '));
  useEffect(() => {
    const parsed = text
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean);
    if (parsed.join(',') !== value.join(',')) setText(value.join(', '));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  return (
    <input
      className="input ltr mono"
      value={text}
      placeholder={placeholder}
      aria-invalid={invalid || undefined}
      spellCheck={false}
      autoCapitalize="none"
      onChange={(e) => {
        setText(e.target.value);
        onChange(
          e.target.value
            .split(',')
            .map((x) => x.trim())
            .filter(Boolean),
        );
      }}
    />
  );
}

function NumInput({ value, onChange, min, max, placeholder, invalid }: { value: number; onChange: (n: number) => void; min?: number; max?: number; placeholder?: string; invalid?: boolean }) {
  return (
    <input
      className="input num ltr"
      type="number"
      inputMode="numeric"
      min={min}
      max={max}
      value={Number.isFinite(value) ? value : ''}
      placeholder={placeholder}
      aria-invalid={invalid || undefined}
      onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))}
    />
  );
}

export function InboundWizard({ open, onClose, editing }: { open: boolean; onClose: () => void; editing: Inbound | null }) {
  const { t, n, date } = useI18n();
  const [step, setStep] = useState(0);
  const [dir, setDir] = useState(1);
  const [d, setD] = useState<InboundInput>(defaults('vless'));
  const [genBusy, setGenBusy] = useState(false);
  const [showErr, setShowErr] = useState(false);
  const [serverErr, setServerErr] = useState<string | null>(null);
  const certs = useCerts();
  const save = useSaveInbound();
  const allInbounds = useInbounds();
  const panelSettings = useSettings();
  const nodesQ = useNodes();
  const nodes: VNode[] = nodesQ.data ?? [];
  // "where it runs": main server, or nodes (multi-select). Inbounds placed
  // on a node by an older version also keep the main server; that is shown
  // as a switch only for them.
  const [onNodes, setOnNodes] = useState(false);
  const legacyBoth = !!editing && editing.serve_main && (editing.node_ids?.length ?? 0) > 0;

  useEffect(() => {
    if (!open) return;
    setStep(editing ? 1 : 0);
    setD(editing ? toInput(editing) : defaults('vless'));
    setOnNodes(!!editing && (editing.node_ids?.length ?? 0) > 0);
    setShowErr(false);
    setServerErr(null);
  }, [open, editing]);

  const steps = stepsFor(d.protocol);
  const last = steps.length - 1;
  const stepId = steps[Math.min(step, last)];
  const p = d.protocol;
  const patch = (x: Partial<InboundInput>) => setD((v) => ({ ...v, ...x }));
  const patchS = (x: Partial<InboundSettings>) => setD((v) => ({ ...v, settings: { ...v.settings, ...x } }));
  const patchT = (x: Partial<InboundInput['transport']>) => setD((v) => ({ ...v, transport: { ...v.transport, ...x } }));
  const patchTls = (x: Partial<NonNullable<InboundInput['tls']>>) => setD((v) => ({ ...v, tls: { ...(v.tls ?? (isQuic(v.protocol) ? quicTls() : emptyTls())), ...x } }));
  const patchReality = (x: Partial<NonNullable<InboundInput['reality']>>) => setD((v) => ({ ...v, reality: { ...(v.reality ?? emptyReality()), ...x } }));
  const patchAwg = (x: Partial<AwgParams>) => setD((v) => ({ ...v, settings: { ...v.settings, awg: { ...(v.settings.awg ?? { jc: 4, jmin: 40, jmax: 70, s1: 0, s2: 0, h1: 1, h2: 2, h3: 3, h4: 4 }), ...x } } }));

  const pickProtocol = (np: Protocol) => setD((x) => ({ ...defaults(np), remark: x.remark, port: hasPort(np) ? x.port : 0, node_ids: x.node_ids, serve_main: x.serve_main }));
  const setNetwork = (net: Network) => {
    setD((x) => {
      const next = { ...x, network: net };
      if (x.security === 'reality' && !realityAllowed(next)) {
        next.security = 'tls';
        next.reality = null;
        next.tls = x.tls ?? emptyTls();
      }
      if (net !== 'tcp') next.flow = '';
      if ((net === 'ws' || net === 'httpupgrade' || net === 'xhttp') && !x.transport.path) next.transport = { ...x.transport, path: '/' + net };
      if (net === 'grpc' && !x.transport.service_name) next.transport = { ...x.transport, service_name: 'grpc', mode: 'gun' };
      if (net === 'xhttp' && !x.transport.mode) next.transport = { ...next.transport, mode: 'auto' };
      return next;
    });
  };
  const setSecurity = (sec: Security) => {
    setD((x) => ({
      ...x,
      security: sec,
      tls: sec === 'tls' ? x.tls ?? emptyTls() : null,
      reality: sec === 'reality' ? x.reality ?? emptyReality() : null,
      flow: x.protocol === 'vless' && x.network === 'tcp' && sec !== 'none' ? x.flow : '',
    }));
  };

  const genReality = async () => {
    setGenBusy(true);
    try {
      const k = await genRealityKeys();
      patchReality({ private_key: k.private_key, public_key: k.public_key, short_ids: [k.short_id] });
      toast.success(t('inb.genKeysDone'));
    } catch (e) {
      toast.error(t('common.error'), (e as Error).message);
    } finally {
      setGenBusy(false);
    }
  };
  const genWg = async (awgOnly = false) => {
    setGenBusy(true);
    try {
      const k = await genWgKeys();
      if (awgOnly) patchS({ awg: k.awg });
      else patchS({ private_key: k.private_key, public_key: k.public_key, ...(p === 'amneziawg' && !d.settings.awg ? { awg: k.awg } : {}) });
      if (!awgOnly) toast.success(t('inb.genWgDone'));
    } catch (e) {
      toast.error(t('common.error'), (e as Error).message);
    } finally {
      setGenBusy(false);
    }
  };

  // Names are unique across inbounds; ports are unique per server (the
  // main server, each node), like the server rule (it answers 409 too).
  // These checks run live while typing; a value the edited inbound already
  // had is never flagged, matching the server.
  const portMsg = (c: PortClash) =>
    c.server === 'main' ? t('inb.portUsedMain', { port: n(d.port, { useGrouping: false }) }) : t('inb.portUsedNode', { port: n(d.port, { useGrouping: false }), name: c.name });
  const dupErrors = useMemo(() => {
    const e: Record<string, string> = {};
    const others = (allInbounds.data ?? []).filter((i) => i.id !== editing?.id);
    const name = d.remark.trim().toLowerCase();
    if (name && (!editing || editing.remark.trim().toLowerCase() !== name)) {
      const o = others.find((i) => i.remark.trim().toLowerCase() === name);
      if (o) e.remark = t('inb.nameTaken', { name: o.remark });
    }
    if (hasPort(p) && d.port >= 1 && d.port <= 65535) {
      // What is sent: "main server" drops the nodes (see submit).
      const placed = onNodes ? { serve_main: d.serve_main, node_ids: d.node_ids } : { serve_main: true, node_ids: [] };
      const reserved = panelSettings.data?.panel_port ? [panelSettings.data.panel_port] : [];
      const c = portClash({ id: editing?.id, port: d.port, protocol: p, ...placed }, others, nodes, reserved, editing);
      if (c) e.port = portMsg(c);
    }
    return e;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allInbounds.data, panelSettings.data, editing, d.remark, d.port, d.serve_main, d.node_ids, onNodes, nodes, p, t, n]);

  const errors = useMemo(() => {
    const e: Record<string, string> = {};
    const st = d.settings;
    const range = (min: number, max: number) => t('inb.range', { min: n(min), max: n(max) });
    if (step >= 1) {
      if (!d.remark.trim()) e.remark = t('form.required');
      if (hasPort(p) && !d.port) e.port = t('inb.portRequired');
      else if (hasPort(p) && !(d.port >= 1 && d.port <= 65535)) e.port = t('inb.portInvalid');
      if (d.listen.trim() && !isIP(d.listen.trim())) e.listen = t('inb.invalidHost');
      if (p === 'hysteria') {
        if (!between(st.up_mbps, 0, 100000)) e.up = range(0, 100000);
        if (!between(st.down_mbps, 0, 100000)) e.down = range(0, 100000);
      }
      if (isWG(p)) {
        if (st.address.trim()) {
          const c = cidr(st.address);
          if (!c || !c.v4 || c.bits < 16 || c.bits > 30) e.address = t('inb.invalidCidr');
        }
        if (!between(st.mtu, 1280, 1500)) e.mtu = range(1280, 1500);
        if (!between(st.keepalive, 0, 600)) e.keepalive = range(0, 600);
        if (st.dns.some((x) => !isIP(x))) e.dns = t('inb.invalidHost');
      }
      if (p === 'tun') {
        if (!IFACE.test(st.tun_name)) e.tunName = t('inb.tunNameHint');
        if (!between(st.mtu, 576, 9000)) e.mtu = range(576, 9000);
        const g = st.gateway.map(cidr);
        if (!st.gateway.length || g.some((x) => !x) || g.filter((x) => x?.v4).length > 1 || g.filter((x) => x && !x.v4).length > 1) e.gateway = t('inb.gatewayHint');
        if (st.routes.some((x) => !cidr(x))) e.routes = t('inb.invalidCidr');
        else if (st.routes.some((x) => /^0\.0\.0\.0\/0$|^::\/0$/.test(x.trim()))) e.routes = t('inb.noDefaultRoute');
      }
      if (p === 'masque') {
        const g = st.pool.map(cidr);
        if (!st.pool.length || g.some((x) => !x) || g.filter((x) => x?.v4).length > 1 || g.filter((x) => x && !x.v4).length > 1) e.pool = t('inb.poolHint');
        if (!between(st.mtu, 1280, 65535)) e.mtu = range(1280, 65535);
        if (st.path && !st.path.startsWith('/')) e.path = t('inb.invalidHost');
      }
      if (p === 'tunnel') {
        const a = st.target_address.trim();
        if (!a) e.target = t('form.required');
        else if (!isIP(a) && !HOST.test(a) && a !== 'localhost') e.target = t('inb.invalidHost');
        if (!between(st.target_port, 1, 65535)) e.targetPort = t('inb.portInvalid');
      }
      if (p === 'mtproto' && st.faketls_domain && !HOST.test(st.faketls_domain.trim())) e.faketls = t('inb.invalidHost');
    }
    if (step >= 2) {
      if (steps.includes('security')) {
        if ((isV2(p) || p === 'http') && d.security === 'tls' && !d.tls?.cert_id) e.cert = t('inb.needCert');
        if (d.security === 'reality') {
          if (!d.reality?.dest) e.dest = t('form.required');
          if (!d.reality?.server_names.length) e.serverNames = t('form.required');
        }
        if (p === 'masque' && !d.tls?.alpn.length) e.alpn = t('form.required');
      }
      if (p === 'amneziawg' && st.awg) {
        const a = st.awg;
        const hs = [a.h1, a.h2, a.h3, a.h4];
        if (
          !between(a.jc, 0, 128) ||
          a.jmin < 0 ||
          a.jmax > 1280 ||
          a.jmin > a.jmax ||
          !between(a.s1, 0, 1132) ||
          !between(a.s2, 0, 1188) ||
          (a.s1 > 0 && a.s1 + 56 === a.s2) ||
          hs.some((h) => h < 5) ||
          new Set(hs).size !== 4
        )
          e.awg = t('inb.awgRule');
      }
    }
    if (step >= 1 && onNodes && nodeCapable(p) && nodes.length > 0 && d.node_ids.length === 0) e.nodes = t('inb.pickNodes');
    if (step >= 1) Object.assign(e, dupErrors);
    return e;
  }, [d, step, steps, p, t, n, dupErrors, onNodes, nodes.length]);
  // Only errors that belong to steps up to the current one block "next".
  const stepValid = Object.keys(errors).length === 0;

  const next = () => {
    if (!stepValid) {
      setShowErr(true);
      return;
    }
    setShowErr(false);
    setDir(1);
    setStep((x) => Math.min(last, x + 1));
  };
  const back = () => {
    setDir(-1);
    setShowErr(false);
    setStep((x) => Math.max(0, x - 1));
  };
  const submit = () => {
    if (!stepValid) {
      setShowErr(true);
      return;
    }
    setServerErr(null);
    save.mutate(
      { id: editing?.id, body: toBody(onNodes ? d : { ...d, node_ids: [], serve_main: true }) },
      {
        onSuccess: () => {
          toast.success(editing ? t('inb.saved') : t('inb.created'));
          onClose();
        },
        onError: (e) => {
          // A port conflict names its server; show it in the panel's language.
          const x = e instanceof ApiError ? e.details : undefined;
          if (x?.field === 'port' && (x.server === 'main' || x.server === 'node')) {
            setServerErr(portMsg(x.server === 'main' ? { server: 'main' } : { server: 'node', nodeId: Number(x.node_id), name: String(x.node_name ?? '') }));
          } else if (e instanceof ApiError && e.code === 'node_agent_old') {
            setServerErr(t('inb.nodeOldErr', { node: String(x?.node_name ?? '') }));
          } else setServerErr((e as Error).message);
        },
      },
    );
  };

  const err = (k: string) => (showErr ? errors[k] : dupErrors[k]);
  const daysLeft = (iso: string) => Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
  const flowAllowed = d.protocol === 'vless' && d.network === 'tcp' && d.security !== 'none';
  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
  const BackIcon = rtl ? ArrowRight : ArrowLeft;
  const NextIcon = rtl ? ArrowLeft : ArrowRight;
  const tr = transportsOf(d);
  const portLabel = tr.tcp && tr.udp ? t('inb.portTcpUdp') : tr.udp ? t('inb.udpPort') : isV2(p) ? t('inb.port') : t('inb.portTcp');

  const certButtons = (
    <div className={s.span2}>
      <span className="field__label">{t('inb.cert')}</span>
      {certs.data && certs.data.length === 0 ? (
        <div className={s.noCert}>
          <span>{t('inb.noCerts')}</span>
          <Link to="/certs" className="btn btn--sm">
            {t('inb.getCert')}
          </Link>
        </div>
      ) : (
        <div className={s.certPick} role="radiogroup" aria-label={t('inb.cert')}>
          {(certs.data ?? []).map((c) => {
            const left = daysLeft(c.not_after);
            return (
              <button
                key={c.id}
                type="button"
                role="radio"
                aria-checked={d.tls?.cert_id === c.id}
                className={`${s.certOpt} ${d.tls?.cert_id === c.id ? s.tileOn : ''}`}
                onClick={() => patchTls({ cert_id: c.id, sni: d.tls?.sni || c.domain })}
              >
                <Lock size={16} />
                <span className={s.certText}>
                  <strong className="ltr">{c.domain}</strong>
                  <small>
                    {date(c.not_after)} · <span className={left < 14 ? s.warnText : undefined}>{t('certs.daysLeft', { n: left })}</span>
                  </small>
                </span>
              </button>
            );
          })}
        </div>
      )}
      {err('cert') && <span className="field__error">{err('cert')}</span>}
    </div>
  );

  const reviewRows: [string, ReactNode][] = [];
  {
    const st = d.settings;
    if (nodes.length > 0 && nodeCapable(p)) {
      const picked = onNodes ? nodes.filter((x) => d.node_ids.includes(x.id)).map((x) => x.name) : [];
      reviewRows.push([t('inb.runsOn'), picked.length ? (d.serve_main && legacyBoth ? [t('inb.runMain'), ...picked] : picked).join('، ') : t('inb.runMain')]);
    }
    reviewRows.push([
      t('inb.engine'),
      <span key="eng" className={s.engineTag} data-engine={engineOf(p)}>
        {engineName(engineOf(p))}
      </span>,
    ]);
    if (isV2(p)) {
      reviewRows.push([t('inb.step.transport'), `${d.network}${d.transport.path ? ` · ${d.transport.path}` : ''}${d.transport.service_name ? ` · ${d.transport.service_name}` : ''}`]);
      reviewRows.push([t('inb.step.security'), `${d.security}${d.tls?.sni ? ` · ${d.tls.sni}` : ''}${d.reality?.server_names[0] ? ` · ${d.reality.server_names[0]}` : ''}`]);
      if (d.flow) reviewRows.push([t('inb.flow'), d.flow]);
      if (d.ss_method) reviewRows.push([t('inb.ssMethod'), d.ss_method]);
    }
    if (isQuic(p) || p === 'http') {
      const c = certs.data?.find((x) => x.id === d.tls?.cert_id);
      reviewRows.push([t('inb.step.security'), p === 'http' && d.security !== 'tls' ? 'none' : `TLS · ${c ? c.domain : t('inb.selfSignedAuto')}${d.tls?.sni ? ` · ${d.tls.sni}` : ''}`]);
    }
    if (p === 'masque') reviewRows.push(['ALPN', (d.tls?.alpn ?? []).join(', ')]);
    if (p === 'hysteria') {
      if (st.obfs_password) reviewRows.push([t('inb.obfsPassword'), 'salamander']);
      if (st.up_mbps || st.down_mbps) reviewRows.push(['Mbps', `↑ ${st.up_mbps || '—'} · ↓ ${st.down_mbps || '—'}`]);
    }
    if (p === 'tuic') reviewRows.push([t('inb.congestion'), st.congestion || 'bbr']);
    if (p === 'masque') reviewRows.push([t('inb.pool'), st.pool.join(', ')]);
    if (isWG(p)) {
      reviewRows.push([t('inb.wgAddress'), st.address || t('common.auto')]);
      reviewRows.push([t('inb.dns'), st.dns.join(', ') || '—']);
      reviewRows.push([t('inb.mtu'), String(st.mtu)]);
      if (p === 'amneziawg') reviewRows.push([t('inb.awgParams'), st.awg ? `Jc ${st.awg.jc} · S1 ${st.awg.s1} · S2 ${st.awg.s2}` : t('common.auto')]);
    }
    if (p === 'tun') {
      reviewRows.push([t('inb.tunName'), st.tun_name]);
      reviewRows.push([t('inb.gateway'), st.gateway.join(', ')]);
      reviewRows.push([t('inb.routes'), st.routes.join(', ') || '—']);
    }
    if (p === 'mixed') reviewRows.push([t('inb.mixedUdp'), st.udp ? t('common.yes') : t('common.no')]);
    if (p === 'tunnel') reviewRows.push([t('inb.forwardTo'), `${st.target_address}:${st.target_port} · ${st.tunnel_network || 'tcp'}`]);
    if (p === 'mtproto') reviewRows.push([t('inb.fakeTls'), st.faketls_domain || 'www.cloudflare.com']);
    if (sniffingApplies(p)) reviewRows.push([t('inb.sniffing'), d.sniffing ? t('common.yes') : t('common.no')]);
  }
  const flowNodes = [
    protoName(p),
    isV2(p) ? d.network : transportLabel(tr) || 'tun',
    isV2(p) ? d.security : isQuic(p) || (p === 'http' && d.security === 'tls') ? 'tls' : engineName(engineOf(p)),
  ];

  return (
    <Sheet
      open={open}
      onClose={onClose}
      width={720}
      title={editing ? t('inb.wizard.editTitle') : t('inb.wizard.title')}
      subtitle={<span>{t('inb.stepOf', { n: step + 1, total: steps.length })}</span>}
      footer={
        <>
          {step > 0 && (
            <button className="btn" onClick={back}>
              <BackIcon size={17} />
              {t('common.back')}
            </button>
          )}
          {step < last ? (
            <button className="btn btn--primary" onClick={next}>
              {t('common.next')}
              <NextIcon size={17} />
            </button>
          ) : (
            <button className="btn btn--primary" onClick={submit} disabled={save.isPending}>
              {save.isPending ? <Loader2 size={17} className="spin" /> : <Check size={17} />}
              {save.isPending ? t('common.saving') : editing ? t('common.save') : t('common.create')}
            </button>
          )}
        </>
      }
    >
      <ol className={s.stepper} aria-label={t('inb.wizard.title')}>
        {steps.map((id, i) => (
          <li key={id} className={`${s.step} ${i === step ? s.stepNow : ''} ${i < step ? s.stepDone : ''}`} aria-current={i === step ? 'step' : undefined}>
            <button
              type="button"
              className={s.stepDot}
              disabled={i > step}
              onClick={() => {
                if (i < step) {
                  setDir(-1);
                  setStep(i);
                }
              }}
              aria-label={t(STEP_LABEL[id])}
            >
              {i < step ? <Check size={14} strokeWidth={3} /> : n(i + 1)}
            </button>
            <span className={s.stepLabel}>{t(STEP_LABEL[id])}</span>
          </li>
        ))}
      </ol>

      {/* Step change: a keyed div with a CSS transform-only enter. No exit
          animation and no AnimatePresence "wait": that waited for the old
          step's opacity exit to finish before mounting the next step, and in
          WebViews that never advance animations (Telegram on Android) it never
          finished, leaving the body blank. Content is visible even if the
          animation never runs. */}
        <div key={stepId} className={`${s.stepBody} ${dir > 0 ? s.stepFwd : s.stepBack}`}>
          {stepId === 'protocol' && (
            <div className={s.groups}>
              {PROTOCOL_GROUPS.map((g) => (
                <section key={g.id} className={s.group} aria-labelledby={`pg-${g.id}`}>
                  <header className={s.groupHead}>
                    <h3 id={`pg-${g.id}`}>{t(`inb.group.${g.id}` as MessageKey)}</h3>
                    <span>{t(`inb.group.${g.id}.d` as MessageKey)}</span>
                  </header>
                  <div className={s.protoTiles} role="radiogroup" aria-labelledby={`pg-${g.id}`}>
                    {g.protocols.map((pp) => {
                      const on = d.protocol === pp;
                      const ptr = transportsOf(defaults(pp));
                      const eng = engineOf(pp);
                      return (
                        <button key={pp} type="button" role="radio" aria-checked={on} className={`${s.protoTile} ${on ? s.tileOn : ''}`} onClick={() => pickProtocol(pp)} data-proto={pp}>
                          <span className={s.protoTop}>
                            <ProtoGlyph protocol={pp} size={40} />
                            <span className={s.engineTag} data-engine={eng} title={`${t('inb.engine')}: ${engineName(eng)}`}>
                              {engineName(eng)}
                            </span>
                            <span className={s.protoCheck} aria-hidden="true">
                              {on && <Check size={13} strokeWidth={3} />}
                            </span>
                          </span>
                          <strong className="display">{protoName(pp)}</strong>
                          <span className={s.protoDesc}>{t(`inb.proto.${pp}` as MessageKey)}</span>
                          <span className={s.protoTags}>
                            {transportLabel(ptr) && <i>{transportLabel(ptr)}</i>}
                            {pp === 'tun' && <i>L3</i>}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </section>
              ))}
            </div>
          )}

          {stepId === 'connection' && (
            <div className={s.formGrid}>
              <div className={s.span2}>
                <Field label={t('inb.remark')} error={err('remark')}>
                  <input className="input" value={d.remark} onChange={(e) => patch({ remark: e.target.value })} aria-invalid={!!err('remark')} data-autofocus placeholder={`${protoName(p)} · Main`} maxLength={128} />
                </Field>
              </div>

              {nodes.length > 0 && nodeCapable(p) && (
                <div className={s.span2}>
                  <span className="field__label">{t('inb.runsOn')}</span>
                  <div className="seg" role="radiogroup" aria-label={t('inb.runsOn')}>
                    <button type="button" role="radio" aria-checked={!onNodes} onClick={() => setOnNodes(false)}>
                      {t('inb.runMain')}
                    </button>
                    <button
                      type="button"
                      role="radio"
                      aria-checked={onNodes}
                      onClick={() => {
                        setOnNodes(true);
                        // A fresh choice of nodes runs on the nodes only.
                        if (!legacyBoth) patch({ serve_main: false });
                      }}
                    >
                      {t('inb.runNodes')}
                    </button>
                  </div>
                  {onNodes ? (
                    <>
                      <div className={s.nodePick} role="group" aria-label={t('inb.runNodes')}>
                        {nodes.map((nd) => {
                          const on = d.node_ids.includes(nd.id);
                          // An agent too old for this protocol cannot take it (one it already runs stays listed).
                          const old = nodeTooOld(p, nd.state) && !(editing?.protocol === p && editing.node_ids?.includes(nd.id));
                          return (
                            <button
                              key={nd.id}
                              type="button"
                              role="checkbox"
                              aria-checked={on}
                              disabled={old && !on}
                              title={old ? t('inb.nodeOldHint') : undefined}
                              className={`${s.nodeOpt} ${on ? s.nodeOn : ''}`}
                              onClick={() => patch({ node_ids: on ? d.node_ids.filter((x) => x !== nd.id) : [...d.node_ids, nd.id] })}
                            >
                              <span className={s.nodeCheck}>{on && <Check size={13} strokeWidth={3} />}</span>
                              <span className={s.nodeText}>
                                <strong>{nd.name}</strong>
                                <small className="ltr">{nd.address}</small>
                              </span>
                              {!nd.enabled && <span className="chip chip--disabled">{t('inb.nodeOff')}</span>}
                              {old && <span className="chip chip--disabled">{t('inb.nodeOld')}</span>}
                            </button>
                          );
                        })}
                      </div>
                      {err('nodes') && <span className="field__error">{err('nodes')}</span>}
                      {legacyBoth ? (
                        <div className={s.switchLine}>
                          <span>
                            {t('inb.alsoMain')}
                            <small className="field__hint" style={{ display: 'block' }}>
                              {t('inb.alsoMainHint')}
                            </small>
                          </span>
                          <Switch checked={d.serve_main} onChange={(v) => patch({ serve_main: v })} label={t('inb.alsoMain')} />
                        </div>
                      ) : (
                        <p className={s.infoNote}>{t('inb.nodesOnlyNote')}</p>
                      )}
                    </>
                  ) : (
                    <span className="field__hint">{t('inb.runsOnHint')}</span>
                  )}
                </div>
              )}

              {isV2(p) && (
                <div className={s.span2}>
                  <span className="field__label">{t('inb.step.transport')}</span>
                  <div className={`${s.tiles} ${s.tilesSm}`} role="radiogroup" aria-label={t('inb.step.transport')}>
                    {NETWORKS.map((net) => {
                      const disabled = p === 'shadowsocks' && net !== 'tcp';
                      return (
                        <button key={net} type="button" role="radio" aria-checked={d.network === net} disabled={disabled} className={`${s.tile} ${s.tileSm} ${d.network === net ? s.tileOn : ''}`} onClick={() => setNetwork(net)}>
                          <strong className="mono">{net}</strong>
                          <span>{t(`inb.net.${net}` as MessageKey)}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}

              {hasPort(p) ? (
                <>
                  <Field label={portLabel} error={err('port')}>
                    <span className={s.portField}>
                      <input className="input num ltr" type="number" inputMode="numeric" min={1} max={65535} value={d.port || ''} placeholder={portHint(p) ? t('inb.portPlaceholder', { port: portHint(p) }) : undefined} aria-required onChange={(e) => patch({ port: e.target.value === '' ? 0 : Number(e.target.value) })} aria-invalid={!!err('port')} />
                      {transportLabel(tr) && <span className={s.portTag}>{transportLabel(tr)}</span>}
                    </span>
                  </Field>
                  <Field label={t('inb.listen')} hint={t('inb.listenHint')} error={err('listen')} optional>
                    <input className="input ltr" value={d.listen} onChange={(e) => patch({ listen: e.target.value })} placeholder="0.0.0.0" aria-invalid={!!err('listen')} />
                  </Field>
                </>
              ) : (
                <p className={`${s.span2} ${s.infoNote}`}>{t('inb.noPortTun')}</p>
              )}

              {isV2(p) && (d.network === 'ws' || d.network === 'httpupgrade' || d.network === 'xhttp') && (
                <>
                  <Field label={t('inb.path')}>
                    <input className="input ltr mono" value={d.transport.path} onChange={(e) => patchT({ path: e.target.value })} />
                  </Field>
                  <Field label={t('inb.host')} optional>
                    <input className="input ltr" value={d.transport.host} onChange={(e) => patchT({ host: e.target.value })} />
                  </Field>
                </>
              )}
              {isV2(p) && d.network === 'grpc' && (
                <>
                  <Field label={t('inb.serviceName')}>
                    <input className="input ltr mono" value={d.transport.service_name} onChange={(e) => patchT({ service_name: e.target.value })} />
                  </Field>
                  <Field label={t('inb.mode')}>
                    <Select<string> ltr mono value={d.transport.mode || 'gun'} onChange={(v) => patchT({ mode: v })} ariaLabel={t('inb.mode')} options={['gun', 'multi'].map((m) => ({ value: m, label: m }))} />
                  </Field>
                </>
              )}
              {isV2(p) && d.network === 'xhttp' && (
                <Field label={t('inb.mode')}>
                  <Select<string>
                    ltr
                    mono
                    value={d.transport.mode || 'auto'}
                    onChange={(v) => patchT({ mode: v })}
                    ariaLabel={t('inb.mode')}
                    options={['auto', 'packet-up', 'stream-up', 'stream-one'].map((m) => ({ value: m, label: m }))}
                  />
                </Field>
              )}
              {p === 'shadowsocks' && (
                <Field label={t('inb.ssMethod')}>
                  <Select<string> ltr mono value={d.ss_method} onChange={(v) => patch({ ss_method: v })} ariaLabel={t('inb.ssMethod')} options={SS_METHODS.map((m) => ({ value: m, label: m }))} />
                </Field>
              )}

              {p === 'hysteria' && (
                <>
                  <div className={s.span2}>
                    <Field label={t('inb.obfsPassword')} hint={t('inb.obfsHint')} optional>
                      <input className="input ltr mono" value={d.settings.obfs_password} onChange={(e) => patchS({ obfs_password: e.target.value })} autoComplete="off" spellCheck={false} />
                    </Field>
                  </div>
                  <Field label={t('inb.upMbps')} hint={t('inb.mbpsHint')} error={err('up')}>
                    <NumInput value={d.settings.up_mbps} onChange={(v) => patchS({ up_mbps: v })} min={0} max={100000} invalid={!!err('up')} />
                  </Field>
                  <Field label={t('inb.downMbps')} hint={t('inb.mbpsHint')} error={err('down')}>
                    <NumInput value={d.settings.down_mbps} onChange={(v) => patchS({ down_mbps: v })} min={0} max={100000} invalid={!!err('down')} />
                  </Field>
                </>
              )}
              {p === 'tuic' && (
                <Field label={t('inb.congestion')}>
                  <Select<string>
                    ltr
                    mono
                    value={d.settings.congestion || 'bbr'}
                    onChange={(v) => patchS({ congestion: v as InboundSettings['congestion'] })}
                    ariaLabel={t('inb.congestion')}
                    options={['bbr', 'cubic', 'new_reno'].map((m) => ({ value: m, label: m }))}
                  />
                </Field>
              )}
              {p === 'masque' && (
                <>
                  <div className={s.span2}>
                    <Field label={t('inb.pool')} hint={t('inb.poolHint')} error={err('pool')}>
                      <CsvInput value={d.settings.pool} onChange={(v) => patchS({ pool: v })} placeholder="10.14.0.1/24, fd14::1/64" invalid={!!err('pool')} />
                    </Field>
                  </div>
                  <Field label={t('inb.mtu')} error={err('mtu')}>
                    <NumInput value={d.settings.mtu} onChange={(v) => patchS({ mtu: v })} min={1280} max={65535} invalid={!!err('mtu')} />
                  </Field>
                  <Field label={t('inb.masquePath')} error={err('path')}>
                    <input className="input ltr mono" value={d.settings.path} onChange={(e) => patchS({ path: e.target.value })} placeholder={MASQUE_PATH} />
                  </Field>
                </>
              )}
              {isWG(p) && (
                <>
                  <Field label={t('inb.wgAddress')} hint={t('inb.wgAddressHint')} error={err('address')}>
                    <input className="input ltr mono" value={d.settings.address} onChange={(e) => patchS({ address: e.target.value })} placeholder="10.66.66.1/24" aria-invalid={!!err('address')} />
                  </Field>
                  <Field label={t('inb.dns')} hint={t('inb.dnsHint')} error={err('dns')}>
                    <CsvInput value={d.settings.dns} onChange={(v) => patchS({ dns: v })} placeholder="1.1.1.1, 8.8.8.8" invalid={!!err('dns')} />
                  </Field>
                  <Field label={t('inb.mtu')} error={err('mtu')}>
                    <NumInput value={d.settings.mtu} onChange={(v) => patchS({ mtu: v })} min={1280} max={1500} invalid={!!err('mtu')} />
                  </Field>
                  <Field label={t('inb.keepalive')} hint={t('inb.keepaliveHint')} error={err('keepalive')}>
                    <NumInput value={d.settings.keepalive} onChange={(v) => patchS({ keepalive: v })} min={0} max={600} invalid={!!err('keepalive')} />
                  </Field>
                </>
              )}
              {p === 'tun' && (
                <>
                  <Field label={t('inb.tunName')} hint={t('inb.tunNameHint')} error={err('tunName')}>
                    <input className="input ltr mono" value={d.settings.tun_name} onChange={(e) => patchS({ tun_name: e.target.value })} maxLength={15} aria-invalid={!!err('tunName')} />
                  </Field>
                  <Field label={t('inb.mtu')} error={err('mtu')}>
                    <NumInput value={d.settings.mtu} onChange={(v) => patchS({ mtu: v })} min={576} max={9000} invalid={!!err('mtu')} />
                  </Field>
                  <div className={s.span2}>
                    <Field label={t('inb.gateway')} hint={t('inb.gatewayHint')} error={err('gateway')}>
                      <CsvInput value={d.settings.gateway} onChange={(v) => patchS({ gateway: v })} placeholder="10.77.0.1/24" invalid={!!err('gateway')} />
                    </Field>
                  </div>
                  <div className={s.span2}>
                    <Field label={t('inb.routes')} hint={t('inb.routesHint')} error={err('routes')} optional>
                      <CsvInput value={d.settings.routes} onChange={(v) => patchS({ routes: v })} placeholder="10.10.0.0/16, 192.168.50.0/24" invalid={!!err('routes')} />
                    </Field>
                  </div>
                </>
              )}
              {p === 'mixed' && (
                <div className={`${s.span2} ${s.switchLine}`}>
                  <span>{t('inb.mixedUdp')}</span>
                  <Switch checked={d.settings.udp} onChange={(v) => patchS({ udp: v })} label={t('inb.mixedUdp')} />
                </div>
              )}
              {p === 'tunnel' && (
                <>
                  <Field label={t('inb.targetAddress')} error={err('target')}>
                    <input className="input ltr" value={d.settings.target_address} onChange={(e) => patchS({ target_address: e.target.value })} placeholder="127.0.0.1" aria-invalid={!!err('target')} />
                  </Field>
                  <Field label={t('inb.targetPort')} error={err('targetPort')}>
                    <NumInput value={d.settings.target_port} onChange={(v) => patchS({ target_port: v })} min={1} max={65535} placeholder="22" invalid={!!err('targetPort')} />
                  </Field>
                  <Field label={t('inb.tunnelNetwork')}>
                    <Select<string>
                      ltr
                      mono
                      value={d.settings.tunnel_network || 'tcp'}
                      onChange={(v) => patchS({ tunnel_network: v as InboundSettings['tunnel_network'] })}
                      ariaLabel={t('inb.tunnelNetwork')}
                      options={[
                        { value: 'tcp', label: 'TCP' },
                        { value: 'udp', label: 'UDP' },
                        { value: 'tcp,udp', label: 'TCP + UDP' },
                      ]}
                    />
                  </Field>
                </>
              )}
              {p === 'mtproto' && (
                <div className={s.span2}>
                  <Field label={t('inb.fakeTls')} hint={t('inb.fakeTlsHint')} error={err('faketls')}>
                    <input className="input ltr" value={d.settings.faketls_domain} onChange={(e) => patchS({ faketls_domain: e.target.value })} placeholder="www.cloudflare.com" aria-invalid={!!err('faketls')} />
                  </Field>
                </div>
              )}

              {hasUsers(p) && (
                <div className={s.span2}>
                  <Field label={t('inb.addressOverride')} hint={t('inb.addressOverrideHint')} optional>
                    <input className="input ltr" value={d.address_override} onChange={(e) => patch({ address_override: e.target.value })} />
                  </Field>
                </div>
              )}
              {!hasUsers(p) && <p className={`${s.span2} ${s.note}`}>{t('inb.noLinksNote')}</p>}
              {sniffingApplies(p) && (
                <div className={`${s.span2} ${s.switchLine}`}>
                  <span>{t('inb.sniffing')}</span>
                  <Switch checked={d.sniffing} onChange={(v) => patch({ sniffing: v })} label={t('inb.sniffing')} />
                </div>
              )}
              {needsNetAdmin(p) && (
                <p className={`${s.span2} ${s.warnNote}`}>
                  <AlertTriangle size={16} aria-hidden="true" />
                  <span>{t('inb.netAdminNote')}</span>
                </p>
              )}
            </div>
          )}

          {stepId === 'security' && isV2(p) && (
            <div className={s.formGrid}>
              <div className={s.span2}>
                <div className={`${s.tiles} ${s.tilesSec}`} role="radiogroup" aria-label={t('inb.step.security')}>
                  {SECURITIES.map((sec) => {
                    const disabled = (sec === 'reality' && !realityAllowed(d)) || (p === 'shadowsocks' && sec !== 'none');
                    const Icon = sec === 'none' ? ShieldOff : sec === 'tls' ? Lock : Sparkles;
                    return (
                      <button key={sec} type="button" role="radio" aria-checked={d.security === sec} disabled={disabled} className={`${s.tile} ${s.tileSm} ${d.security === sec ? s.tileOn : ''}`} onClick={() => setSecurity(sec)}>
                        <Icon size={22} />
                        <strong className="display">{sec === 'none' ? t('common.none') : sec === 'tls' ? 'TLS' : 'Reality'}</strong>
                        <span>{t(`inb.sec.${sec}`)}</span>
                      </button>
                    );
                  })}
                </div>
                {p === 'shadowsocks' ? <p className={s.note}>{t('inb.ssNoTls')}</p> : !realityAllowed(d) && <p className={s.note}>{t('inb.realityOnly')}</p>}
              </div>

              {d.security === 'tls' && d.tls && (
                <>
                  {certButtons}
                  <Field label={t('inb.sni')}>
                    <input className="input ltr" value={d.tls.sni} onChange={(e) => patchTls({ sni: e.target.value })} />
                  </Field>
                  <Field label={t('inb.fingerprint')}>
                    <Select<string> ltr mono value={d.tls.fingerprint || 'chrome'} onChange={(v) => patchTls({ fingerprint: v })} ariaLabel={t('inb.fingerprint')} options={FPS.map((f) => ({ value: f, label: f }))} />
                  </Field>
                  <div className={s.span2}>
                    <span className="field__label">{t('inb.alpn')}</span>
                    <div className={s.chipRow}>
                      {ALPNS.map((a) => {
                        const on = d.tls!.alpn.includes(a);
                        return (
                          <button key={a} type="button" className="pill mono" aria-pressed={on} onClick={() => patchTls({ alpn: on ? d.tls!.alpn.filter((x) => x !== a) : [...d.tls!.alpn, a] })}>
                            {a}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                  <div className={`${s.span2} ${s.switchLine}`}>
                    <span>{t('inb.allowInsecure')}</span>
                    <Switch checked={d.tls.allow_insecure} onChange={(v) => patchTls({ allow_insecure: v })} label={t('inb.allowInsecure')} />
                  </div>
                </>
              )}

              {d.security === 'reality' && d.reality && (
                <>
                  <Field label={t('inb.dest')} error={err('dest')}>
                    <input className="input ltr" value={d.reality.dest} onChange={(e) => patchReality({ dest: e.target.value })} />
                  </Field>
                  <Field label={t('inb.serverNames')} hint={t('inb.serverNamesHint')} error={err('serverNames')}>
                    <CsvInput value={d.reality.server_names} onChange={(v) => patchReality({ server_names: v })} invalid={!!err('serverNames')} />
                  </Field>
                  <div className={`${s.span2} ${s.keyBox}`}>
                    <div className={s.keyHead}>
                      <span className="field__label">
                        <KeyRound size={15} /> {t('inb.privateKey')} / {t('inb.publicKey')}
                      </span>
                      <button type="button" className="btn btn--sm btn--primary" onClick={genReality} disabled={genBusy}>
                        {genBusy ? <Loader2 size={15} className="spin" /> : <Sparkles size={15} />}
                        {t('inb.genKeys')}
                      </button>
                    </div>
                    <div className={s.keys}>
                      <label>
                        <small>{t('inb.privateKey')}</small>
                        <code className="mono ltr">{d.reality.private_key || '—'}</code>
                      </label>
                      <label>
                        <small>{t('inb.publicKey')}</small>
                        <code className="mono ltr">{d.reality.public_key || '—'}</code>
                      </label>
                      <label>
                        <small>{t('inb.shortIds')}</small>
                        <code className="mono ltr">{d.reality.short_ids.join(', ') || '—'}</code>
                      </label>
                    </div>
                    {!d.reality.private_key && <span className="field__hint">{t('inb.keysAuto')}</span>}
                  </div>
                  <Field label={t('inb.fingerprint')}>
                    <Select<string> ltr mono value={REALITY_FPS.includes(d.reality.fingerprint) ? d.reality.fingerprint : 'chrome'} onChange={(v) => patchReality({ fingerprint: v })} ariaLabel={t('inb.fingerprint')} options={REALITY_FPS.map((f) => ({ value: f, label: f }))} />
                  </Field>
                  <Field label={t('inb.spiderX')}>
                    <input className="input ltr mono" value={d.reality.spider_x} onChange={(e) => patchReality({ spider_x: e.target.value })} />
                  </Field>
                </>
              )}

              {flowAllowed && (
                <Field label={t('inb.flow')}>
                  <Select<string>
                    ltr
                    mono
                    value={d.flow}
                    onChange={(v) => patch({ flow: v as InboundInput['flow'] })}
                    ariaLabel={t('inb.flow')}
                    options={[
                      { value: '', label: 'none' },
                      { value: 'xtls-rprx-vision', label: 'xtls-rprx-vision' },
                    ]}
                  />
                </Field>
              )}
            </div>
          )}

          {stepId === 'security' && p === 'http' && (
            <div className={s.formGrid}>
              <div className={s.span2}>
                <div className={`${s.tiles} ${s.tilesSec2}`} role="radiogroup" aria-label={t('inb.step.security')}>
                  {(['none', 'tls'] as Security[]).map((sec) => {
                    const Icon = sec === 'none' ? ShieldOff : Lock;
                    return (
                      <button key={sec} type="button" role="radio" aria-checked={d.security === sec} className={`${s.tile} ${s.tileSm} ${d.security === sec ? s.tileOn : ''}`} onClick={() => setSecurity(sec)}>
                        <Icon size={22} />
                        <strong className="display">{sec === 'none' ? 'HTTP' : 'HTTPS'}</strong>
                        <span>{t(`inb.sec.${sec}`)}</span>
                      </button>
                    );
                  })}
                </div>
                <p className={s.note}>{t('inb.httpSec')}</p>
              </div>
              {d.security === 'tls' && d.tls && (
                <>
                  {certButtons}
                  <div className={s.span2}>
                    <Field label={t('inb.sni')} optional>
                      <input className="input ltr" value={d.tls.sni} onChange={(e) => patchTls({ sni: e.target.value })} />
                    </Field>
                  </div>
                </>
              )}
            </div>
          )}

          {stepId === 'security' && isQuic(p) && (
            <div className={s.formGrid}>
              <p className={`${s.span2} ${s.infoNote}`}>
                <ShieldCheck size={16} aria-hidden="true" />
                <span>{t('inb.quicTlsNote')}</span>
              </p>
              <div className={s.span2}>
                <Field label={t('inb.quicCert')} hint={t('inb.quicCertHint')}>
                  <Select<number>
                    value={d.tls?.cert_id ?? 0}
                    onChange={(v) => {
                      const c = certs.data?.find((x) => x.id === v);
                      patchTls({ cert_id: v || null, sni: c ? d.tls?.sni || c.domain : d.tls?.sni ?? '', allow_insecure: v ? d.tls?.allow_insecure ?? false : false });
                    }}
                    ariaLabel={t('inb.quicCert')}
                    options={[
                      { value: 0, label: t('inb.selfSignedAuto') },
                      ...(certs.data ?? []).map((c) => ({ value: c.id, label: <span className="ltr">{c.domain}</span>, text: c.domain, hint: t(`certs.source.${c.source}` as MessageKey) })),
                    ]}
                  />
                </Field>
              </div>
              <Field label={t('inb.sni')} hint={d.tls?.cert_id ? undefined : t('inb.sniDefault')} optional>
                <input className="input ltr" value={d.tls?.sni ?? ''} onChange={(e) => patchTls({ sni: e.target.value })} placeholder={d.tls?.cert_id ? '' : 'www.bing.com'} />
              </Field>
              <Field label={t('inb.fingerprint')}>
                <Select<string> ltr mono value={d.tls?.fingerprint || 'chrome'} onChange={(v) => patchTls({ fingerprint: v })} ariaLabel={t('inb.fingerprint')} options={FPS.map((f) => ({ value: f, label: f }))} />
              </Field>
              {p === 'masque' && (
                <div className={s.span2}>
                  <span className="field__label">{t('inb.alpn')}</span>
                  <div className={s.chipRow}>
                    {['h3', 'h2'].map((a) => {
                      const cur = d.tls?.alpn ?? [];
                      const on = cur.includes(a);
                      return (
                        <button key={a} type="button" className="pill mono" aria-pressed={on} onClick={() => patchTls({ alpn: on ? cur.filter((x) => x !== a) : [...cur, a] })}>
                          {a}
                        </button>
                      );
                    })}
                  </div>
                  {err('alpn') ? <span className="field__error">{err('alpn')}</span> : <span className="field__hint">{t('inb.masqueAlpn')}</span>}
                </div>
              )}
              {!!d.tls?.cert_id && (
                <div className={`${s.span2} ${s.switchLine}`}>
                  <span>{t('inb.allowInsecure')}</span>
                  <Switch checked={!!d.tls?.allow_insecure} onChange={(v) => patchTls({ allow_insecure: v })} label={t('inb.allowInsecure')} />
                </div>
              )}
            </div>
          )}

          {stepId === 'keys' && (
            <div className={s.formGrid}>
              <div className={`${s.span2} ${s.keyBox}`}>
                <div className={s.keyHead}>
                  <span className="field__label">
                    <KeyRound size={15} /> {t('inb.privateKey')} / {t('inb.publicKey')}
                  </span>
                  <button type="button" className="btn btn--sm btn--primary" onClick={() => genWg(false)} disabled={genBusy}>
                    {genBusy ? <Loader2 size={15} className="spin" /> : <Sparkles size={15} />}
                    {t('inb.genWgKeys')}
                  </button>
                </div>
                <div className={s.keys}>
                  <label>
                    <small>{t('inb.privateKey')}</small>
                    <input
                      className="input ltr mono"
                      value={d.settings.private_key}
                      onChange={(e) => patchS({ private_key: e.target.value.trim(), public_key: '' })}
                      placeholder="auto"
                      spellCheck={false}
                      autoComplete="off"
                    />
                  </label>
                  <label>
                    <small>{t('inb.publicKey')}</small>
                    <code className="mono ltr">{d.settings.public_key || '—'}</code>
                  </label>
                </div>
                <span className="field__hint">{d.settings.private_key ? t('inb.wgKeysHint') : `${t('inb.keysAuto')} ${t('inb.wgKeysHint')}`}</span>
              </div>
              {p === 'amneziawg' && (
                <div className={`${s.span2} ${s.keyBox}`}>
                  <div className={s.keyHead}>
                    <span className="field__label">
                      <Shuffle size={15} /> {t('inb.awgParams')}
                    </span>
                    <span className={s.keyBtns}>
                      {d.settings.awg && (
                        <button type="button" className="btn btn--sm" onClick={() => patchS({ awg: null })}>
                          {t('inb.awgClear')}
                        </button>
                      )}
                      <button type="button" className="btn btn--sm btn--primary" onClick={() => genWg(true)} disabled={genBusy}>
                        {genBusy ? <Loader2 size={15} className="spin" /> : <Shuffle size={15} />}
                        {t('inb.awgGen')}
                      </button>
                    </span>
                  </div>
                  {d.settings.awg ? (
                    <div className={s.awgGrid}>
                      {(['jc', 'jmin', 'jmax', 's1', 's2', 'h1', 'h2', 'h3', 'h4'] as (keyof AwgParams)[]).map((k) => (
                        <label key={k} className={s.awgCell}>
                          <small className="mono">{k.toUpperCase()}</small>
                          <NumInput value={d.settings.awg![k]} onChange={(v) => patchAwg({ [k]: v })} min={0} invalid={!!err('awg')} />
                        </label>
                      ))}
                    </div>
                  ) : (
                    <span className="field__hint">{t('inb.awgAuto')}</span>
                  )}
                  {err('awg') ? <span className="field__error">{err('awg')}</span> : d.settings.awg && <span className="field__hint">{t('inb.awgRule')}</span>}
                </div>
              )}
            </div>
          )}

          {stepId === 'review' && (
            <div className={s.review}>
              <div className={s.reviewHero}>
                <ProtoGlyph protocol={p} size={64} />
                <div>
                  <strong className="display" dir="auto">
                    {d.remark}
                  </strong>
                  <span className="mono ltr">{p === 'tun' ? `tun://${d.settings.tun_name}` : `${protoName(p).toLowerCase()}://${d.listen || '0.0.0.0'}:${d.port}`}</span>
                </div>
              </div>
              <div className={s.flowLine} aria-hidden="true">
                {flowNodes.map((x, i) => (
                  <span key={i} className={s.flowNode}>
                    {x}
                  </span>
                ))}
              </div>
              <dl className={s.reviewList}>
                {reviewRows.map(([k, v]) => (
                  <div key={k}>
                    <dt>{k}</dt>
                    <dd className="mono ltr">{v}</dd>
                  </div>
                ))}
              </dl>
              {needsNetAdmin(p) && (
                <p className={s.warnNote}>
                  <AlertTriangle size={16} aria-hidden="true" />
                  <span>{t('inb.netAdminNote')}</span>
                </p>
              )}
              <p className={s.note}>{t('inb.review.hint')}</p>
              {serverErr && (
                <p className="field__error" role="alert">
                  {serverErr}
                </p>
              )}
            </div>
          )}
        </div>
    </Sheet>
  );
}
