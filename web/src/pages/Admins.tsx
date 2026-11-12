import { Crown, Lock, Plus, SlidersHorizontal, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useAdmins, useDeleteAdmin, useUpdateAdmin } from '@/api/hooks';
import { ApiError } from '@/api/client';
import type { Admin } from '@/api/types';
import { EmptyState, ErrorState, PageHeader, Section, Skeleton, Switch } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import { useSession } from '@/store/session';
import { AdminSheet } from './admins/AdminSheet';
import s from './pages.module.css';

export default function AdminsPage() {
  const { t, n, bytes } = useI18n();
  const me = useSession((x) => x.admin);
  const sudo = !!me?.is_sudo;
  const q = useAdmins(sudo);
  const del = useDeleteAdmin();
  const upd = useUpdateAdmin();
  const [editId, setEditId] = useState<number | null>(null);
  const [creating, setCreating] = useState(false);

  if (!sudo)
    return (
      <div>
        <PageHeader title={t('adm.title')} />
        <Section>
          <div className={s.locked}>
            <Lock size={28} />
            <p>{t('adm.sudoOnly')}</p>
          </div>
        </Section>
      </div>
    );

  const remove = async (a: Admin) => {
    if (!(await confirmAction({ title: t('adm.deleteConfirm', { name: a.username }), danger: true, confirmLabel: t('common.delete') }))) return;
    del.mutate(a.id, { onSuccess: () => toast.success(t('adm.deleted')), onError: (x) => toast.error(t('common.error'), x instanceof ApiError && x.status === 409 ? t('adm.ownsUsers') : (x as Error).message) });
  };

  const setDisabled = async (a: Admin, disabled: boolean) => {
    if (disabled && !(await confirmAction({ title: t('adm.disableConfirm', { name: a.username }), body: t('adm.disableBody'), danger: true, confirmLabel: t('adm.disable') }))) return;
    upd.mutate(
      { id: a.id, patch: { disabled } },
      {
        onSuccess: () => toast.success(disabled ? t('adm.disabledToast') : t('adm.enabledToast')),
        onError: (x) => toast.error(t('common.error'), x instanceof ApiError && /last sudo/.test(x.message) ? t('adm.lastSudo') : (x as Error).message),
      },
    );
  };

  return (
    <div>
      <PageHeader
        title={t('adm.title')}
        actions={
          <button className="btn btn--primary" onClick={() => setCreating(true)}>
            <Plus size={18} />
            {t('adm.new')}
          </button>
        }
      />
      <div className={s.adminGrid}>
        {q.isError ? (
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        ) : q.isPending ? (
          [0, 1, 2].map((i) => <Skeleton key={i} h={120} r={24} />)
        ) : q.data.length === 0 ? (
          <EmptyState kind="search" title="—" />
        ) : (
          q.data.map((a) => (
            <article key={a.id} className={`glass ${s.adminCard} ${a.disabled ? s.adminOff : ''}`}>
              <span className={`${s.adminAvatar} ${a.is_sudo ? s.adminSudo : ''}`}>{a.username.slice(0, 1).toUpperCase()}</span>
              <div className={s.adminMain}>
                <strong className="ltr">{a.username}</strong>
                <span className={s.adminChips}>
                  <span className={`chip ${a.is_sudo ? 'chip--violet' : ''}`}>
                    {a.is_sudo && <Crown size={12} />}
                    {a.is_sudo ? t('adm.sudo') : t('adm.regular')}
                  </span>
                  {a.disabled && <span className="chip chip--disabled">{t('adm.disabled')}</span>}
                  {!a.is_sudo && a.permissions && <span className="chip">{t('adm.permCount', { n: n(a.permissions.length) })}</span>}
                </span>
                {!a.is_sudo && (
                  <span className={s.adminQuota}>
                    {a.user_quota ? t('adm.usersOf', { n: n(a.users ?? 0), m: n(a.user_quota) }) : t('adm.usersN', { n: n(a.users ?? 0) })}
                    {!!a.traffic_quota && ` · ${t('adm.trafficOf', { v: `\u2066${bytes(a.traffic_allocated ?? 0, 0)}\u2069`, m: `\u2066${bytes(a.traffic_quota, 0)}\u2069` })}`}
                  </span>
                )}
              </div>
              {a.username === me?.username ? (
                <span className="chip chip--active">{t('adm.you')}</span>
              ) : (
                <span className={s.adminBtns}>
                  <span className={s.adminSwitch} title={a.disabled ? t('adm.enable') : t('adm.disable')}>
                    <Switch checked={!a.disabled} onChange={(on) => setDisabled(a, !on)} label={`${a.disabled ? t('adm.enable') : t('adm.disable')} ${a.username}`} />
                  </span>
                  <button className="btn btn--sm btn--icon btn--ghost" onClick={() => setEditId(a.id)} aria-label={`${t('adm.access')} ${a.username}`} title={t('adm.access')}>
                    <SlidersHorizontal size={16} />
                  </button>
                  <button className="btn btn--sm btn--icon btn--ghost" onClick={() => remove(a)} aria-label={`${t('common.delete')} ${a.username}`} title={t('common.delete')}>
                    <Trash2 size={16} />
                  </button>
                </span>
              )}
            </article>
          ))
        )}
      </div>
      <AdminSheet
        admin={creating ? null : (q.data?.find((a) => a.id === editId) ?? null)}
        creating={creating}
        onClose={() => {
          setCreating(false);
          setEditId(null);
        }}
      />
    </div>
  );
}
