import { Check, Loader2, Plus } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useCreateAdmin, useUpdateAdmin } from '@/api/hooks';
import type { Admin, AdminPatch, Perm } from '@/api/types';
import { Sheet } from '@/components/Overlay';
import { Field, PasswordInput, Switch } from '@/components/misc';
import { useI18n, type MessageKey } from '@/i18n';
import { ALL_PERMS, AREA_PERMS, USER_PERMS } from '@/lib/perms';
import { GB } from '@/lib/user';
import { toast } from '@/store/feedback';
import { NumChips, Stepper, gb } from '../users/UserForm';
import u from '../users/Users.module.css';
import s from '../pages.module.css';

const TRAFFIC = [0, 500, 1000, 5000];
/** What a new sub-admin starts with (matches the server's default): manage its own users. */
const NEW_PERMS: Perm[] = ['users_view', 'users_create', 'users_edit', 'users_delete'];

interface Props {
  /** the admin whose access is edited (PATCH /admins/{id}) */
  admin: Admin | null;
  /** open the sheet as "new admin" (POST /admins) instead */
  creating?: boolean;
  onClose: () => void;
}

const samePerms = (a: Perm[] | undefined, b: Perm[]) => ALL_PERMS.filter((p) => (a ?? ALL_PERMS).includes(p)).join() === ALL_PERMS.filter((p) => b.includes(p)).join();

/** New admin, or permissions, quotas and sudo access of one admin. */
export function AdminSheet({ admin, creating = false, onClose }: Props) {
  const { t, n, bytes } = useI18n();
  const update = useUpdateAdmin();
  const create = useCreateAdmin();
  const open = creating || !!admin;
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [sudo, setSudo] = useState(false);
  const [perms, setPerms] = useState<Set<Perm>>(new Set());
  const [userQuota, setUserQuota] = useState(0);
  const [trafficGb, setTrafficGb] = useState(0);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (creating) {
      setErr(null);
      setUsername('');
      setPassword('');
      setSudo(false);
      setPerms(new Set(NEW_PERMS));
      setUserQuota(0);
      setTrafficGb(0);
      return;
    }
    if (!admin) return;
    setErr(null);
    setSudo(admin.is_sudo);
    setPerms(new Set(admin.permissions ?? ALL_PERMS));
    setUserQuota(admin.user_quota ?? 0);
    setTrafficGb(gb(admin.traffic_quota ?? 0));
  }, [admin, creating]);

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

  const pending = update.isPending || create.isPending;
  const fail = (x: unknown) => setErr((x as Error).message);

  const submitCreate = async () => {
    const name = username.trim();
    if (!name || password.length < 8) return;
    const list = ALL_PERMS.filter((p) => perms.has(p));
    const tq = Math.round(trafficGb * GB);
    setErr(null);
    try {
      const a = await create.mutateAsync({
        username: name,
        password,
        is_sudo: sudo,
        ...(sudo ? {} : { permissions: list, user_quota: userQuota, traffic_quota: tq }),
      });
      // a server that ignores access fields on create gets them right after, in the same submit
      if (!sudo && a?.id && (!samePerms(a.permissions, list) || (a.user_quota ?? 0) !== userQuota || (a.traffic_quota ?? 0) !== tq)) {
        try {
          await update.mutateAsync({
            id: a.id,
            patch: {
              permissions: list,
              user_quota: userQuota,
              traffic_quota: tq,
            },
          });
        } catch (x) {
          toast.error(t('adm.createdNoAccess'), (x as Error).message);
          onClose();
          return;
        }
      }
      toast.success(t('adm.created'));
      onClose();
    } catch (x) {
      fail(x);
    }
  };

  const submitEdit = () => {
    if (!admin) return;
    const patch: AdminPatch = {};
    if (sudo !== admin.is_sudo) patch.is_sudo = sudo;
    if (!sudo) {
      const list = ALL_PERMS.filter((p) => perms.has(p));
      if (admin.is_sudo || !samePerms(admin.permissions, list)) patch.permissions = list;
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
        onError: fail,
      },
    );
  };

  const save = () => (creating ? submitCreate() : submitEdit());
  const canSave = !creating || (!!username.trim() && password.length >= 8);

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
      open={open}
      onClose={onClose}
      title={creating ? t('adm.new') : t('adm.access')}
      subtitle={!creating && admin ? <span className="ltr">{admin.username}</span> : undefined}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="admin-sheet-form" className="btn btn--primary" disabled={pending || !canSave}>
            {pending ? <Loader2 size={17} className="spin" /> : creating ? <Plus size={17} /> : <Check size={17} />}
            {pending ? t('common.saving') : creating ? t('common.create') : t('common.save')}
          </button>
        </>
      }
    >
      {open && (
        <form
          id="admin-sheet-form"
          className={u.form}
          onSubmit={(e) => {
            e.preventDefault();
            if (!pending && canSave) save();
          }}
        >
          {err && (
            <div className={u.formErr} role="alert">
              {err}
            </div>
          )}
          {creating && (
            <>
              <Field label={t('form.username')}>
                <input className="input ltr" value={username} onChange={(e) => setUsername(e.target.value)} required autoCapitalize="none" autoComplete="off" spellCheck={false} />
              </Field>
              <Field label={t('login.password')} hint={t('set.passwordShort')}>
                <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoComplete="new-password" />
              </Field>
            </>
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
                  <span className="field__hint">{creating || !admin ? t('adm.userQuotaNew') : t('adm.userQuotaHint', { n: n(admin.users ?? 0) })}</span>
                </div>
                <div className={u.fs}>
                  <span className="field__label">{t('adm.trafficQuota')}</span>
                  <NumChips value={trafficGb} onChange={setTrafficGb} options={TRAFFIC} zero={t('common.unlimited')} unit="GB" label={t('adm.trafficQuota')} />
                  <span className="field__hint">
                    {creating || !admin
                      ? t('adm.trafficQuotaNew')
                      : t('adm.trafficQuotaHint', {
                          v: `\u2066${bytes(admin.traffic_allocated ?? 0)}\u2069`,
                        })}
                  </span>
                </div>
              </fieldset>
            </>
          )}
        </form>
      )}
    </Sheet>
  );
}
