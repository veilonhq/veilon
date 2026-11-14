import { Activity, ArrowDown, ArrowUp, ArrowUpCircle, Clock, Cpu, MemoryStick, Plus, Server, Waypoints } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { useInbounds, useNodes, useTraffic, useUpdateOutdatedNodes } from '@/api/hooks';
import { ServerTraffic } from '@/components/ServerTraffic';
import type { VNode } from '@/api/types';
import { confirmAction, toast } from '@/store/feedback';
import { canUpdate, NodeUpdateRow, updating, updateErrText } from './nodes/NodeUpdate';
import { EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { formatClock, formatIsoDay, useI18n } from '@/i18n';
import { NodeSheet, NoPanelWarning } from './nodes/NodeSheet';
import { NodeXrayRow } from './nodes/NodeXray';
import s from './nodes/Nodes.module.css';
import { ltrIso, xrayNeedsIso } from '@/lib/advanced';

/** "2026-11-10 18:20" in the panel's zone (ISO 8601, Latin digits). */
export const isoStamp = (v: string) => `${formatIsoDay(v)} ${formatClock(v).slice(0, 5)}`;

export default function NodesPage() {
  const { t, n } = useI18n();
  const q = useNodes();
  const ibQ = useInbounds();
  const traffic = useTraffic('30d');
  const [params, setParams] = useSearchParams();
  const editId = Number(params.get('n')) || null;
  const creating = params.get('new') === '1';
  const askInstall = params.get('install') === '1';
  const editing = editId ? (q.data?.find((x) => x.id === editId) ?? null) : null;
  const go = (k: 'n' | 'new' | null, v?: string) => {
    const p = new URLSearchParams(params);
    p.delete('n');
    p.delete('new');
    p.delete('install');
    if (k && v) p.set(k, v);
    setParams(p);
  };
  const openInstall = (id: number) => {
    const p = new URLSearchParams(params);
    p.delete('new');
    p.set('n', String(id));
    p.set('install', '1');
    setParams(p);
  };
  const installAsked = () => {
    const p = new URLSearchParams(params);
    p.delete('install');
    setParams(p, { replace: true });
  };
  const remark = (id: number) => ibQ.data?.find((i) => i.id === id)?.remark;

  const newBtn = (
    <button className="btn btn--primary" onClick={() => go('new', '1')}>
      <Plus size={18} />
      {t('node.new')}
    </button>
  );
  const list = q.data ?? [];
  const online = list.filter((x) => x.state.status === 'online').length;
  const outdated = list.filter(canUpdate);
  const updateAll = useUpdateOutdatedNodes();
  const runUpdateAll = async () => {
    const to = outdated[0]?.update_to ?? '';
    const ok = await confirmAction({
      title: t('node.updAllTitle', { n: n(outdated.length) }),
      body: t('node.updAllBody', { to }),
      confirmLabel: t('node.updateAll'),
    });
    if (!ok) return;
    updateAll.mutate(undefined, {
      onSuccess: (r) => {
        const failed = r.results.filter((x) => !x.ok);
        if (r.started) toast.success(t('node.updAllStarted', { n: n(r.started) }));
        for (const f of failed) toast.error(f.name, updateErrText(t, f.code, f.message));
        if (!r.started && !failed.length) toast.success(t('node.updAllNone'));
      },
      onError: (x) => toast.error(t('common.error'), (x as Error).message),
    });
  };

  return (
    <div>
      <PageHeader
        eyebrow={q.data ? (list.length ? t('node.sum', { on: n(online), n: n(list.length) }) : t('node.count', { n: n(0) })) : undefined}
        title={t('node.title')}
        actions={
          <>
            {outdated.length > 0 && (
              <button className="btn" onClick={runUpdateAll} disabled={updateAll.isPending || list.some(updating)}>
                <ArrowUpCircle size={18} />
                {t('node.updateAll')}
              </button>
            )}
            {newBtn}
          </>
        }
      />
      <p className={s.intro}>{t('node.intro')}</p>
      <div className={s.pageWarn}>
        <NoPanelWarning />
      </div>
      {q.isError ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : q.isPending ? (
        <div className={s.grid}>
          {[0, 1].map((i) => (
            <Skeleton key={i} h={220} r={26} />
          ))}
        </div>
      ) : list.length === 0 ? (
        <EmptyState kind="gates" title={t('node.empty')} body={t('node.emptyBody')} action={newBtn} />
      ) : (
        <>
          <div className={s.grid}>
            {list.map((x) => (
              <NodeCard key={x.id} node={x} onOpen={() => go('n', String(x.id))} onInstall={() => openInstall(x.id)} remark={remark} />
            ))}
          </div>
          <section className={`glass ${s.servers}`}>
            <ServerTraffic servers={traffic.data?.servers} range="30d" showTitle />
          </section>
        </>
      )}
      <NodeSheet open={creating || !!editing} node={editing} askInstall={askInstall && !!editing} onInstallAsked={installAsked} onClose={() => go(null)} inbounds={ibQ.data ?? []} onCreated={(id) => go('n', String(id))} />
    </div>
  );
}

function NodeCard({ node: x, onOpen, onInstall, remark }: { node: VNode; onOpen: () => void; onInstall: () => void; remark: (id: number) => string | undefined }) {
  const { t, n, bytes } = useI18n();
  const st = x.state;
  const live = st.status === 'online' || st.status === 'degraded';
  const names = x.inbound_ids.map((id) => remark(id)).filter(Boolean) as string[];
  return (
    <div className={`${s.card} ${s['s_' + st.status]}`}>
      <button type="button" className={s.cardOpen} onClick={onOpen} aria-label={t('node.open', { name: x.name })} />
      <span className={s.top}>
        <span className={s.beacon} aria-hidden="true">
          <Server size={18} />
        </span>
        <span className={s.head}>
          <strong className={s.name}>{x.name}</strong>
          <span className={`${s.addr} ltr`}>
            <span className={s.host}>{x.address.includes(':') ? `[${x.address}]` : x.address}</span>
            <span className={s.port}>:{x.api_port}</span>
          </span>
        </span>
        <span className={s.status}>
          <i aria-hidden="true" />
          {t(`node.st.${st.status}`)}
        </span>
      </span>

      {st.error && (st.status === 'offline' || st.status === 'degraded') && <span className={s.err}>{st.error}</span>}

      {!!st.xray_too_old?.length && (
        <span className={s.xrayOld} role="status">
          {t('node.xrayOld', { ver: st.xray_version || '—', list: st.xray_too_old.map((o) => `${ltrIso(o.remark)}: ${xrayNeedsIso(o.needs)}`).join('؛ ') })}
        </span>
      )}

      {st.userfw_needs_update && (
        <span className={s.xrayOld} role="status">
          {t('nodes.userfwOld')}
        </span>
      )}

      <span className={s.metrics}>
        <span className={s.metric}>
          <small>
            <Activity size={12} aria-hidden="true" />
            {t('node.latency')}
          </small>
          <b>{live ? <bdi dir="ltr">{n(st.latency_ms)} ms</bdi> : '—'}</b>
        </span>
        <span className={s.metric}>
          <small>
            <Cpu size={12} aria-hidden="true" />
            {t('node.load')}
          </small>
          <b>{live ? n(st.load1, { maximumFractionDigits: 2 }) : '—'}</b>
        </span>
        <span className={s.metric}>
          <small>
            <MemoryStick size={12} aria-hidden="true" />
            {t('node.mem')}
          </small>
          <b>{live ? `${n(Math.round(st.mem_used * 100))}%` : '—'}</b>
          {live && (
            <span className={s.bar} aria-hidden="true">
              <span
                style={{
                  inlineSize: `${Math.min(100, Math.round(st.mem_used * 100))}%`,
                }}
              />
            </span>
          )}
        </span>
      </span>

      <span className={s.foot}>
        <span className={s.traffic} title={t('node.trafficAll')} aria-label={t('node.trafficAll')}>
          <span>
            <ArrowUp size={13} aria-hidden="true" />
            <bdi>{bytes(x.up)}</bdi>
          </span>
          <span>
            <ArrowDown size={13} aria-hidden="true" />
            <bdi>{bytes(x.down)}</bdi>
          </span>
        </span>
        <span className={s.seen}>
          <Clock size={13} aria-hidden="true" />
          <span className="ltr">{x.last_seen_at ? isoStamp(x.last_seen_at) : t('common.never')}</span>
        </span>
      </span>

      <span className={s.chips}>
        <Waypoints size={14} aria-hidden="true" />
        {names.length ? (
          names.map((r) => (
            <span key={r} className={s.chip}>
              {r}
            </span>
          ))
        ) : (
          <span className={s.muted}>{t('node.inboundsN', { n: n(0) })}</span>
        )}
      </span>
      <NodeUpdateRow node={x} onInstall={onInstall} />
      <NodeXrayRow node={x} />
    </div>
  );
}
