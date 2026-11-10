import { ArrowDown, ArrowUp, Check, ChevronDown, Loader2, Plus, Signpost, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useInboundHosts, useSaveHosts } from '@/api/hooks';
import type { Inbound, InboundHost } from '@/api/types';
import { EmptyState, Field, Skeleton, Switch } from '@/components/misc';
import { Sheet } from '@/components/Overlay';
import { Select } from '@/components/Select';
import { useI18n } from '@/i18n';
import { isV2 } from '@/lib/protocols';
import { toast } from '@/store/feedback';
import s from './Inbounds.module.css';

const FPS = ['', 'chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'random', 'randomized'];
const ALPNS = ['h3', 'h2', 'http/1.1'];
const HOST_RE = /^(\*\.)?([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.?$/;
const IP_RE = /^(\d{1,3}(\.\d{1,3}){3}|\[?[0-9a-fA-F:]+\]?)$/;
const okHost = (v: string) => !v || HOST_RE.test(v) || IP_RE.test(v);

const blank = (): InboundHost => ({ remark: '', address: '', port: 0, sni: '', host: '', path: '', fingerprint: '', alpn: '', enabled: true });

export function hostErrors(h: InboundHost): Partial<Record<keyof InboundHost, 'address' | 'port' | 'path'>> {
  const e: Partial<Record<keyof InboundHost, 'address' | 'port' | 'path'>> = {};
  if (!okHost(h.address.trim())) e.address = 'address';
  if (!okHost(h.sni.trim())) e.sni = 'address';
  if (!okHost(h.host.trim())) e.host = 'address';
  if (!Number.isInteger(h.port) || h.port < 0 || h.port > 65535) e.port = 'port';
  if (h.path && (!h.path.startsWith('/') || /\s/.test(h.path))) e.path = 'path';
  return e;
}

/** Hosts (link overrides) of one inbound: each enabled host becomes its own share/subscription link. */
export function HostsSheet({ inbound, onClose }: { inbound: Inbound | null; onClose: () => void }) {
  const { t, n } = useI18n();
  const q = useInboundHosts(inbound?.id ?? null);
  const save = useSaveHosts();
  const [list, setList] = useState<InboundHost[] | null>(null);
  const [open, setOpen] = useState<number | null>(null);
  const [tried, setTried] = useState(false);

  useEffect(() => {
    if (!inbound) {
      setList(null);
      setOpen(null);
      setTried(false);
    } else if (q.data && list === null) setList(q.data.map((h) => ({ ...h })));
  }, [inbound, q.data, list]);

  if (!inbound) return <Sheet open={false} onClose={onClose} title=""><div /></Sheet>;
  const v2 = isV2(inbound.protocol);
  const tls = inbound.security === 'tls';
  const secure = tls || inbound.security === 'reality';
  const pathy = v2 && ['ws', 'httpupgrade', 'xhttp'].includes(inbound.network);
  const defSNI = inbound.security === 'reality' ? (inbound.reality?.server_names?.[0] ?? '') : (inbound.tls?.sni ?? '');
  const defAddr = inbound.address_override || t('hosts.serverAddr');

  const up = (i: number, k: keyof InboundHost, v: unknown) => setList((l) => (l ? l.map((h, j) => (j === i ? { ...h, [k]: v } : h)) : l));
  const moveBy = (i: number, d: number) =>
    setList((l) => {
      if (!l) return l;
      const j = i + d;
      if (j < 0 || j >= l.length) return l;
      const c = [...l];
      [c[i], c[j]] = [c[j], c[i]];
      setOpen((o) => (o === i ? j : o === j ? i : o));
      return c;
    });
  const bad = (list ?? []).map(hostErrors);
  const anyBad = bad.some((e) => Object.keys(e).length > 0);
  const dirty = !!list && !!q.data && JSON.stringify(list) !== JSON.stringify(q.data);

  const submit = () => {
    setTried(true);
    if (!list) return;
    if (anyBad) {
      setOpen(bad.findIndex((e) => Object.keys(e).length > 0));
      return;
    }
    save.mutate(
      { id: inbound.id, hosts: list.map((h) => ({ ...h, address: h.address.trim(), sni: h.sni.trim(), host: h.host.trim(), path: h.path.trim() })) },
      {
        onSuccess: (r) => {
          setList(r.map((h) => ({ ...h })));
          toast.success(t('hosts.saved'));
          onClose();
        },
        onError: (e) => toast.error(t('common.error'), (e as Error).message),
      },
    );
  };
  const errText = (i: number, k: keyof InboundHost) => {
    const e = tried ? bad[i]?.[k] : undefined;
    return e === 'address' ? t('hosts.err.address') : e === 'port' ? t('xc.err.port') : e === 'path' ? t('hosts.err.path') : undefined;
  };

  return (
    <Sheet
      open={!!inbound}
      onClose={onClose}
      title={t('hosts.title')}
      subtitle={<bdi>{inbound.remark}</bdi>}
      footer={
        <>
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn--primary" disabled={!dirty || save.isPending} onClick={submit}>
            {save.isPending ? <Loader2 size={16} className="spin" /> : <Check size={16} />}
            {t('common.save')}
          </button>
        </>
      }
    >
      <p className={s.hostIntro}>{t('hosts.intro')}</p>
      {!list ? (
        <div className={s.hostList}>
          <Skeleton h={64} r={18} />
          <Skeleton h={64} r={18} />
        </div>
      ) : (
        <>
          {list.length === 0 && <EmptyState kind="gates" title={t('hosts.empty')} body={t('hosts.emptyBody')} />}
          <ol className={s.hostList}>
            {list.map((h, i) => {
              const expanded = open === i;
              const summary = `${h.address || defAddr}:${h.port || inbound.port}`;
              const hasErr = tried && Object.keys(bad[i]).length > 0;
              return (
                <li key={i} className={`${s.hostCard} ${h.enabled ? '' : s.hostOff} ${hasErr ? s.hostErr : ''}`}>
                  <div className={s.hostHead}>
                    <button type="button" className={s.hostToggle} aria-expanded={expanded} onClick={() => setOpen(expanded ? null : i)}>
                      <span className={s.hostIdx}>{n(i + 1)}</span>
                      <span className={s.hostText}>
                        <strong>{h.remark ? <bdi>{h.remark}</bdi> : <bdi>{inbound.remark}</bdi>}</strong>
                        <small className="ltr">{summary}</small>
                      </span>
                      <ChevronDown size={16} className={s.hostChev} aria-hidden="true" />
                    </button>
                    <Switch checked={h.enabled} onChange={(v) => up(i, 'enabled', v)} label={t('hosts.enabled')} />
                  </div>
                  {expanded && (
                    <div className={s.hostBody}>
                      <Field label={t('hosts.remark')} hint={t('hosts.remarkHint')} optional>
                        <input className="input" value={h.remark} maxLength={64} placeholder={inbound.remark} onChange={(e) => up(i, 'remark', e.target.value)} />
                      </Field>
                      <div className={s.hostAddr}>
                        <Field label={t('hosts.address')} error={errText(i, 'address')} optional>
                          <input className="input ltr" value={h.address} placeholder={defAddr} onChange={(e) => up(i, 'address', e.target.value.trim())} aria-invalid={!!errText(i, 'address')} />
                        </Field>
                        <Field label={t('hosts.port')} error={errText(i, 'port')} optional>
                          <input className="input ltr num" inputMode="numeric" value={h.port || ''} placeholder={String(inbound.port)} onChange={(e) => up(i, 'port', Number(e.target.value.replace(/\D/g, '')) || 0)} />
                        </Field>
                      </div>
                      {secure && (
                        <div className={s.hostGrid}>
                          <Field label="SNI" error={errText(i, 'sni')} optional>
                            <input className="input ltr" value={h.sni} placeholder={defSNI} onChange={(e) => up(i, 'sni', e.target.value.trim())} />
                          </Field>
                          <Field label={t('xc.out.fp')}>
                            <Select<string> value={h.fingerprint} onChange={(v) => up(i, 'fingerprint', v)} ariaLabel={t('xc.out.fp')} options={FPS.map((f) => ({ value: f, label: f ? <span className="ltr">{f}</span> : t('hosts.inherit'), text: f || t('hosts.inherit') }))} />
                          </Field>
                        </div>
                      )}
                      {pathy && (
                        <div className={s.hostGrid}>
                          <Field label={t('hosts.hostHeader')} error={errText(i, 'host')} optional>
                            <input className="input ltr" value={h.host} placeholder={inbound.transport.host || ''} onChange={(e) => up(i, 'host', e.target.value.trim())} />
                          </Field>
                          <Field label={t('xc.out.path')} error={errText(i, 'path')} optional>
                            <input className="input ltr mono" value={h.path} placeholder={inbound.transport.path || '/'} onChange={(e) => up(i, 'path', e.target.value)} />
                          </Field>
                        </div>
                      )}
                      {tls && (
                        <div className="field">
                          <span className="field__label">ALPN</span>
                          <div className={`seg ${s.hostAlpn}`}>
                            {ALPNS.map((a) => {
                              const on = h.alpn.split(',').includes(a);
                              return (
                                <button
                                  key={a}
                                  type="button"
                                  aria-pressed={on}
                                  onClick={() => up(i, 'alpn', ALPNS.filter((x) => (x === a ? !on : h.alpn.split(',').includes(x))).join(','))}
                                >
                                  <span className="ltr">{a}</span>
                                </button>
                              );
                            })}
                          </div>
                          <span className="field__hint">{t('hosts.alpnHint')}</span>
                        </div>
                      )}
                      <div className={s.hostTools}>
                        <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === 0} onClick={() => moveBy(i, -1)} aria-label={t('xc.up')} title={t('xc.up')}>
                          <ArrowUp size={16} />
                        </button>
                        <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === list.length - 1} onClick={() => moveBy(i, 1)} aria-label={t('xc.down')} title={t('xc.down')}>
                          <ArrowDown size={16} />
                        </button>
                        <button
                          type="button"
                          className="btn btn--sm btn--danger-ghost"
                          onClick={() => {
                            setList((l) => (l ? l.filter((_, j) => j !== i) : l));
                            setOpen(null);
                          }}
                        >
                          <Trash2 size={15} />
                          {t('common.delete')}
                        </button>
                      </div>
                    </div>
                  )}
                </li>
              );
            })}
          </ol>
          <button
            type="button"
            className="btn btn--sm"
            disabled={list.length >= 32}
            onClick={() => {
              setList((l) => [...(l ?? []), blank()]);
              setOpen(list.length);
            }}
          >
            <Plus size={15} />
            {t('hosts.add')}
          </button>
        </>
      )}
    </Sheet>
  );
}

export const HostsIcon = Signpost;
