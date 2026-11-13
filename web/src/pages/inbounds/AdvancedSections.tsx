import { ChevronDown, Download, KeyRound, Loader2, Plus, RefreshCw, SlidersHorizontal, Sparkles, Trash2 } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { genEchKeys, genMldsa65Keys, getReverseBridge } from '@/api/hooks';
import type { AdvFallback, AdvLimit, Inbound, InboundAdvanced, InboundInput } from '@/api/types';
import { CopyButton, Field, Switch } from '@/components/misc';
import { Select } from '@/components/Select';
import { useI18n, type MessageKey } from '@/i18n';
import { KCP_HEADERS, SNIFF_DESTS, TCP_CONGESTIONS, TLS_VERSIONS, advancedCount, applies, fallbackDestOk, headerNameOk, rangeOk } from '@/lib/advanced';
import { toast } from '@/store/feedback';
import s from './Inbounds.module.css';

type SetAdv = (fn: (a: InboundAdvanced) => InboundAdvanced) => void;
type T = (k: MessageKey, v?: Record<string, string | number>) => string;

/* ---------- small inputs ---------- */
function Num({ value, onChange, placeholder, invalid }: { value: number | undefined; onChange: (n: number) => void; placeholder?: string; invalid?: boolean }) {
  return (
    <input
      className="input num ltr"
      type="number"
      inputMode="numeric"
      min={0}
      value={value ? value : ''}
      placeholder={placeholder}
      aria-invalid={invalid || undefined}
      onChange={(e) => onChange(e.target.value === '' ? 0 : Math.max(0, Math.floor(Number(e.target.value))))}
    />
  );
}
function Txt({ value, onChange, placeholder, invalid, mono = true }: { value: string | undefined; onChange: (v: string) => void; placeholder?: string; invalid?: boolean; mono?: boolean }) {
  return (
    <input
      className={`input ltr ${mono ? 'mono' : ''}`}
      value={value ?? ''}
      placeholder={placeholder}
      aria-invalid={invalid || undefined}
      spellCheck={false}
      autoCapitalize="none"
      autoComplete="off"
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
function Csv({ value, onChange, placeholder }: { value: string[] | undefined; onChange: (v: string[]) => void; placeholder?: string }) {
  const [text, setText] = useState((value ?? []).join(', '));
  useEffect(() => {
    const parsed = text.split(',').map((x) => x.trim()).filter(Boolean);
    if (parsed.join(',') !== (value ?? []).join(',')) setText((value ?? []).join(', '));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  return (
    <input
      className="input ltr mono"
      value={text}
      placeholder={placeholder}
      spellCheck={false}
      autoCapitalize="none"
      onChange={(e) => {
        setText(e.target.value);
        onChange(e.target.value.split(',').map((x) => x.trim()).filter(Boolean));
      }}
    />
  );
}
function Toggle({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className={`${s.span2} ${s.switchLine}`}>
      <span>
        {label}
        {hint && (
          <small className="field__hint" style={{ display: 'block' }}>
            {hint}
          </small>
        )}
      </span>
      <Switch checked={checked} onChange={onChange} label={label} />
    </div>
  );
}
function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <h4 className={`${s.span2} ${s.advGroup}`}>{title}</h4>
      {children}
    </>
  );
}
/** Key/value list of HTTP headers. */
function Headers({ value, onChange, t }: { value: Record<string, string> | undefined; onChange: (v: Record<string, string>) => void; t: T }) {
  const [rows, setRows] = useState<[string, string][]>(() => Object.entries(value ?? {}));
  const push = (r: [string, string][]) => {
    setRows(r);
    const o: Record<string, string> = {};
    for (const [k, v] of r) if (k.trim()) o[k.trim()] = v;
    onChange(o);
  };
  return (
    <div className={`${s.span2} ${s.advList}`}>
      <span className="field__label">{t('adv.headers')}</span>
      {rows.map(([k, v], i) => (
        <div key={i} className={s.advRow}>
          <input className="input ltr mono" value={k} placeholder="X-Header" aria-invalid={(!!k && !headerNameOk(k)) || undefined} onChange={(e) => push(rows.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
          <input className="input ltr mono" value={v} placeholder={t('adv.headerValue')} onChange={(e) => push(rows.map((r, j) => (j === i ? [r[0], e.target.value] : r)))} />
          <button type="button" className="btn btn--sm btn--ghost" aria-label={t('common.delete')} onClick={() => push(rows.filter((_, j) => j !== i))}>
            <Trash2 size={15} />
          </button>
        </div>
      ))}
      <button type="button" className="btn btn--sm" onClick={() => push([...rows, ['', '']])}>
        <Plus size={15} /> {t('adv.addHeader')}
      </button>
    </div>
  );
}

/** The collapsible "Advanced" block: closed by default, open when something is set. */
export function AdvancedBlock({ id, count, children }: { id: string; count: number; children: ReactNode }) {
  const { t, n } = useI18n();
  const [open, setOpen] = useState(count > 0);
  return (
    <section className={`${s.span2} ${s.adv}`} data-section={`advanced-${id}`}>
      <button type="button" className={s.advHead} aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <SlidersHorizontal size={16} aria-hidden="true" />
        <span className={s.advTitle}>
          {t('adv.title')}
          <small>{t('adv.hint')}</small>
        </span>
        {count > 0 && <span className="chip">{n(count)}</span>}
        <ChevronDown size={18} className={s.advChevron} aria-hidden="true" />
      </button>
      {open && <div className={`${s.formGrid} ${s.advBody}`}>{children}</div>}
    </section>
  );
}

/* ---------- validation (mirrors the server) ---------- */
export function advErrors(d: InboundInput, step: 'connection' | 'security', t: T): Record<string, string> {
  const e: Record<string, string> = {};
  const a = d.advanced;
  if (!a) return e;
  const ok = applies(d);
  const range = t('adv.rangeHint');
  if (step === 'connection') {
    if (ok.xhttp && a.xhttp) {
      const x = a.xhttp;
      if (!rangeOk(x.x_padding_bytes, 0, 1 << 20)) e['adv.padding'] = range;
      if (!rangeOk(x.sc_max_each_post_bytes, 1, 1 << 26)) e['adv.maxPost'] = range;
      if (!rangeOk(x.sc_stream_up_server_secs, 1, 86400)) e['adv.streamUpSecs'] = range;
      const m = x.xmux;
      if (m && [m.max_concurrency, m.max_connections, m.c_max_reuse_times, m.h_max_request_times, m.h_max_reusable_secs].some((v) => !rangeOk(v))) e['adv.xmux'] = range;
      if (m?.max_concurrency && m?.max_connections) e['adv.xmux'] = t('adv.xmuxBoth');
      if (x.download?.address?.trim() && d.transport.mode === 'stream-one') e['adv.download'] = t('adv.downloadMode');
    }
    if (ok.ws && a.ws?.headers && Object.entries(a.ws.headers).some(([k, v]) => !headerNameOk(k) || /[\r\n]/.test(v))) e['adv.headers'] = t('adv.headerBad');
    if (ok.kcp && a.kcp) {
      const k = a.kcp;
      if (k.mtu && (k.mtu < 576 || k.mtu > 1460)) e['adv.kcpMtu'] = t('inb.range', { min: 576, max: 1460 });
      if (k.tti && (k.tti < 10 || k.tti > 100)) e['adv.kcpTti'] = t('inb.range', { min: 10, max: 100 });
    }
    const f = a.masks?.fragment;
    if (ok.fragment && f && (f.length || f.delay || f.packets)) {
      if (!f.length || !rangeOk(f.length, 1, 16384)) e['adv.fragment'] = t('adv.fragmentHint');
      if (!rangeOk(f.delay, 0, 10000) || (f.packets && f.packets !== 'tlshello' && !rangeOk(f.packets, 1, 100))) e['adv.fragment'] = t('adv.fragmentHint');
    }
    const nz = a.masks?.noise;
    if (ok.noise && nz && (!rangeOk(nz.rand, 1, 1400) || !rangeOk(nz.delay, 0, 10000))) e['adv.noise'] = range;
    const h = a.hysteria;
    if (ok.hysteria && h) {
      if (h.congestion === 'force-brutal' && !h.brutal_up_mbps) e['adv.brutal'] = t('adv.brutalNeedsUp');
      if (h.udp_idle_timeout && (h.udp_idle_timeout < 2 || h.udp_idle_timeout > 600)) e['adv.udpIdle'] = t('inb.range', { min: 2, max: 600 });
      if (h.masquerade?.type === 'proxy' && !/^https?:\/\/\S+$/.test(h.masquerade.url ?? '')) e['adv.masqUrl'] = t('adv.masqUrlBad');
    }
  } else {
    if (ok.fallbacks && a.fallbacks?.some((f) => !fallbackDestOk(f.dest) || (!!f.path && !f.path.startsWith('/')))) e['adv.fallbacks'] = t('adv.fallbackBad');
    if (ok.tls && a.tls?.min_version && a.tls.max_version && a.tls.min_version > a.tls.max_version) e['adv.tlsVer'] = t('adv.tlsVerBad');
    if (ok.tls && a.tls?.cipher_suites && !/^[A-Z0-9_]+(:[A-Z0-9_]+)*$/.test(a.tls.cipher_suites.replace(/\s/g, ''))) e['adv.ciphers'] = t('adv.ciphersBad');
    const r = a.reality;
    if (ok.reality && r) {
      for (const v of [r.min_client_ver, r.max_client_ver]) if (v && !/^\d{1,3}(\.\d{1,3}){0,2}$/.test(v)) e['adv.clientVer'] = t('adv.clientVerBad');
    }
  }
  return e;
}

/* ---------- connection step ---------- */
export function ConnectionAdvanced({ d, setAdv, err, inbounds, editing }: { d: InboundInput; setAdv: SetAdv; err: (k: string) => string | undefined; inbounds: Inbound[]; editing: Inbound | null }) {
  const { t } = useI18n();
  const a = d.advanced ?? {};
  const ok = applies(d);
  const patch = <K extends keyof InboundAdvanced>(k: K, v: Partial<NonNullable<InboundAdvanced[K]>>) => setAdv((x) => ({ ...x, [k]: { ...((x[k] as object) ?? {}), ...v } }));
  const sn = a.sniffing ?? {};
  const dests = sn.dest_override?.length ? sn.dest_override : ['http', 'tls', 'quic'];
  const so = a.sockopt ?? {};
  const x = a.xhttp ?? {};
  const xm = x.xmux ?? {};
  const dl = x.download ?? { address: '' };
  const k = a.kcp ?? {};
  const hy = a.hysteria ?? {};
  const tu = a.tuic ?? {};
  const g = a.grpc ?? {};
  const rv = a.reverse ?? { enabled: false, uuid: '', route_self: true };
  const fr = a.masks?.fragment ?? { packets: '', length: '' };
  const nz = a.masks?.noise ?? { rand: '' };
  const [busy, setBusy] = useState(false);
  const others = inbounds.filter((i) => i.id !== editing?.id && i.protocol !== 'tun' && i.protocol !== 'tunnel');

  const downloadBridge = async () => {
    if (!editing) return;
    setBusy(true);
    try {
      const cfg = await getReverseBridge(editing.id);
      const blob = new Blob([JSON.stringify(cfg, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const el = document.createElement('a');
      el.href = url;
      el.download = `veilon-bridge-${editing.id}.json`;
      el.click();
      setTimeout(() => URL.revokeObjectURL(url), 2000);
    } catch (e) {
      toast.error(t('common.error'), (e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const items: ReactNode[] = [];
  if (ok.sniffing && (d.protocol !== 'tuic' && d.protocol !== 'masque')) {
    items.push(
      <Group key="sn" title={t('adv.sniffing')}>
        <div className={s.span2}>
          <span className="field__label">{t('adv.sniffDest')}</span>
          <div className={s.chipRow}>
            {SNIFF_DESTS.map((x0) => {
              const on = dests.includes(x0);
              return (
                <button
                  key={x0}
                  type="button"
                  className="pill mono"
                  aria-pressed={on}
                  onClick={() => patch('sniffing', { dest_override: on ? dests.filter((y) => y !== x0) : [...dests, x0] })}
                >
                  {x0}
                </button>
              );
            })}
          </div>
        </div>
        <Toggle label={t('adv.routeOnly')} hint={t('adv.routeOnlyHint')} checked={!!sn.route_only} onChange={(v) => patch('sniffing', { route_only: v })} />
        <Toggle label={t('adv.metadataOnly')} hint={t('adv.metadataOnlyHint')} checked={!!sn.metadata_only} onChange={(v) => patch('sniffing', { metadata_only: v, dest_override: v ? ['fakedns'] : undefined })} />
        <div className={s.span2}>
          <Field label={t('adv.domainsExcluded')} hint={t('adv.domainsExcludedHint')} optional>
            <Csv value={sn.domains_excluded} onChange={(v) => patch('sniffing', { domains_excluded: v })} placeholder="courier.push.apple.com" />
          </Field>
        </div>
      </Group>,
    );
  }
  if (ok.tcpHeader) {
    const on = a.tcp?.type === 'http';
    items.push(
      <Group key="tcp" title={t('adv.httpHeader')}>
        <Toggle label={t('adv.httpHeaderOn')} hint={t('adv.httpHeaderHint')} checked={on} onChange={(v) => setAdv((y) => ({ ...y, tcp: v ? { type: 'http', host: y.tcp?.host ?? [], path: y.tcp?.path ?? [] } : undefined }))} />
        {on && (
          <>
            <Field label={t('adv.headerHosts')} optional>
              <Csv value={a.tcp?.host} onChange={(v) => patch('tcp', { type: 'http', host: v })} placeholder="www.bing.com" />
            </Field>
            <Field label={t('adv.headerPaths')} optional>
              <Csv value={a.tcp?.path} onChange={(v) => patch('tcp', { type: 'http', path: v })} placeholder="/" />
            </Field>
          </>
        )}
      </Group>,
    );
  }
  if (ok.ws) {
    items.push(
      <Group key="ws" title={d.network === 'ws' ? 'WebSocket' : 'HTTPUpgrade'}>
        <Headers value={a.ws?.headers} onChange={(v) => patch('ws', { headers: v })} t={t} />
        {err('adv.headers') && <span className={`${s.span2} field__error`}>{err('adv.headers')}</span>}
        {ok.heartbeat && (
          <Field label={t('adv.heartbeat')} hint={t('adv.heartbeatHint')} optional>
            <Num value={a.ws?.heartbeat_period} onChange={(v) => patch('ws', { heartbeat_period: v })} placeholder="0" />
          </Field>
        )}
      </Group>,
    );
  }
  if (ok.grpc) {
    items.push(
      <Group key="grpc" title="gRPC">
        <Field label={t('adv.userAgent')} optional>
          <Txt value={g.user_agent} onChange={(v) => patch('grpc', { user_agent: v })} mono={false} />
        </Field>
        <Field label={t('adv.idleTimeout')} hint={t('adv.seconds')} optional>
          <Num value={g.idle_timeout} onChange={(v) => patch('grpc', { idle_timeout: v })} />
        </Field>
        <Field label={t('adv.healthCheck')} hint={t('adv.seconds')} optional>
          <Num value={g.health_check_timeout} onChange={(v) => patch('grpc', { health_check_timeout: v })} />
        </Field>
        <Field label={t('adv.windowSize')} hint={t('adv.bytes')} optional>
          <Num value={g.initial_windows_size} onChange={(v) => patch('grpc', { initial_windows_size: v })} />
        </Field>
        <Toggle label={t('adv.permitNoStream')} checked={!!g.permit_without_stream} onChange={(v) => patch('grpc', { permit_without_stream: v })} />
      </Group>,
    );
  }
  if (ok.xhttp) {
    const pxm = (v: Partial<typeof xm>) => patch('xhttp', { xmux: { ...xm, ...v } });
    const pdl = (v: Partial<typeof dl>) => patch('xhttp', { download: { ...dl, ...v } });
    items.push(
      <Group key="xh" title="XHTTP">
        <Field label={t('adv.padding')} hint={t('adv.rangeHint')} error={err('adv.padding')} optional>
          <Txt value={x.x_padding_bytes} onChange={(v) => patch('xhttp', { x_padding_bytes: v })} placeholder="100-1000" invalid={!!err('adv.padding')} />
        </Field>
        <Field label={t('adv.maxPost')} hint={t('adv.rangeHint')} error={err('adv.maxPost')} optional>
          <Txt value={x.sc_max_each_post_bytes} onChange={(v) => patch('xhttp', { sc_max_each_post_bytes: v })} placeholder="1000000" invalid={!!err('adv.maxPost')} />
        </Field>
        <Field label={t('adv.bufferedPosts')} optional>
          <Num value={x.sc_max_buffered_posts} onChange={(v) => patch('xhttp', { sc_max_buffered_posts: v })} placeholder="30" />
        </Field>
        <Field label={t('adv.streamUpSecs')} hint={t('adv.rangeHint')} error={err('adv.streamUpSecs')} optional>
          <Txt value={x.sc_stream_up_server_secs} onChange={(v) => patch('xhttp', { sc_stream_up_server_secs: v })} placeholder="20-80" invalid={!!err('adv.streamUpSecs')} />
        </Field>
        <Toggle label={t('adv.noSSE')} hint={t('adv.noSSEHint')} checked={!!x.no_sse_header} onChange={(v) => patch('xhttp', { no_sse_header: v })} />
        <h5 className={`${s.span2} ${s.advSub}`}>{t('adv.xmux')}</h5>
        <Field label="maxConcurrency" optional>
          <Txt value={xm.max_concurrency} onChange={(v) => pxm({ max_concurrency: v })} placeholder="16-32" />
        </Field>
        <Field label="maxConnections" optional>
          <Txt value={xm.max_connections} onChange={(v) => pxm({ max_connections: v })} placeholder="0" />
        </Field>
        <Field label="cMaxReuseTimes" optional>
          <Txt value={xm.c_max_reuse_times} onChange={(v) => pxm({ c_max_reuse_times: v })} placeholder="0" />
        </Field>
        <Field label="hMaxRequestTimes" optional>
          <Txt value={xm.h_max_request_times} onChange={(v) => pxm({ h_max_request_times: v })} placeholder="600-900" />
        </Field>
        <Field label="hMaxReusableSecs" optional>
          <Txt value={xm.h_max_reusable_secs} onChange={(v) => pxm({ h_max_reusable_secs: v })} placeholder="1800-3000" />
        </Field>
        <Field label="hKeepAlivePeriod" optional>
          <Num value={xm.h_keep_alive_period} onChange={(v) => pxm({ h_keep_alive_period: v })} />
        </Field>
        {err('adv.xmux') && <span className={`${s.span2} field__error`}>{err('adv.xmux')}</span>}
        <p className={`${s.span2} ${s.note}`}>{t('adv.xmuxHint')}</p>
        <h5 className={`${s.span2} ${s.advSub}`}>{t('adv.download')}</h5>
        <Field label={t('adv.downloadAddress')} hint={t('adv.downloadHint')} error={err('adv.download')} optional>
          <Txt value={dl.address} onChange={(v) => pdl({ address: v })} placeholder="cdn.example.com" invalid={!!err('adv.download')} />
        </Field>
        <Field label={t('inb.port')} optional>
          <Num value={dl.port} onChange={(v) => pdl({ port: v })} placeholder={d.port ? String(d.port) : '443'} />
        </Field>
        <Field label="SNI" optional>
          <Txt value={dl.sni} onChange={(v) => pdl({ sni: v })} />
        </Field>
        <Field label={t('inb.host')} optional>
          <Txt value={dl.host} onChange={(v) => pdl({ host: v })} />
        </Field>
      </Group>,
    );
  }
  if (ok.kcp) {
    items.push(
      <Group key="kcp" title="mKCP">
        <Field label={t('adv.kcpHeader')}>
          <Select<string> ltr mono value={k.header || 'none'} onChange={(v) => patch('kcp', { header: v === 'none' ? '' : v })} ariaLabel={t('adv.kcpHeader')} options={KCP_HEADERS.map((h) => ({ value: h, label: h }))} />
        </Field>
        <Field label={t('adv.kcpSeed')} hint={t('adv.kcpSeedHint')} optional>
          <Txt value={k.seed} onChange={(v) => patch('kcp', { seed: v })} />
        </Field>
        {k.header === 'dns' && (
          <Field label={t('adv.kcpDomain')} optional>
            <Txt value={k.header_domain} onChange={(v) => patch('kcp', { header_domain: v })} placeholder="www.example.com" />
          </Field>
        )}
        <Field label="MTU" error={err('adv.kcpMtu')} optional>
          <Num value={k.mtu} onChange={(v) => patch('kcp', { mtu: v })} placeholder="1350" invalid={!!err('adv.kcpMtu')} />
        </Field>
        <Field label="TTI" hint={t('adv.ms')} error={err('adv.kcpTti')} optional>
          <Num value={k.tti} onChange={(v) => patch('kcp', { tti: v })} placeholder="50" invalid={!!err('adv.kcpTti')} />
        </Field>
        <Field label={t('adv.uplink')} hint="MB/s" optional>
          <Num value={k.uplink_capacity} onChange={(v) => patch('kcp', { uplink_capacity: v })} placeholder="5" />
        </Field>
        <Field label={t('adv.downlink')} hint="MB/s" optional>
          <Num value={k.downlink_capacity} onChange={(v) => patch('kcp', { downlink_capacity: v })} placeholder="20" />
        </Field>
        <Field label="cwndMultiplier" optional>
          <Num value={k.cwnd_multiplier} onChange={(v) => patch('kcp', { cwnd_multiplier: v })} placeholder="1" />
        </Field>
        <Field label="maxSendingWindow" hint={t('adv.bytes')} optional>
          <Num value={k.max_sending_window} onChange={(v) => patch('kcp', { max_sending_window: v })} />
        </Field>
      </Group>,
    );
  }
  if (ok.ss) {
    items.push(
      <Group key="ss" title="Shadowsocks">
        <Toggle label={t('adv.ssUdp')} hint={t('adv.ssUdpHint')} checked={!a.ss?.no_udp} onChange={(v) => setAdv((y) => ({ ...y, ss: v ? undefined : { no_udp: true } }))} />
      </Group>,
    );
  }
  if (ok.hysteria) {
    const mq = hy.masquerade;
    items.push(
      <Group key="hy" title="Hysteria 2">
        <Field label={t('adv.congestion')} hint={t(`adv.hyCong.${hy.congestion || 'auto'}` as MessageKey)}>
          <Select<string>
            ltr
            value={hy.congestion || 'auto'}
            onChange={(v) => patch('hysteria', { congestion: (v === 'auto' ? '' : v) as NonNullable<InboundAdvanced['hysteria']>['congestion'] })}
            ariaLabel={t('adv.congestion')}
            options={['auto', 'bbr', 'brutal', 'force-brutal'].map((c) => ({ value: c, label: c }))}
          />
        </Field>
        <Field label={t('adv.udpIdle')} hint={t('adv.seconds')} error={err('adv.udpIdle')} optional>
          <Num value={hy.udp_idle_timeout} onChange={(v) => patch('hysteria', { udp_idle_timeout: v })} placeholder="60" invalid={!!err('adv.udpIdle')} />
        </Field>
        {(hy.congestion === 'brutal' || hy.congestion === 'force-brutal') && (
          <>
            <Field label={t('adv.brutalUp')} hint="Mbps" error={err('adv.brutal')}>
              <Num value={hy.brutal_up_mbps} onChange={(v) => patch('hysteria', { brutal_up_mbps: v })} invalid={!!err('adv.brutal')} />
            </Field>
            <Field label={t('adv.brutalDown')} hint="Mbps" optional>
              <Num value={hy.brutal_down_mbps} onChange={(v) => patch('hysteria', { brutal_down_mbps: v })} />
            </Field>
          </>
        )}
        <Field label={t('adv.masquerade')} hint={t('adv.masqueradeHint')}>
          <Select<string>
            value={mq?.type ?? 'none'}
            onChange={(v) => patch('hysteria', { masquerade: v === 'none' ? null : { type: v as 'proxy' | 'string', url: mq?.url ?? '', content: mq?.content ?? '' } })}
            ariaLabel={t('adv.masquerade')}
            options={[
              { value: 'none', label: t('adv.masq.none') },
              { value: 'proxy', label: t('adv.masq.proxy') },
              { value: 'string', label: t('adv.masq.string') },
            ]}
          />
        </Field>
        {mq?.type === 'proxy' && (
          <>
            <Field label={t('adv.masqUrl')} error={err('adv.masqUrl')}>
              <Txt value={mq.url} onChange={(v) => patch('hysteria', { masquerade: { ...mq, url: v } })} placeholder="https://www.example.com" invalid={!!err('adv.masqUrl')} />
            </Field>
            <Toggle label={t('adv.rewriteHost')} checked={!!mq.rewrite_host} onChange={(v) => patch('hysteria', { masquerade: { ...mq, rewrite_host: v } })} />
          </>
        )}
        {mq?.type === 'string' && (
          <>
            <Field label={t('adv.masqContent')}>
              <Txt value={mq.content} onChange={(v) => patch('hysteria', { masquerade: { ...mq, content: v } })} placeholder="hello" mono={false} />
            </Field>
            <Field label={t('adv.statusCode')} optional>
              <Num value={mq.status_code} onChange={(v) => patch('hysteria', { masquerade: { ...mq, status_code: v } })} placeholder="200" />
            </Field>
          </>
        )}
      </Group>,
    );
  }
  if (ok.tuic) {
    items.push(
      <Group key="tuic" title="TUIC">
        <Toggle label={t('adv.zeroRtt')} hint={t('adv.zeroRttHint')} checked={!!tu.zero_rtt} onChange={(v) => patch('tuic', { zero_rtt: v })} />
        <Field label={t('adv.heartbeat')} hint={t('adv.seconds')} optional>
          <Num value={tu.heartbeat} onChange={(v) => patch('tuic', { heartbeat: v })} placeholder="10" />
        </Field>
        <Field label={t('adv.relayMode')} hint={t('adv.relayModeHint')}>
          <Select<string> ltr mono value={tu.udp_relay_mode || 'native'} onChange={(v) => patch('tuic', { udp_relay_mode: v as 'native' | 'quic' })} ariaLabel={t('adv.relayMode')} options={['native', 'quic'].map((m) => ({ value: m, label: m }))} />
        </Field>
      </Group>,
    );
  }
  if (ok.sockopt) {
    items.push(
      <Group key="so" title={t('adv.socket')}>
        {ok.proxyProtocol && (
          <>
            <Toggle label={t('adv.proxyProtocol')} hint={t('adv.proxyProtocolHint')} checked={!!so.accept_proxy_protocol} onChange={(v) => patch('sockopt', { accept_proxy_protocol: v })} />
            <Toggle label="TCP Fast Open" checked={!!so.tcp_fast_open} onChange={(v) => patch('sockopt', { tcp_fast_open: v })} />
            <Field label={t('adv.tcpCongestion')} optional>
              <Select<string> ltr mono value={so.congestion || 'default'} onChange={(v) => patch('sockopt', { congestion: v === 'default' ? '' : v })} ariaLabel={t('adv.tcpCongestion')} options={['default', ...TCP_CONGESTIONS].map((c) => ({ value: c, label: c === 'default' ? t('adv.systemDefault') : c }))} />
            </Field>
            <Field label={t('adv.keepaliveIdle')} hint={t('adv.seconds')} optional>
              <Num value={so.keepalive_idle} onChange={(v) => patch('sockopt', { keepalive_idle: v })} />
            </Field>
            <Field label={t('adv.keepaliveInterval')} hint={t('adv.seconds')} optional>
              <Num value={so.keepalive_interval} onChange={(v) => patch('sockopt', { keepalive_interval: v })} />
            </Field>
          </>
        )}
        <Field label={t('adv.mark')} hint={t('adv.markHint')} optional>
          <Num value={so.mark} onChange={(v) => patch('sockopt', { mark: v })} />
        </Field>
        <Toggle label={t('adv.v6only')} checked={!!so.v6only} onChange={(v) => patch('sockopt', { v6only: v })} />
      </Group>,
    );
  }
  if (ok.fragment || ok.noise) {
    items.push(
      <Group key="mask" title={t('adv.masks')}>
        {ok.fragment && (
          <>
            <Field label={t('adv.fragPackets')} hint={t('adv.fragPacketsHint')} optional>
              <Txt value={fr.packets} onChange={(v) => setAdv((y) => ({ ...y, masks: { ...y.masks, fragment: { ...fr, packets: v } } }))} placeholder="tlshello" />
            </Field>
            <Field label={t('adv.fragLength')} hint={t('adv.rangeHint')} error={err('adv.fragment')} optional>
              <Txt value={fr.length} onChange={(v) => setAdv((y) => ({ ...y, masks: { ...y.masks, fragment: { ...fr, length: v } } }))} placeholder="100-200" invalid={!!err('adv.fragment')} />
            </Field>
            <Field label={t('adv.fragDelay')} hint={t('adv.ms')} optional>
              <Txt value={fr.delay} onChange={(v) => setAdv((y) => ({ ...y, masks: { ...y.masks, fragment: { ...fr, delay: v } } }))} placeholder="10-20" />
            </Field>
          </>
        )}
        {ok.noise && (
          <>
            <Field label={t('adv.noiseSize')} hint={t('adv.rangeHint')} error={err('adv.noise')} optional>
              <Txt value={nz.rand} onChange={(v) => setAdv((y) => ({ ...y, masks: { ...y.masks, noise: { ...nz, rand: v } } }))} placeholder="10-20" invalid={!!err('adv.noise')} />
            </Field>
            <Field label={t('adv.noiseDelay')} hint={t('adv.ms')} optional>
              <Txt value={nz.delay} onChange={(v) => setAdv((y) => ({ ...y, masks: { ...y.masks, noise: { ...nz, delay: v } } }))} placeholder="10-16" />
            </Field>
          </>
        )}
        <p className={`${s.span2} ${s.note}`}>{t('adv.masksHint')}</p>
      </Group>,
    );
  }
  if (ok.reverse) {
    items.push(
      <Group key="rv" title={t('adv.reverse')}>
        <Toggle
          label={t('adv.reverseOn')}
          hint={t('adv.reverseHint')}
          checked={rv.enabled}
          onChange={(v) => setAdv((y) => ({ ...y, reverse: { ...rv, enabled: v, route_self: y.reverse?.route_self ?? true } }))}
        />
        {rv.enabled && (
          <>
            <Toggle label={t('adv.reverseSelf')} checked={rv.route_self} onChange={(v) => patch('reverse', { route_self: v })} />
            {others.length > 0 && (
              <div className={s.span2}>
                <span className="field__label">{t('adv.reverseOthers')}</span>
                <div className={s.chipRow}>
                  {others.map((o) => {
                    const on = (rv.route_inbound_ids ?? []).includes(o.id);
                    return (
                      <button key={o.id} type="button" className="pill" aria-pressed={on} onClick={() => patch('reverse', { route_inbound_ids: on ? (rv.route_inbound_ids ?? []).filter((y) => y !== o.id) : [...(rv.route_inbound_ids ?? []), o.id] })}>
                        <span dir="auto">{o.remark}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
            <div className={`${s.span2} ${s.keyBox}`}>
              <div className={s.keyHead}>
                <span className="field__label">
                  <KeyRound size={15} /> {t('adv.reverseId')}
                </span>
                <button type="button" className="btn btn--sm" onClick={() => patch('reverse', { uuid: '' })}>
                  <RefreshCw size={15} /> {t('adv.newId')}
                </button>
              </div>
              <code className="mono ltr">{rv.uuid || t('common.auto')}</code>
              {editing?.advanced?.reverse?.enabled ? (
                <button type="button" className="btn btn--sm btn--primary" onClick={downloadBridge} disabled={busy}>
                  {busy ? <Loader2 size={15} className="spin" /> : <Download size={15} />} {t('adv.bridgeConfig')}
                </button>
              ) : (
                <span className="field__hint">{t('adv.bridgeAfterSave')}</span>
              )}
              <span className="field__hint">{t('adv.bridgeHint')}</span>
            </div>
            <p className={`${s.span2} ${s.warnNote}`}>
              <span>{t('adv.reverseWarn')}</span>
            </p>
          </>
        )}
      </Group>,
    );
  }
  if (!items.length) return null;
  return (
    <AdvancedBlock id="connection" count={advancedCount(d.advanced) - (d.advanced?.tls ? 1 : 0) - (d.advanced?.reality ? 1 : 0) - (d.advanced?.fallbacks?.length ? 1 : 0)}>
      {items}
    </AdvancedBlock>
  );
}

/* ---------- security step ---------- */
function Limit({ label, value, onChange, t }: { label: string; value: AdvLimit | undefined; onChange: (v: AdvLimit) => void; t: T }) {
  const v = value ?? {};
  return (
    <>
      <h5 className={`${s.span2} ${s.advSub}`}>{label}</h5>
      <Field label={t('adv.afterBytes')} hint={t('adv.bytes')} optional>
        <Num value={v.after_bytes} onChange={(x) => onChange({ ...v, after_bytes: x })} />
      </Field>
      <Field label={t('adv.bytesPerSec')} hint={t('adv.bytes')} optional>
        <Num value={v.bytes_per_sec} onChange={(x) => onChange({ ...v, bytes_per_sec: x })} />
      </Field>
      <Field label={t('adv.burst')} hint={t('adv.bytes')} optional>
        <Num value={v.burst_bytes_per_sec} onChange={(x) => onChange({ ...v, burst_bytes_per_sec: x })} />
      </Field>
    </>
  );
}

export function SecurityAdvanced({ d, setAdv, err }: { d: InboundInput; setAdv: SetAdv; err: (k: string) => string | undefined }) {
  const { t } = useI18n();
  const a = d.advanced ?? {};
  const ok = applies(d);
  const patch = <K extends keyof InboundAdvanced>(k: K, v: Partial<NonNullable<InboundAdvanced[K]>>) => setAdv((x) => ({ ...x, [k]: { ...((x[k] as object) ?? {}), ...v } }));
  const tl = a.tls ?? {};
  const re = a.reality ?? {};
  const fbs: AdvFallback[] = a.fallbacks ?? [];
  const [busy, setBusy] = useState('');
  const [echName, setEchName] = useState('');
  const setFbs = (l: AdvFallback[]) => setAdv((x) => ({ ...x, fallbacks: l }));

  const genMl = async () => {
    setBusy('ml');
    try {
      const k = await genMldsa65Keys();
      patch('reality', { mldsa65_seed: k.server, mldsa65_verify: k.client });
      toast.success(t('adv.keysDone'));
    } catch (e) {
      toast.error(t('common.error'), (e as Error).message);
    } finally {
      setBusy('');
    }
  };
  const genEch = async () => {
    setBusy('ech');
    try {
      const k = await genEchKeys(echName.trim());
      patch('tls', { ech_server_keys: k.server, ech_config_list: k.client });
      toast.success(t('adv.keysDone'));
    } catch (e) {
      toast.error(t('common.error'), (e as Error).message);
    } finally {
      setBusy('');
    }
  };

  const items: ReactNode[] = [];
  if (ok.fallbacks) {
    items.push(
      <Group key="fb" title={t('adv.fallbacks')}>
        <p className={`${s.span2} ${s.note}`}>{t('adv.fallbacksHint')}</p>
        {fbs.map((f, i) => {
          const set = (v: Partial<AdvFallback>) => setFbs(fbs.map((x, j) => (j === i ? { ...x, ...v } : x)));
          return (
            <div key={i} className={`${s.span2} ${s.fbCard}`}>
              <div className={s.fbGrid}>
                <Field label={t('adv.fbDest')}>
                  <Txt value={f.dest} onChange={(v) => set({ dest: v })} placeholder="8080 / 127.0.0.1:8080" invalid={!!f.dest && !fallbackDestOk(f.dest)} />
                </Field>
                <Field label={t('adv.fbPath')} optional>
                  <Txt value={f.path} onChange={(v) => set({ path: v })} placeholder="/ws" />
                </Field>
                <Field label={t('adv.fbName')} optional>
                  <Txt value={f.name} onChange={(v) => set({ name: v })} placeholder="a.example.com" />
                </Field>
                <Field label="ALPN" optional>
                  <Select<string> ltr mono value={f.alpn || 'any'} onChange={(v) => set({ alpn: v === 'any' ? '' : v })} ariaLabel="ALPN" options={['any', 'h2', 'http/1.1'].map((x) => ({ value: x, label: x === 'any' ? t('adv.any') : x }))} />
                </Field>
                <Field label={t('adv.fbXver')} optional>
                  <Select<number> ltr value={f.xver ?? 0} onChange={(v) => set({ xver: v })} ariaLabel={t('adv.fbXver')} options={[0, 1, 2].map((x) => ({ value: x, label: x === 0 ? t('common.none') : `v${x}` }))} />
                </Field>
              </div>
              <button type="button" className="btn btn--sm btn--ghost" onClick={() => setFbs(fbs.filter((_, j) => j !== i))}>
                <Trash2 size={15} /> {t('common.delete')}
              </button>
            </div>
          );
        })}
        {err('adv.fallbacks') && <span className={`${s.span2} field__error`}>{err('adv.fallbacks')}</span>}
        <div className={s.span2}>
          <button type="button" className="btn btn--sm" onClick={() => setFbs([...fbs, { dest: '' }])}>
            <Plus size={15} /> {t('adv.addFallback')}
          </button>
        </div>
      </Group>,
    );
  }
  if (ok.tls) {
    items.push(
      <Group key="tls" title="TLS">
        <Field label={t('adv.minVersion')} optional>
          <Select<string> ltr value={tl.min_version || 'default'} onChange={(v) => patch('tls', { min_version: v === 'default' ? '' : v })} ariaLabel={t('adv.minVersion')} options={['default', ...TLS_VERSIONS].map((x) => ({ value: x, label: x === 'default' ? t('adv.systemDefault') : x }))} />
        </Field>
        <Field label={t('adv.maxVersion')} error={err('adv.tlsVer')} optional>
          <Select<string> ltr value={tl.max_version || 'default'} onChange={(v) => patch('tls', { max_version: v === 'default' ? '' : v })} ariaLabel={t('adv.maxVersion')} options={['default', ...TLS_VERSIONS].map((x) => ({ value: x, label: x === 'default' ? t('adv.systemDefault') : x }))} />
        </Field>
        <div className={s.span2}>
          <Field label={t('adv.ciphers')} hint={t('adv.ciphersHint')} error={err('adv.ciphers')} optional>
            <Txt value={tl.cipher_suites} onChange={(v) => patch('tls', { cipher_suites: v })} placeholder="TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:..." invalid={!!err('adv.ciphers')} />
          </Field>
        </div>
        <Toggle label={t('adv.rejectSni')} hint={t('adv.rejectSniHint')} checked={!!tl.reject_unknown_sni} onChange={(v) => patch('tls', { reject_unknown_sni: v })} />
        <Toggle label={t('adv.pinCert')} hint={t('adv.pinCertHint')} checked={!!tl.pin_cert} onChange={(v) => patch('tls', { pin_cert: v })} />
        <div className={`${s.span2} ${s.keyBox}`}>
          <div className={s.encHead}>
            <span>
              <span className={s.encTitle}>
                <KeyRound size={15} /> {t('adv.ech')}
              </span>
              <small className="field__hint" style={{ display: 'block' }}>
                {t('adv.echHint')}
              </small>
            </span>
            <Switch
              checked={!!tl.ech_server_keys}
              onChange={(v) => (v ? void genEch() : patch('tls', { ech_server_keys: '', ech_config_list: '' }))}
              label={t('adv.ech')}
            />
          </div>
          {!!tl.ech_server_keys && (
            <>
              <Field label={t('adv.echName')} hint={t('adv.echNameHint')} optional>
                <Txt value={echName} onChange={setEchName} placeholder="cloudflare-ech.com" />
              </Field>
              <div className={s.keyHead}>
                <span className="field__label">{t('adv.echKeys')}</span>
                <button type="button" className="btn btn--sm btn--primary" onClick={genEch} disabled={!!busy}>
                  {busy === 'ech' ? <Loader2 size={15} className="spin" /> : <Sparkles size={15} />} {t('inb.genKeys')}
                </button>
              </div>
              <div className={s.keys}>
                <label>
                  <small>{t('adv.echServerKeys')}</small>
                  <input className="input ltr mono" value={tl.ech_server_keys} onChange={(e) => patch('tls', { ech_server_keys: e.target.value.trim(), ech_config_list: '' })} spellCheck={false} autoComplete="off" />
                </label>
                <label>
                  <small>{t('adv.echConfig')}</small>
                  <code className="mono ltr">
                    <span className={s.keyClip}>{tl.ech_config_list || t('adv.afterSave')}</span>
                  </code>
                </label>
                {tl.ech_config_list && (
                  <span className={s.keyBtns}>
                    <CopyButton text={tl.ech_config_list} label={t('adv.copyClient')} />
                  </span>
                )}
              </div>
            </>
          )}
        </div>
      </Group>,
    );
  }
  if (ok.reality) {
    items.push(
      <Group key="re" title="Reality">
        <div className={`${s.span2} ${s.keyBox}`}>
          <div className={s.encHead}>
            <span>
              <span className={s.encTitle}>
                <KeyRound size={15} /> ML-DSA-65
              </span>
              <small className="field__hint" style={{ display: 'block' }}>
                {t('adv.mldsaHint')}
              </small>
            </span>
            <Switch checked={!!re.mldsa65_seed} onChange={(v) => (v ? void genMl() : patch('reality', { mldsa65_seed: '', mldsa65_verify: '' }))} label="ML-DSA-65" />
          </div>
          {!!re.mldsa65_seed && (
            <>
              <div className={s.keyHead}>
                <span className="field__label">{t('inb.enc.keys')}</span>
                <button type="button" className="btn btn--sm btn--primary" onClick={genMl} disabled={!!busy}>
                  {busy === 'ml' ? <Loader2 size={15} className="spin" /> : <Sparkles size={15} />} {t('inb.genKeys')}
                </button>
              </div>
              <div className={s.keys}>
                <label>
                  <small>{t('adv.mldsaSeed')}</small>
                  <input className="input ltr mono" value={re.mldsa65_seed} onChange={(e) => patch('reality', { mldsa65_seed: e.target.value.trim(), mldsa65_verify: '' })} spellCheck={false} autoComplete="off" />
                </label>
                <label>
                  <small>{t('adv.mldsaVerify')}</small>
                  <code className="mono ltr">
                    <span className={s.keyClip}>{re.mldsa65_verify || t('adv.afterSave')}</span>
                  </code>
                </label>
                {re.mldsa65_verify && (
                  <span className={s.keyBtns}>
                    <CopyButton text={re.mldsa65_verify} label={t('adv.copyClient')} />
                  </span>
                )}
              </div>
              <p className={s.warnNote}>
                <span>{t('adv.mldsaTarget')}</span>
              </p>
            </>
          )}
        </div>
        <Field label={t('adv.minClient')} error={err('adv.clientVer')} optional>
          <Txt value={re.min_client_ver} onChange={(v) => patch('reality', { min_client_ver: v })} placeholder="25.9.5" />
        </Field>
        <Field label={t('adv.maxClient')} optional>
          <Txt value={re.max_client_ver} onChange={(v) => patch('reality', { max_client_ver: v })} placeholder="" />
        </Field>
        <Field label={t('adv.maxTimeDiff')} hint={t('adv.maxTimeDiffHint')} optional>
          <Num value={re.max_time_diff} onChange={(v) => patch('reality', { max_time_diff: v })} placeholder="0" />
        </Field>
        <Limit label={t('adv.limitUp')} value={re.limit_fallback_upload} onChange={(v) => patch('reality', { limit_fallback_upload: v })} t={t} />
        <Limit label={t('adv.limitDown')} value={re.limit_fallback_download} onChange={(v) => patch('reality', { limit_fallback_download: v })} t={t} />
        <p className={`${s.span2} ${s.note}`}>{t('adv.limitHint')}</p>
      </Group>,
    );
  }
  if (!items.length) return null;
  const count = (a.fallbacks?.length ? 1 : 0) + (a.tls ? 1 : 0) + (a.reality ? 1 : 0);
  return (
    <AdvancedBlock id="security" count={count}>
      {items}
    </AdvancedBlock>
  );
}
