import { Activity, ArrowDown, ArrowUp, Clock, Cpu, MemoryStick, Plus, Server, Waypoints } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { useInbounds, useNodes } from '@/api/hooks';
import type { VNode } from '@/api/types';
import { EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { formatClock, formatIsoDay, useI18n } from '@/i18n';
import { NodeSheet } from './nodes/NodeSheet';
import s from './nodes/Nodes.module.css';

/** "2026-11-10 18:20" in the panel's zone (ISO 8601, Latin digits). */
export const isoStamp = (v: string) => `${formatIsoDay(v)} ${formatClock(v).slice(0, 5)}`;

export default function NodesPage() {
  const { t, n } = useI18n();
  const q = useNodes();
  const ibQ = useInbounds();
  const [params, setParams] = useSearchParams();
  const editId = Number(params.get('n')) || null;
  const creating = params.get('new') === '1';
  const editing = editId ? (q.data?.find((x) => x.id === editId) ?? null) : null;
  const go = (k: 'n' | 'new' | null, v?: string) => {
    const p = new URLSearchParams(params);
    p.delete('n');
    p.delete('new');
    if (k && v) p.set(k, v);
    setParams(p);
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

  return (
    <div>
      <PageHeader
        eyebrow={q.data ? (list.length ? t('node.sum', { on: n(online), n: n(list.length) }) : t('node.count', { n: n(0) })) : undefined}
        title={t('node.title')}
        actions={newBtn}
      />
      <p className={s.intro}>{t('node.intro')}</p>
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
        <div className={s.grid}>
          {list.map((x) => (
            <NodeCard key={x.id} node={x} onOpen={() => go('n', String(x.id))} remark={remark} />
          ))}
        </div>
      )}
      <NodeSheet open={creating || !!editing} node={editing} onClose={() => go(null)} inbounds={ibQ.data ?? []} onCreated={(id) => go('n', String(id))} />
    </div>
  );
}

function NodeCard({ node: x, onOpen, remark }: { node: VNode; onOpen: () => void; remark: (id: number) => string | undefined }) {
  const { t, n, bytes } = useI18n();
  const st = x.state;
  const live = st.status === 'online' || st.status === 'degraded';
  const names = x.inbound_ids.map((id) => remark(id)).filter(Boolean) as string[];
  return (
    <button type="button" className={`${s.card} ${s['s_' + st.status]}`} onClick={onOpen}>
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

      <span className={s.metrics}>
        <span className={s.metric}>
          <small>{t('node.xray')}</small>
          <b className="ltr">{st.xray_version || '—'}</b>
        </span>
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
        <span className={s.traffic}>
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
    </button>
  );
}
