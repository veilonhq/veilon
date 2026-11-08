import { Crown, Loader2, Lock, Plus, SlidersHorizontal, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useAdmins, useCreateAdmin, useDeleteAdmin } from '@/api/hooks';
import type { Admin } from '@/api/types';
import { EmptyState, ErrorState, Field, PageHeader, PasswordInput, Section, Skeleton, Switch } from '@/components/misc';
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
  const create = useCreateAdmin();
  const del = useDeleteAdmin();
  const [u, setU] = useState('');
  const [p, setP] = useState('');
  const [isSudo, setIsSudo] = useState(false);
  const [editId, setEditId] = useState<number | null>(null);

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

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(
      { username: u.trim(), password: p, is_sudo: isSudo },
      {
        onSuccess: (a) => {
          toast.success(t('adm.created'));
          // a new sub-admin starts with the default flags: open its access sheet
          if (!a.is_sudo && a.id) setEditId(a.id);
          setU('');
          setP('');
          setIsSudo(false);
        },
        onError: (x) => toast.error(t('common.error'), (x as Error).message),
      },
    );
  };
  const remove = async (a: Admin) => {
    if (!(await confirmAction({ title: t('adm.deleteConfirm', { name: a.username }), danger: true, confirmLabel: t('common.delete') }))) return;
    del.mutate(a.id, { onSuccess: () => toast.success(t('adm.deleted')), onError: (x) => toast.error(t('common.error'), (x as Error).message) });
  };

  return (
    <div>
      <PageHeader title={t('adm.title')} />
      <div className={s.twoCol}>
        <div className={s.adminGrid}>
          {q.isError ? (
            <ErrorState error={q.error} onRetry={() => q.refetch()} />
          ) : q.isPending ? (
            [0, 1, 2].map((i) => <Skeleton key={i} h={120} r={24} />)
          ) : q.data.length === 0 ? (
            <EmptyState kind="search" title="—" />
          ) : (
            q.data.map((a) => (
              <article key={a.id} className={`glass ${s.adminCard}`}>
                <span className={`${s.adminAvatar} ${a.is_sudo ? s.adminSudo : ''}`}>{a.username.slice(0, 1).toUpperCase()}</span>
                <div className={s.adminMain}>
                  <strong className="ltr">{a.username}</strong>
                  <span className={s.adminChips}>
                    <span className={`chip ${a.is_sudo ? 'chip--violet' : ''}`}>
                      {a.is_sudo && <Crown size={12} />}
                      {a.is_sudo ? t('adm.sudo') : t('adm.regular')}
                    </span>
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
        <Section title={t('adm.new')} className={s.sticky}>
          <form className={s.form} onSubmit={submit}>
            <Field label={t('form.username')}>
              <input className="input ltr" value={u} onChange={(e) => setU(e.target.value)} required autoCapitalize="none" autoComplete="off" />
            </Field>
            <Field label={t('login.password')} hint={t('set.passwordShort')}>
              <PasswordInput value={p} onChange={(e) => setP(e.target.value)} required minLength={8} autoComplete="new-password" />
            </Field>
            <div className={s.switchLine}>
              <span>{t('adm.isSudo')}</span>
              <Switch checked={isSudo} onChange={setIsSudo} label={t('adm.isSudo')} />
            </div>
            <button className="btn btn--primary btn--block" disabled={!u || p.length < 8 || create.isPending}>
              {create.isPending ? <Loader2 size={17} className="spin" /> : <Plus size={17} />}
              {t('common.create')}
            </button>
          </form>
        </Section>
      </div>
      <AdminSheet admin={q.data?.find((a) => a.id === editId) ?? null} onClose={() => setEditId(null)} />
    </div>
  );
}
