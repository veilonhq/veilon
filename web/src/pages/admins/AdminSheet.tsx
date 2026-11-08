import { Check, Loader2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useUpdateAdmin } from '@/api/hooks';
import type { Admin, AdminPatch, Perm } from '@/api/types';
import { Sheet } from '@/components/Overlay';
import { Switch } from '@/components/misc';
import { useI18n, type MessageKey } from '@/i18n';
import { ALL_PERMS, AREA_PERMS, USER_PERMS } from '@/lib/perms';
import { GB } from '@/lib/user';
import { toast } from '@/store/feedback';
import { NumChips, Stepper, gb } from '../users/UserForm';
import u from '../users/Users.module.css';
import s from '../pages.module.css';

const TRAFFIC = [0, 500, 1000, 5000];

interface Props {
  admin: Admin | null;
  onClose: () => void;
}

/** Permissions, quotas and sudo access of one admin (PATCH /admins/{id}). */
export function AdminSheet({ admin, onClose }: Props) {
  const { t, n, bytes } = useI18n();
  const update = useUpdateAdmin();
  const [sudo, setSudo] = useState(false);
  const [perms, setPerms] = useState<Set<Perm>>(new Set());
  const [userQuota, setUserQuota] = useState(0);
  const [trafficGb, setTrafficGb] = useState(0);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!admin) return;
    setErr(null);
    setSudo(admin.is_sudo);
    setPerms(new Set(admin.permissions ?? ALL_PERMS));
    setUserQuota(admin.user_quota ?? 0);
    setTrafficGb(gb(admin.traffic_quota ?? 0));
  }, [admin]);

  const toggle = (p: Perm, on: boolean) =>
    setPerms((x) => {
      const y = new Set(x);
      if (on) y.add(p);
      else y.delete(p);
      // seeing all users is pointless without seeing users at all
      if (p === 'users_view' && !on) y.delete('all_users');
      if (p === 'all_users' && on) y.add('users_view');
      return y;
    });

  const save = () => {
    if (!admin) return;
    const patch: AdminPatch = {};
    if (sudo !== admin.is_sudo) patch.is_sudo = sudo;
    if (!sudo) {
      const list = ALL_PERMS.filter((p) => perms.has(p));
      const before = ALL_PERMS.filter((p) => (admin.permissions ?? ALL_PERMS).includes(p));
      if (admin.is_sudo || list.join() !== before.join()) patch.permissions = list;
      if (userQuota !== (admin.user_quota ?? 0)) patch.user_quota = userQuota;
      const tq = Math.round(trafficGb * GB);
      if (tq !== (admin.traffic_quota ?? 0)) patch.traffic_quota = tq;
    }
    if (!Object.keys(patch).length) return onClose();
    setErr(null);
    update.mutate(
      { id: admin.id, patch },
      {
        onSuccess: () => {
          toast.success(t('adm.saved'));
          onClose();
        },
        onError: (x) => setErr((x as Error).message),
      },
    );
  };

  const row = (p: Perm) => (
    <div key={p} className={s.switchCard}>
      <span>
        <strong>{t(`perm.${p}` as MessageKey)}</strong>
        <small>{t(`perm.${p}.hint` as MessageKey)}</small>
      </span>
      <Switch checked={perms.has(p)} onChange={(v) => toggle(p, v)} label={t(`perm.${p}` as MessageKey)} />
    </div>
  );

  return (
    <Sheet
      open={!!admin}
      onClose={onClose}
      title={t('adm.access')}
      subtitle={admin ? <span className="ltr">{admin.username}</span> : undefined}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn--primary" onClick={save} disabled={update.isPending}>
            {update.isPending ? <Loader2 size={17} className="spin" /> : <Check size={17} />}
            {update.isPending ? t('common.saving') : t('common.save')}
          </button>
        </>
      }
    >
      {admin && (
        <div className={u.form}>
          {err && (
            <div className={u.formErr} role="alert">
              {err}
            </div>
          )}
          <div className={s.switchCard}>
            <span>
              <strong>{t('adm.isSudo')}</strong>
              <small>{t('adm.sudoHint')}</small>
            </span>
            <Switch checked={sudo} onChange={setSudo} label={t('adm.isSudo')} />
          </div>

          {!sudo && (
            <>
              <fieldset className={u.fs}>
                <legend className="field__label">{t('adm.permUsers')}</legend>
                <div className={s.permList}>{USER_PERMS.map(row)}</div>
              </fieldset>
              <fieldset className={u.fs}>
                <legend className="field__label">{t('adm.permAreas')}</legend>
                <div className={s.permList}>{AREA_PERMS.map(row)}</div>
              </fieldset>
              <fieldset className={u.fs}>
                <legend className="field__label">{t('adm.quotas')}</legend>
                <div className={u.fs}>
                  <span className="field__label">{t('adm.userQuota')}</span>
                  <Stepper value={userQuota} onChange={setUserQuota} max={100000} label={t('adm.userQuota')} format={(v) => (v === 0 ? t('common.unlimited') : n(v))} />
                  <span className="field__hint">{t('adm.userQuotaHint', { n: n(admin.users ?? 0) })}</span>
                </div>
                <div className={u.fs}>
                  <span className="field__label">{t('adm.trafficQuota')}</span>
                  <NumChips value={trafficGb} onChange={setTrafficGb} options={TRAFFIC} zero={t('common.unlimited')} unit="GB" label={t('adm.trafficQuota')} />
                  <span className="field__hint">
                    {t('adm.trafficQuotaHint', {
                      v: `\u2066${bytes(admin.traffic_allocated ?? 0)}\u2069`,
                    })}
                  </span>
                </div>
              </fieldset>
            </>
          )}
        </div>
      )}
    </Sheet>
  );
}
