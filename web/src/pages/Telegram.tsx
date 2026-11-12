import { AnimatePresence, motion } from 'framer-motion';
import { AlertTriangle, Bot, Check, CheckCircle2, ExternalLink, Loader2, Send, Trash2, XCircle } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiError } from '@/api/client';
import { useSaveTelegram, useTelegram, useTestTelegram } from '@/api/hooks';
import type { TelegramInput, TelegramNotify, TelegramSettings, TelegramTestResult } from '@/api/types';
import { ErrorState, Field, PageHeader, PasswordInput, Section, Skeleton, Switch } from '@/components/misc';
import { useI18n, type MessageKey } from '@/i18n';
import { MAX_TG_ADMINS, formatAdminIds, parseAdminIds, validProxy, validToken } from '@/lib/telegram';
import { confirmAction, toast } from '@/store/feedback';
import s from './pages.module.css';

interface Draft {
  enabled: boolean;
  admins: string;
  proxy: string;
  notify: TelegramNotify;
  /** a new token; "" keeps the stored one */
  token: string;
  clearToken: boolean;
}

const fromServer = (v: TelegramSettings): Draft => ({
  enabled: v.enabled,
  admins: formatAdminIds(v.admins),
  proxy: v.proxy,
  notify: { ...v.notify },
  token: '',
  clearToken: false,
});

const NOTIFY: Array<{ key: keyof TelegramNotify; label: MessageKey; hint: MessageKey }> = [
  { key: 'traffic', label: 'tg.n.traffic', hint: 'tg.n.trafficHint' },
  { key: 'expiry', label: 'tg.n.expiry', hint: 'tg.n.expiryHint' },
  { key: 'nodes', label: 'tg.n.nodes', hint: 'tg.n.nodesHint' },
  { key: 'update', label: 'tg.n.update', hint: 'tg.n.updateHint' },
];

/** Telegram admin bot (sudo admins): token, admin ids, proxy, notifications, test and live status. */
export default function TelegramPage() {
  const { t } = useI18n();
  const q = useTelegram();
  const save = useSaveTelegram();
  const test = useTestTelegram();
  const [draft, setDraft] = useState<Draft | null>(null);
  const base = useRef<Draft | null>(null);
  const [result, setResult] = useState<TelegramTestResult | null>(null);
  const [testErr, setTestErr] = useState('');

  // Server values replace the draft field by field; a field being edited is kept.
  useEffect(() => {
    if (!q.data) return;
    const next = fromServer(q.data);
    const prev = base.current;
    base.current = next;
    setDraft((d) => {
      if (!d || !prev) return next;
      const out: Draft = { ...next, token: d.token, clearToken: d.clearToken && q.data!.token_set };
      if (d.enabled !== prev.enabled) out.enabled = d.enabled;
      if (d.admins !== prev.admins) out.admins = d.admins;
      if (d.proxy !== prev.proxy) out.proxy = d.proxy;
      if (JSON.stringify(d.notify) !== JSON.stringify(prev.notify)) out.notify = d.notify;
      return out;
    });
  }, [q.data]);

  const parsed = useMemo(() => parseAdminIds(draft?.admins ?? ''), [draft?.admins]);
  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />;
  const v = q.data;
  const set = <K extends keyof Draft>(k: K, val: Draft[K]) => setDraft((d) => (d ? { ...d, [k]: val } : d));

  const tokenTyped = draft?.token.trim() ?? '';
  const tokenErr = tokenTyped && !validToken(tokenTyped) ? t('tg.tokenInvalid') : undefined;
  const adminsErr = parsed.bad.length
    ? t('tg.adminsBad', { v: parsed.bad.join('، ') })
    : parsed.ids.length > MAX_TG_ADMINS
      ? t('tg.adminsMax', { n: MAX_TG_ADMINS })
      : undefined;
  const proxyErr = draft && !validProxy(draft.proxy) ? t('tg.proxyInvalid') : undefined;
  const willHaveToken = !!tokenTyped || (!!v?.token_set && !draft?.clearToken);
  const enableErr = draft?.enabled ? (!willHaveToken ? t('tg.needToken') : parsed.ids.length === 0 ? t('tg.needAdmins') : undefined) : undefined;
  const invalid = !!(tokenErr || adminsErr || proxyErr || enableErr);

  const b = base.current;
  const dirty =
    !!draft &&
    !!b &&
    (draft.enabled !== b.enabled ||
      formatAdminIds(parsed.ids) !== b.admins ||
      draft.proxy.trim() !== b.proxy ||
      JSON.stringify(draft.notify) !== JSON.stringify(b.notify) ||
      !!tokenTyped ||
      draft.clearToken);

  const errText = (x: unknown) => (x instanceof ApiError ? x.message : (x as Error).message);

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!draft || !b || invalid) return;
    const body: TelegramInput = {};
    if (draft.enabled !== b.enabled) body.enabled = draft.enabled;
    if (formatAdminIds(parsed.ids) !== b.admins) body.admins = parsed.ids;
    if (draft.proxy.trim() !== b.proxy) body.proxy = draft.proxy.trim();
    if (JSON.stringify(draft.notify) !== JSON.stringify(b.notify)) body.notify = draft.notify;
    if (tokenTyped) body.token = tokenTyped;
    else if (draft.clearToken) body.clear_token = true;
    save.mutate(body, {
      onSuccess: (nv) => {
        // the typed token is gone from the page once saved: only the hint stays
        base.current = fromServer(nv);
        setDraft(fromServer(nv));
        toast.success(body.clear_token ? t('tg.tokenRemoved') : t('tg.saved'));
      },
      onError: (x) => toast.error(t('common.error'), errText(x)),
    });
  };

  const removeToken = async () => {
    if (!(await confirmAction({ title: t('tg.tokenRemoveTitle'), body: t('tg.tokenRemoveBody'), danger: true, confirmLabel: t('tg.tokenRemove') }))) return;
    setDraft((d) => (d ? { ...d, token: '', clearToken: true, enabled: false } : d));
  };

  const runTest = () => {
    if (!draft) return;
    setResult(null);
    setTestErr('');
    const body: { token?: string; admins?: number[]; proxy?: string } = { admins: parsed.ids, proxy: draft.proxy.trim() };
    if (tokenTyped) body.token = tokenTyped;
    test.mutate(body, {
      onSuccess: (r) => setResult(r),
      onError: (x) => setTestErr(errText(x)),
    });
  };
  const testBlock = !willHaveToken || draft?.clearToken ? t('tg.testNeedToken') : parsed.ids.length === 0 ? t('tg.testNeedAdmins') : undefined;

  return (
    <div>
      <PageHeader title={t('nav.telegram')} subtitle={t('tg.pageSub')} />
      {!draft || !v ? (
        <div className={s.settingsGrid}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h={260} r={24} />
          ))}
        </div>
      ) : (
        <form className={s.settingsGrid} onSubmit={submit} aria-label={t('nav.telegram')}>
          <Section title={t('tg.status')} className={s.fullRow}>
            <StatusRow v={v} />
          </Section>

          <Section title={t('tg.bot')}>
            <div className={s.form}>
              <div className={s.switchCard}>
                <span>
                  <strong>{t('tg.enable')}</strong>
                  <small>{t('tg.enableHint')}</small>
                </span>
                <Switch checked={draft.enabled} onChange={(x) => set('enabled', x)} label={t('tg.enable')} />
              </div>
              {enableErr && (
                <p className={s.warn}>
                  <AlertTriangle size={15} /> {enableErr}
                </p>
              )}
              <Field
                label={t('tg.token')}
                error={tokenErr}
                hint={draft.clearToken ? t('tg.tokenPending') : v.token_set ? t('tg.tokenKeep', { hint: `\u2066${v.token_hint}\u2069` }) : t('tg.tokenHint')}
              >
                <PasswordInput
                  value={draft.token}
                  onChange={(e) => setDraft((d) => (d ? { ...d, token: e.target.value, clearToken: false } : d))}
                  placeholder={v.token_set && !draft.clearToken ? v.token_hint : '123456:ABC...'}
                  autoComplete="off"
                  name="telegram-token"
                  aria-invalid={!!tokenErr}
                />
              </Field>
              {v.token_set && !draft.clearToken && !tokenTyped && (
                <button type="button" className="btn btn--sm btn--ghost" onClick={removeToken}>
                  <Trash2 size={15} />
                  {t('tg.tokenRemove')}
                </button>
              )}
              <Field label={t('tg.admins')} error={adminsErr} hint={t('tg.adminsHint')}>
                <input
                  className="input ltr num"
                  value={draft.admins}
                  onChange={(e) => set('admins', e.target.value)}
                  placeholder="123456789, 987654321"
                  inputMode="text"
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={!!adminsErr}
                />
              </Field>
              <Field label={t('tg.proxy')} optional error={proxyErr} hint={t('tg.proxyHint')}>
                <input
                  className="input ltr mono"
                  value={draft.proxy}
                  onChange={(e) => set('proxy', e.target.value)}
                  placeholder="socks5://127.0.0.1:1080"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  aria-invalid={!!proxyErr}
                />
              </Field>
              <p className={s.tgNote}>{t('tg.actsAs')}</p>
            </div>
          </Section>

          <div className={s.tgCol}>
            <Section title={t('tg.notify')}>
              <div className={s.form}>
                <p className={s.tgNote}>{t('tg.notifyHint')}</p>
                <div className={s.permList}>
                  {NOTIFY.map((it) => (
                    <div key={it.key} className={s.switchCard}>
                      <span>
                        <strong>{t(it.label)}</strong>
                        <small>{t(it.hint)}</small>
                      </span>
                      <Switch checked={draft.notify[it.key]} onChange={(x) => set('notify', { ...draft.notify, [it.key]: x })} label={t(it.label)} />
                    </div>
                  ))}
                </div>
              </div>
            </Section>

            <Section title={t('tg.test')}>
              <div className={s.form}>
                <p className={s.tgNote}>{t('tg.testHint')}</p>
                <button type="button" className="btn" onClick={runTest} disabled={!!testBlock || !!tokenErr || !!adminsErr || !!proxyErr || test.isPending}>
                  {test.isPending ? <Loader2 size={16} className="spin" /> : <Send size={16} />}
                  {test.isPending ? t('tg.testing') : t('tg.testBtn')}
                </button>
                {testBlock && <small className={s.tgMuted}>{testBlock}</small>}
                {testErr && (
                  <p className={s.tgResultErr} role="alert">
                    <XCircle size={16} />
                    <span>
                      <strong>{t('tg.testError')}</strong>
                      <small className="ltr">{testErr}</small>
                    </span>
                  </p>
                )}
                {result && (
                  <div className={s.tgResult} role="status">
                    <p className={s.tgResultBot}>
                      <Bot size={16} />
                      <span>{t('tg.bot')}</span>
                      <strong className="ltr">@{result.bot.username || result.bot.name}</strong>
                    </p>
                    <ul className={s.tgResultList}>
                      {result.admins.map((a) => (
                        <li key={a.id}>
                          {a.ok ? <CheckCircle2 size={16} className={s.tgOk} /> : <XCircle size={16} className={s.tgBad} />}
                          <span className="ltr num">{a.id}</span>
                          <span className={`chip ${a.ok ? 'chip--active' : 'chip--expired'}`}>{a.ok ? t('tg.testOk') : t('tg.testFail')}</span>
                          {a.error && <small className={`ltr ${s.tgErrText}`}>{a.error}</small>}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </Section>
          </div>

          <AnimatePresence>
            {dirty && (
              <motion.div className={s.saveBar} initial={{ y: 30, opacity: 0, x: '-50%' }} animate={{ y: 0, opacity: 1, x: '-50%' }} exit={{ y: 30, opacity: 0, x: '-50%' }}>
                <span>{t('set.unsaved')}</span>
                <button type="button" className="btn btn--sm btn--ghost" onClick={() => b && setDraft({ ...b })}>
                  {t('common.cancel')}
                </button>
                <button type="submit" className="btn btn--sm btn--primary" disabled={save.isPending || invalid}>
                  {save.isPending ? <Loader2 size={15} className="spin" /> : <Check size={15} />}
                  {t('common.save')}
                </button>
              </motion.div>
            )}
          </AnimatePresence>
        </form>
      )}
    </div>
  );
}

function StatusRow({ v }: { v: TelegramSettings }) {
  const { t, relative, stamp } = useI18n();
  const st = v.status;
  const state = st.running ? 'running' : st.error ? 'failing' : 'stopped';
  return (
    <div className={s.tgStatus}>
      <span className={`${s.tgBadge} ${state === 'running' ? s.tgBadgeOn : state === 'failing' ? s.tgBadgeErr : ''}`} aria-hidden="true">
        <Bot size={24} />
      </span>
      <div className={s.tgStatusMain}>
        <span className={s.adminChips}>
          <span className={`chip ${state === 'running' ? 'chip--active' : state === 'failing' ? 'chip--expired' : 'chip--disabled'}`}>
            {t(state === 'running' ? 'tg.running' : state === 'failing' ? 'tg.failing' : 'tg.stopped')}
          </span>
          {st.username && <strong className="ltr">@{st.username}</strong>}
        </span>
        {st.running && st.since && (
          <small className={s.tgMuted} title={stamp(st.since)}>
            {t('tg.since', { t: relative(st.since) })}
          </small>
        )}
        {!st.running && !st.error && <small className={s.tgMuted}>{t('tg.statusOff')}</small>}
        {st.error && (
          <small className={s.tgErrText}>
            {t('tg.statusError')}: <span className="ltr">{st.error}</span>
          </small>
        )}
      </div>
      {st.username && (
        <a className="btn btn--sm" href={`https://t.me/${st.username}`} target="_blank" rel="noreferrer noopener">
          <ExternalLink size={15} />
          {t('tg.openBot')}
        </a>
      )}
    </div>
  );
}
