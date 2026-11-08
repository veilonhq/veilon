import { Plus, ShieldBan, Users as UsersIcon, Waypoints } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { useGroups, useInbounds } from '@/api/hooks';
import type { Group } from '@/api/types';
import { EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { useI18n } from '@/i18n';
import { GroupForm } from './groups/GroupForm';
import s from './pages.module.css';

export default function GroupsPage() {
  const { t, n, bytes } = useI18n();
  const q = useGroups();
  const inboundsQ = useInbounds();
  const [params, setParams] = useSearchParams();
  const editId = Number(params.get('g')) || null;
  const creating = params.get('new') === '1';
  const editing = editId ? (q.data?.find((g) => g.id === editId) ?? null) : null;
  const go = (k: 'g' | 'new' | null, v?: string) => {
    const p = new URLSearchParams(params);
    p.delete('g');
    p.delete('new');
    if (k && v) p.set(k, v);
    setParams(p);
  };

  const summary = (g: Group) => {
    const d = g.defaults ?? {};
    return [
      d.data_limit ? `\u2066${bytes(d.data_limit, 0)}\u2069` : d.data_limit === 0 ? t('common.unlimited') : '',
      d.expire_days ? `${n(d.expire_days)} ${t('common.days')}` : d.on_hold_days ? t('form.expiry.onHold') : '',
      d.ip_limit ? t('form.sum.ips', { n: n(d.ip_limit) }) : '',
      d.speed_limit_down || d.speed_limit_up ? t('badge.speed') : '',
    ]
      .filter(Boolean)
      // each part is its own bidi run, so "100 GB" never swaps places with "30 days"
      .map((x) => `\u2068${x}\u2069`)
      .join(' · ');
  };

  const newBtn = (
    <button className="btn btn--primary" onClick={() => go('new', '1')}>
      <Plus size={18} />
      {t('grp.new')}
    </button>
  );

  return (
    <div>
      <PageHeader eyebrow={q.data ? t('grp.count', { n: q.data.length }) : undefined} title={t('grp.title')} actions={newBtn} />
      {q.isError ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : q.isPending ? (
        <div className={s.groupGrid}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h={132} r={24} />
          ))}
        </div>
      ) : q.data.length === 0 ? (
        <EmptyState kind="passes" title={t('grp.empty')} body={t('grp.emptyBody')} action={newBtn} />
      ) : (
        <div className={s.groupGrid}>
          {q.data.map((g) => {
            const sum = summary(g);
            return (
              <button key={g.id} className={`glass ${s.groupCard}`} onClick={() => go('g', String(g.id))} style={{ '--g': g.color } as React.CSSProperties}>
                <span className={s.groupHead}>
                  <span className={s.groupDot} aria-hidden="true" />
                  <strong className={s.groupName}>{g.name}</strong>
                  {g.ad_block && (
                    <span className="chip chip--active" title={t('grp.adBlock')}>
                      <ShieldBan size={12} />
                      {t('grp.adBlockShort')}
                    </span>
                  )}
                </span>
                <span className={s.groupStats}>
                  <span>
                    <UsersIcon size={14} aria-hidden="true" />
                    {t('grp.members', { n: n(g.users) })}
                  </span>
                  <span>
                    <Waypoints size={14} aria-hidden="true" />
                    {t('form.sum.inbounds', { n: n(g.inbound_ids.length) })}
                  </span>
                </span>
                <span className={s.groupSum}>{sum || t('grp.noDefaults')}</span>
              </button>
            );
          })}
        </div>
      )}
      <GroupForm open={creating || !!editing} group={editing} onClose={() => go(null)} inbounds={inboundsQ.data ?? []} />
    </div>
  );
}
