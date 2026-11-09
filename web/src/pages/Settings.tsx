import { AlertTriangle, AtSign, Check, KeyRound, Loader2 } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useCerts, useChangePassword, useSaveSettings, useSettings, useUpdateMe } from '@/api/hooks';
import { ApiError } from '@/api/client';
import { useSession } from '@/store/session';
import type { Settings } from '@/api/types';
import { ThemeLangToggles } from '@/components/Shell';
import { ErrorState, Field, PageHeader, PasswordInput, Section, Skeleton, Switch } from '@/components/misc';
import { can } from '@/lib/perms';
import { translate, useI18n } from '@/i18n';
import { toast } from '@/store/feedback';
import { useUi } from '@/store/ui';
import { IS_MOCK } from '@/app/base';
import { AnimatePresence, motion } from 'framer-motion';
import { Select, type SelectOption } from '@/components/Select';
import { listTimeZones } from '@/lib/timezones';
import s from './pages.module.css';

function useTzOptions(current: string | undefined): SelectOption<string>[] {
  const { t } = useI18n();
  return useMemo(() => {
    const all = listTimeZones();
    const find = (z: string) => all.find((x) => x.zone === z);
    const pinned = ['Asia/Tehran', 'UTC'];
    const opts: SelectOption<string>[] = [];
    for (const z of pinned) {
      const o = find(z);
      opts.push({
        value: z,
        label: z === 'Asia/Tehran' ? `${t('set.tzTehran')} · Asia/Tehran` : 'UTC',
        text: z === 'Asia/Tehran' ? `${t('set.tzTehran')} tehran iran Asia/Tehran` : 'UTC GMT',
        hint: o?.offset ?? (z === 'UTC' ? 'UTC+00:00' : 'UTC+03:30'),
        group: t('set.tzPinned'),
      });
    }
    if (current && !pinned.includes(current) && !find(current)) opts.push({ value: current, label: current, text: current, group: t('set.tzPinned') });
    for (const o of all) {
      if (pinned.includes(o.zone)) continue;
      opts.push({ value: o.zone, label: o.zone.replace(/_/g, ' '), text: `${o.zone} ${o.zone.replace(/_/g, ' ')} ${o.offset}`, hint: o.offset, group: t('set.tzAll') });
    }
    return opts;
  }, [t, current]);
}

export default function SettingsPage() {
  const { t } = useI18n();
  const q = useSettings();
  const certs = useCerts();
  const save = useSaveSettings();
  const setLang = useUi((x) => x.setLang);
  const setTz = useUi((x) => x.setTz);
  const [draft, setDraft] = useState<Settings | null>(null);
  const base = useRef<Settings | null>(null);
  const tzOptions = useTzOptions(draft?.timezone);
  const mayEdit = can(useSession((x) => x.admin), 'settings');

  // New server values replace the draft field by field; a field the admin
  // is still editing (changed from the previous server value) is kept.
  useEffect(() => {
    if (!q.data) return;
    const prev = base.current;
    base.current = q.data;
    setDraft((d) => {
      if (!d || !prev) return q.data!;
      const out = { ...q.data! };
      (Object.keys(d) as Array<keyof Settings>).forEach((k) => {
        if (d[k] !== prev[k]) (out as Record<string, unknown>)[k] = d[k];
      });
      return out;
    });
  }, [q.data]);
  const loc = useLocation();
  const ready = !!draft;
  useEffect(() => {
    if (ready && loc.hash === '#account') document.getElementById('account')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [ready, loc.hash, loc.key]);

  const dirty = !!draft && !!q.data && JSON.stringify(draft) !== JSON.stringify(q.data);
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setDraft((d) => (d ? { ...d, [k]: v } : d));

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!draft || !q.data) return;
    const patch: Partial<Settings> = {};
    (Object.keys(draft) as Array<keyof Settings>).forEach((k) => {
      if (draft[k] !== q.data![k]) (patch as Record<string, unknown>)[k] = draft[k];
    });
    save.mutate(patch, {
      onSuccess: (st) => {
        toast.success(t('set.saved'));
        if (patch.language && (st.language === 'fa' || st.language === 'en')) setLang(st.language);
        if (patch.timezone) setTz(st.timezone || 'UTC');
        // a panel_path change applies immediately: the UI must move to the new path
        if (patch.panel_path && st.panel_path && !IS_MOCK) window.location.assign(`/${st.panel_path}/settings`);
      },
      onError: (x) => toast.error(t('common.error'), (x as Error).message),
    });
  };

  // The default language applies at once: the UI switches now (this browser
  // keeps it) and the panel default is saved without waiting for Save.
  const pickLanguage = (v: 'fa' | 'en') => {
    set('language', v);
    setLang(v);
    if (!q.data || q.data.language === v) return;
    save.mutate(
      { language: v },
      {
        onSuccess: () => toast.success(translate(v, 'set.saved')),
        onError: (x) => {
          set('language', q.data!.language);
          setLang(q.data!.language);
          toast.error(translate(q.data!.language, 'common.error'), (x as Error).message);
        },
      },
    );
  };

  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />;

  return (
    <div>
      <PageHeader title={t('set.title')} />
      {!draft ? (
        <div className={s.settingsGrid}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h={300} r={24} />
          ))}
        </div>
      ) : (
        <div className={s.settingsGrid}>
          <AccountSection />
          {mayEdit && (
          <form id="settings-form" onSubmit={submit} className={s.contents}>
          <Section title={t('set.panel')}>
            <div className={s.form}>
              <div className={s.row2}>
                <Field label={t('set.panelPort')}>
                  <input className="input num ltr" type="number" value={draft.panel_port} onChange={(e) => set('panel_port', Number(e.target.value))} />
                </Field>
                <Field label={t('set.panelPath')}>
                  <input className="input mono ltr" value={draft.panel_path} onChange={(e) => set('panel_path', e.target.value.replace(/[^a-zA-Z0-9_-]/g, ''))} />
                </Field>
              </div>
              {draft.panel_path !== q.data?.panel_path && (
                <p className={s.warn}>
                  <AlertTriangle size={15} /> {t('set.pathWarn')}
                </p>
              )}
              <Field label={t('set.panelDomain')} optional>
                <input className="input ltr" value={draft.panel_domain} onChange={(e) => set('panel_domain', e.target.value)} />
              </Field>
              <Field label={t('set.panelCert')} optional>
                <Select<number>
                  value={draft.panel_cert_id ?? 0}
                  onChange={(v) => set('panel_cert_id', v || null)}
                  ariaLabel={t('set.panelCert')}
                  options={[
                    { value: 0, label: t('set.noCert') },
                    ...(certs.data ?? []).map((c) => ({
                      value: c.id,
                      label: <span className="ltr">{c.domain}</span>,
                      text: [c.domain, ...(c.identifiers ?? [])].join(' '),
                      hint: t('set.certOption', {
                        type: c.source === 'acme' ? `${t(`certs.type.${c.type ?? 'domain'}`)} · ${c.profile === 'shortlived' ? t('certs.profile.shortlived') : 'ACME'}` : t(`certs.source.${c.source}`),
                        left: new Date(c.not_after).getTime() <= Date.now() ? t('certs.expired') : t('certs.daysLeft', { n: Math.ceil((new Date(c.not_after).getTime() - Date.now()) / 86_400_000) }),
                      }),
                    })),
                  ]}
                />
              </Field>
            </div>
          </Section>

          <Section title={t('set.sub')}>
            <div className={s.form}>
              <div className={s.row2}>
                <Field label={t('set.subPath')}>
                  <input className="input mono ltr" value={draft.sub_path} onChange={(e) => set('sub_path', e.target.value)} />
                </Field>
                <Field label={t('set.subUpdate')}>
                  <input className="input num ltr" type="number" min={1} value={draft.sub_update_hours} onChange={(e) => set('sub_update_hours', Number(e.target.value))} />
                </Field>
              </div>
              <Field label={t('set.subDomain')} optional>
                <input className="input ltr" value={draft.sub_domain} onChange={(e) => set('sub_domain', e.target.value)} />
              </Field>
              <Field label={t('set.subTitle')}>
                <input className="input" value={draft.sub_title} onChange={(e) => set('sub_title', e.target.value)} />
              </Field>
            </div>
          </Section>

          <Section title={t('set.server')}>
            <div className={s.form}>
              <Field label={t('set.serverAddress')}>
                <input className="input ltr" value={draft.server_address} onChange={(e) => set('server_address', e.target.value)} />
              </Field>
              <div className={s.row2}>
                <Field label={t('set.timezone')}>
                  <Select<string>
                    value={draft.timezone || 'UTC'}
                    onChange={(v) => set('timezone', v)}
                    options={tzOptions}
                    searchable
                    searchPlaceholder={t('set.tzSearch')}
                    ariaLabel={t('set.timezone')}
                  />
                </Field>
                <Field label={t('set.language')} hint={t('set.languageHint')}>
                  <Select<'fa' | 'en'>
                    value={draft.language}
                    onChange={pickLanguage}
                    ariaLabel={t('set.language')}
                    options={[
                      { value: 'fa', label: 'فارسی' },
                      { value: 'en', label: 'English' },
                    ]}
                  />
                </Field>
              </div>
              <div className={s.appearance}>
                <span className="field__label">{t('set.appearance')}</span>
                <div className={s.appearanceCtl}>
                  <ThemeLangToggles />
                </div>
              </div>
            </div>
          </Section>

          <Section title={t('set.blocking')}>
            <div className={s.form}>
              <div className={s.switchCard}>
                <span>
                  <strong>{t('set.adBlock')}</strong>
                  <small>{t('set.adBlockHint')}</small>
                </span>
                <Switch checked={!!draft.ad_block} onChange={(v) => set('ad_block', v)} label={t('set.adBlock')} />
              </div>
              <div className={s.switchCard}>
                <span>
                  <strong>{t('set.blockTorrent')}</strong>
                  <small>{t('set.blockTorrentHint')}</small>
                </span>
                <Switch checked={!!draft.block_torrent} onChange={(v) => set('block_torrent', v)} label={t('set.blockTorrent')} />
              </div>
            </div>
          </Section>

          <AnimatePresence>
            {dirty && (
              <motion.div className={s.saveBar} initial={{ y: 30, opacity: 0, x: '-50%' }} animate={{ y: 0, opacity: 1, x: '-50%' }} exit={{ y: 30, opacity: 0, x: '-50%' }}>
                <span>{t('set.unsaved')}</span>
                <button type="button" className="btn btn--sm btn--ghost" onClick={() => setDraft(q.data!)}>
                  {t('common.cancel')}
                </button>
                <button type="submit" className="btn btn--sm btn--primary" disabled={save.isPending}>
                  {save.isPending ? <Loader2 size={15} className="spin" /> : <Check size={15} />}
                  {t('common.save')}
                </button>
              </motion.div>
            )}
          </AnimatePresence>
          </form>
          )}
        </div>
      )}
    </div>
  );
}

const USERNAME_RE = /^[a-zA-Z0-9_.-]{3,32}$/;

/** The signed-in admin's own account: change username and change password. */
function AccountSection() {
  const { t } = useI18n();
  const admin = useSession((x) => x.admin);
  const rename = useUpdateMe();
  const pw = useChangePassword();
  const [name, setName] = useState('');
  const [namePw, setNamePw] = useState('');
  const [cur, setCur] = useState('');
  const [nw, setNw] = useState('');
  const trimmed = name.trim();
  const nameErr = trimmed && !USERNAME_RE.test(trimmed) ? t('form.usernameInvalid') : trimmed && trimmed === admin?.username ? t('set.usernameSame') : undefined;
  const errText = (x: unknown) =>
    x instanceof ApiError && x.code === 'invalid_credentials' ? t('set.wrongPassword') : x instanceof ApiError && x.status === 409 ? t('set.usernameTaken') : (x as Error).message;

  const doRename = (e: React.FormEvent) => {
    e.preventDefault();
    if (!trimmed || nameErr || !namePw) return;
    rename.mutate(
      { username: trimmed, current_password: namePw },
      {
        onSuccess: () => {
          toast.success(t('set.usernameChanged'));
          setName('');
          setNamePw('');
        },
        onError: (x) => toast.error(t('common.error'), errText(x)),
      },
    );
  };
  const doPassword = (e: React.FormEvent) => {
    e.preventDefault();
    if (!cur || nw.length < 8) return;
    pw.mutate(
      { current_password: cur, new_password: nw },
      {
        onSuccess: () => {
          toast.success(t('set.passwordChanged'));
          setCur('');
          setNw('');
        },
        onError: (x) => toast.error(t('common.error'), errText(x)),
      },
    );
  };

  return (
    <Section title={t('set.account')} className={s.accountSection}>
      <div id="account" className={s.form}>
        <p className={s.accountWho}>
          <span className={s.accountAvatar} aria-hidden="true">
            {(admin?.username ?? 'A').slice(0, 1).toUpperCase()}
          </span>
          <span>
            {t('set.signedInAs')}{' '}
            <strong className="ltr">{admin?.username}</strong>
          </span>
        </p>
        <form className={s.form} onSubmit={doRename} aria-label={t('set.username')}>
          <h3 className={s.subhead}>{t('set.username')}</h3>
          <div className={s.row2}>
            <Field label={t('set.newUsername')} hint={t('form.usernameHint')} error={nameErr}>
              <input
                className="input ltr"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={admin?.username}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                autoComplete="username"
                aria-invalid={!!nameErr}
              />
            </Field>
            <Field label={t('set.currentPassword')} hint={t('set.confirmWithPassword')}>
              <PasswordInput value={namePw} onChange={(e) => setNamePw(e.target.value)} autoComplete="current-password" />
            </Field>
          </div>
          <button type="submit" className="btn" disabled={!trimmed || !!nameErr || !namePw || rename.isPending}>
            {rename.isPending ? <Loader2 size={16} className="spin" /> : <AtSign size={16} />}
            {t('set.username')}
          </button>
        </form>
        <form className={`${s.form} ${s.subForm}`} onSubmit={doPassword} aria-label={t('set.password')}>
          <h3 className={s.subhead}>{t('set.password')}</h3>
          <input type="text" name="username" autoComplete="username" value={admin?.username ?? ''} readOnly hidden />
          <div className={s.row2}>
            <Field label={t('set.currentPassword')}>
              <PasswordInput value={cur} onChange={(e) => setCur(e.target.value)} autoComplete="current-password" />
            </Field>
            <Field label={t('set.newPassword')} hint={t('set.passwordShort')} error={nw && nw.length < 8 ? t('set.passwordShort') : undefined}>
              <PasswordInput value={nw} onChange={(e) => setNw(e.target.value)} autoComplete="new-password" aria-invalid={!!nw && nw.length < 8} />
            </Field>
          </div>
          <button type="submit" className="btn" disabled={!cur || nw.length < 8 || pw.isPending}>
            {pw.isPending ? <Loader2 size={16} className="spin" /> : <KeyRound size={16} />}
            {t('set.password')}
          </button>
        </form>
      </div>
    </Section>
  );
}
