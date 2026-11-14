import { Cable, Check, ChevronDown, Clock, Gauge, HardDrive, KeyRound, Loader2, Minus, Plus, ShieldBan, type LucideIcon } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useCreateUser, useGroups, useUpdateUser, useUser } from '@/api/hooks';
import type { Group, Inbound, ResetStrategy, UserInput, UserLimits } from '@/api/types';
import { Sheet } from '@/components/Overlay';
import { Field, PasswordInput, Skeleton, Switch } from '@/components/misc';
import { Select } from '@/components/Select';
import { useI18n, type MessageKey } from '@/i18n';
import { parseDays, persianDigits } from '@/lib/days';
import { isProxyLogin } from '@/lib/protocols';
import { GB, USERNAME_RE, proxyPassProblem, proxyUserProblem } from '@/lib/user';
import { ApiError } from '@/api/client';
import { toast } from '@/store/feedback';
import { LimitField } from './LimitField';
import { FirewallField, type FwError } from './FirewallField';
import { addEntries, type FwReason } from '@/lib/firewall';
import s from './Users.module.css';

type ExpMode = 'keep' | 'never' | 'days' | 'hold';
export const RESETS: ResetStrategy[] = ['no_reset', 'day', 'week', 'month'];
export const LIMITS = [10, 30, 50, 100];
export const DAYS = [7, 30, 90, 365];
export const DAILY = [1, 2, 5];
/** Weekdays in the panel's order (Saturday first); the value is the JS/Go weekday (0 = Sunday). */
export const WEEK = [6, 0, 1, 2, 3, 4, 5];
export const ALL_DAYS = 0x7f;
export const CLOCKS = Array.from({ length: 48 }, (_, i) => `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`);
type SecKey = 'data' | 'time' | 'conn' | 'speed' | 'access' | 'firewall';

export const round1 = (v: number) => Math.round(v * 10) / 10;
export const gb = (bytes: number) => Math.round((bytes / GB) * 100) / 100;

/**
 * A free number of days (any whole number from 1, e.g. 1), typed with the
 * Persian or Latin keyboard; 0 while empty, which the form treats as invalid.
 */
export function DaysInput({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const { t, lang } = useI18n();
  const [draft, setDraft] = useState(value > 0 ? String(value) : '');
  useEffect(() => {
    setDraft((d) => (Number(d || 0) === value ? d : value > 0 ? String(value) : ''));
  }, [value]);
  return (
    <label className={s.daysBox}>
      <input
        className={`num ${s.daysInput}`}
        type="text"
        inputMode="numeric"
        pattern="[0-9]*"
        autoComplete="off"
        maxLength={5}
        placeholder={t('form.daysPlaceholder')}
        value={lang === 'fa' ? persianDigits(draft) : draft}
        onChange={(e) => {
          const { draft: raw, days } = parseDays(e.target.value);
          setDraft(raw);
          onChange(days);
        }}
        aria-label={t('form.daysLabel')}
      />
      {draft !== '' && <span className={s.daysUnit}>{t('common.days')}</span>}
    </label>
  );
}

/** − value + stepper; laid out left to right in both languages. */
export function Stepper({ value, onChange, step = 1, min = 0, max = 1000, format, label }: { value: number; onChange: (v: number) => void; step?: number; min?: number; max?: number; format: (v: number) => ReactNode; label: string }) {
  const { t } = useI18n();
  const set = (v: number) => onChange(Math.min(max, Math.max(min, step < 1 ? round1(v) : Math.round(v))));
  return (
    <div className={s.stepper} dir="ltr" role="group" aria-label={label}>
      <button type="button" className={s.stepBtn} onClick={() => set(value - step)} disabled={value <= min} aria-label={t('form.decrease')}>
        <Minus size={16} />
      </button>
      <output className={`${s.stepVal} num`} aria-live="polite">
        {format(value)}
      </output>
      <button type="button" className={s.stepBtn} onClick={() => set(value + step)} disabled={value >= max} aria-label={t('form.increase')}>
        <Plus size={16} />
      </button>
    </div>
  );
}

/** A collapsible form section with its current values summarised in the header. */
export function Sec<K extends string = SecKey>({ id, icon: Icon, title, summary, open, onToggle, children }: { id: K; icon: LucideIcon; title: string; summary: string; open: boolean; onToggle: (k: K) => void; children: ReactNode }) {
  return (
    <section className={`${s.sec} ${open ? s.secOpen : ''}`}>
      <button type="button" className={s.secHead} aria-expanded={open} aria-controls={`sec-${id}`} onClick={() => onToggle(id)}>
        <span className={s.secIcon}>
          <Icon size={16} />
        </span>
        <strong>{title}</strong>
        <span className={s.secSum}>{summary}</span>
        <ChevronDown size={17} className={s.secChev} />
      </button>
      {open && (
        <div className={s.secBody} id={`sec-${id}`}>
          {children}
        </div>
      )}
    </section>
  );
}

/** Number chips plus a free number input (0 shows the zero label). */
export function NumChips({ value, onChange, options, zero, unit, label, step = 'any' }: { value: number; onChange: (v: number) => void; options: number[]; zero: string; unit: string; label: string; step?: string }) {
  const { n } = useI18n();
  return (
    <div className={s.chipRow}>
      {options.map((g) => (
        <button type="button" key={g} className="pill" aria-pressed={value === g} onClick={() => onChange(g)}>
          {g === 0 ? zero : `${n(g)} ${unit}`}
        </button>
      ))}
      <input
        className={`input num ${s.inlineNum}`}
        type="number"
        inputMode="decimal"
        min={0}
        step={step}
        value={value}
        onChange={(e) => onChange(Math.max(0, Number(e.target.value) || 0))}
        aria-label={label}
      />
    </div>
  );
}

export const NO_LIMITS: UserLimits = {
  ip_limit: 0, device_limit: 0, require_hwid: false, speed_limit_down: 0, speed_limit_up: 0, traffic_multiplier: 1,
  daily_limit: 0, block_torrent: false, window_from: '', window_to: '', window_days: 0,
};

interface Props {
  open: boolean;
  editId: number | null;
  onClose: () => void;
  onSaved: (id: number) => void;
  inbounds: Inbound[];
}

export function UserForm({ open, editId, onClose, onSaved, inbounds }: Props) {
  const { t, n, date } = useI18n();
  const existing = useUser(editId);
  const create = useCreateUser();
  const update = useUpdateUser();
  const editing = !!editId;

  const [username, setUsername] = useState('');
  const [limitGb, setLimitGb] = useState<number>(30);
  const [expMode, setExpMode] = useState<ExpMode>('days');
  const [days, setDays] = useState(30);
  const [holdDays, setHoldDays] = useState(30);
  const [reset, setReset] = useState<ResetStrategy>('no_reset');
  const [ibs, setIbs] = useState<number[]>([]);
  const [note, setNote] = useState('');
  const [active, setActive] = useState(true);
  const [lim, setLim] = useState<UserLimits>(NO_LIMITS);
  const [winOn, setWinOn] = useState(false);
  const [groupId, setGroupId] = useState<number | null>(null);
  const [proxyUser, setProxyUser] = useState('');
  const [proxyPass, setProxyPass] = useState('');
  const [proxyTaken, setProxyTaken] = useState(false);
  const [fw, setFw] = useState<string[]>([]);
  const [fwDraft, setFwDraft] = useState('');
  const [fwErr, setFwErr] = useState<FwError | null>(null);
  const [openSecs, setOpenSecs] = useState<Set<SecKey>>(new Set(['data', 'time']));
  const groupsQ = useGroups(open);
  const groups = groupsQ.data ?? [];
  const L = <K extends keyof UserLimits>(k: K, v: UserLimits[K]) => setLim((x) => ({ ...x, [k]: v }));
  const toggleSec = (k: SecKey) => setOpenSecs((x) => {
    const y = new Set(x);
    if (y.has(k)) y.delete(k);
    else y.add(k);
    return y;
  });
  const [touched, setTouched] = useState(false);
  const [serverErr, setServerErr] = useState<string | null>(null);

  // initialise / reset form on open
  useEffect(() => {
    if (!open) return;
    setTouched(false);
    setServerErr(null);
    if (editing) {
      const u = existing.data;
      if (!u) return;
      setUsername(u.username);
      setLimitGb(u.data_limit ? Math.round((u.data_limit / GB) * 100) / 100 : 0);
      setExpMode('keep');
      setDays(30);
      setHoldDays(u.on_hold_days ?? 30);
      setReset(u.reset_strategy);
      setIbs(u.inbound_ids);
      setNote(u.note);
      setActive(u.status !== 'disabled');
      setLim({
        ip_limit: u.ip_limit ?? 0, device_limit: u.device_limit ?? 0, require_hwid: !!u.require_hwid, speed_limit_down: u.speed_limit_down ?? 0,
        speed_limit_up: u.speed_limit_up ?? 0, traffic_multiplier: u.traffic_multiplier || 1, daily_limit: u.daily_limit ?? 0, block_torrent: !!u.block_torrent,
        window_from: u.window_from || '08:00', window_to: u.window_to || '18:00', window_days: u.window_days || ALL_DAYS,
      });
      setWinOn(!!(u.window_from && u.window_to) || (!!u.window_days && (u.window_days & ALL_DAYS) !== ALL_DAYS));
      setGroupId(u.group_id ?? null);
      setProxyUser(u.proxy_username ?? '');
      setProxyPass(u.proxy_password ?? '');
      setFw(u.firewall ?? []);
    } else {
      setUsername('');
      setLimitGb(30);
      setExpMode('days');
      setDays(30);
      setHoldDays(30);
      setReset('no_reset');
      setIbs(inbounds.filter((i) => i.enabled).map((i) => i.id));
      setNote('');
      setActive(true);
      setLim({ ...NO_LIMITS, window_from: '08:00', window_to: '18:00', window_days: ALL_DAYS });
      setWinOn(false);
      setGroupId(null);
      setProxyUser('');
      setProxyPass('');
      setFw([]);
    }
    setFwDraft('');
    setFwErr(null);
    setProxyTaken(false);
    setOpenSecs(new Set(['data', 'time']));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing, existing.data?.id]);

  // a new user gets every enabled inbound once the list arrives (the sheet can open before it loads)
  useEffect(() => {
    if (open && !editing && !touched && ibs.length === 0 && inbounds.length) setIbs(inbounds.filter((i) => i.enabled).map((i) => i.id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing, inbounds.length]);

  /** Picking a group on a new user fills the form with the group's defaults. */
  const pickGroup = (id: number | null) => {
    setGroupId(id);
    if (editing || id === null) return;
    const g: Group | undefined = groups.find((x) => x.id === id);
    if (!g) return;
    const d = g.defaults ?? {};
    if (d.data_limit !== undefined) setLimitGb(gb(d.data_limit));
    if (d.expire_days) {
      setExpMode('days');
      setDays(d.expire_days);
    } else if (d.on_hold_days) {
      setExpMode('hold');
      setHoldDays(d.on_hold_days);
    }
    if (d.reset_strategy) setReset(d.reset_strategy);
    if (g.inbound_ids?.length) setIbs(g.inbound_ids.filter((x) => inbounds.some((i) => i.id === x)));
    setLim((x) => {
      const y = { ...x };
      for (const k of Object.keys(NO_LIMITS) as Array<keyof UserLimits>) if (d[k] !== undefined) (y as Record<string, unknown>)[k] = d[k];
      return y;
    });
    if (d.window_from && d.window_to) setWinOn(true);
  };

  /** The custom proxy login applies only to HTTP, Mixed and SOCKS inbounds: shown while one is selected. */
  const proxyOn = ibs.some((id) => isProxyLogin(inbounds.find((i) => i.id === id)?.protocol ?? ''));
  const pu = proxyUserProblem(proxyUser);
  const pp = proxyPassProblem(proxyPass);
  const proxyUserErr = !proxyOn ? null : proxyTaken ? t('form.proxyUserTaken') : pu === 'chars' ? t('form.proxyUserInvalid') : pu === 'auto' ? t('form.proxyUserAuto') : null;
  const proxyPassErr = !proxyOn ? null : pp === 'short' ? t('form.proxyPassShort') : pp === 'chars' ? t('form.proxyPassInvalid') : null;
  const daysErr = winOn && (lim.window_days & ALL_DAYS) === 0 ? t('form.weekdaysErr') : null;
  const userErr = !USERNAME_RE.test(username) ? (username ? t('form.usernameInvalid') : t('form.required')) : null;
  const ibErr = ibs.length === 0 ? t('form.inboundsHint') : null;
  const valid = !userErr && !ibErr && !daysErr && !proxyUserErr && !proxyPassErr && limitGb >= 0 && (expMode !== 'days' || days > 0) && (expMode !== 'hold' || holdDays > 0);
  const busy = create.isPending || update.isPending;
  const expDate = new Date(Date.now() + days * 86_400_000);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!valid) {
      setOpenSecs((x) => new Set([...x, ...(ibErr || proxyUserErr || proxyPassErr ? (['access'] as SecKey[]) : []), ...(daysErr ? (['time'] as SecKey[]) : [])]));
      return;
    }
    setServerErr(null);
    // text typed in the firewall box but not added yet is added now (or stops the save)
    let fwList = fw;
    if (fwDraft.trim()) {
      const r = addEntries(fw, fwDraft);
      if (r.error) {
        setFwErr(r.error);
        setOpenSecs((x) => new Set([...x, 'firewall']));
        return;
      }
      fwList = r.list;
      setFw(r.list);
      setFwDraft('');
    }
    const body: Partial<UserInput> = {
      username,
      data_limit: Math.round(limitGb * GB),
      reset_strategy: reset,
      note,
      inbound_ids: ibs,
      ...lim,
      traffic_multiplier: round1(lim.traffic_multiplier || 1),
      daily_limit: Math.round(lim.daily_limit),
      group_id: groupId,
      firewall: fwList,
    };
    // hidden (no HTTP/Mixed/SOCKS inbound selected): the stored login is kept as it is
    if (proxyOn) Object.assign(body, { proxy_username: proxyUser.trim(), proxy_password: proxyPass.trim() });
    // the window is sent only when it actually restricts something
    const wdays = (lim.window_days & ALL_DAYS) === ALL_DAYS ? 0 : lim.window_days & ALL_DAYS;
    const hours = lim.window_from !== lim.window_to;
    if (!winOn || (!hours && !wdays)) Object.assign(body, { window_from: '', window_to: '', window_days: 0 });
    else Object.assign(body, { window_from: hours ? lim.window_from : '', window_to: hours ? lim.window_to : '', window_days: wdays });
    if (expMode === 'never') Object.assign(body, { expire_at: null, on_hold_days: null });
    if (expMode === 'days') Object.assign(body, { expire_at: expDate.toISOString(), on_hold_days: null });
    if (expMode === 'hold') Object.assign(body, { expire_at: null, on_hold_days: holdDays });
    const onError = (err: unknown) => {
      if (err instanceof ApiError && err.status === 400 && err.details?.field === 'firewall') {
        setFwErr({ entry: String(err.details.entry ?? ''), reason: (err.details.reason as FwReason) ?? 'bad_domain' });
        setOpenSecs((x) => new Set([...x, 'firewall']));
        return;
      }
      if (err instanceof ApiError && err.status === 409 && err.details?.field === 'proxy_username') {
        setProxyTaken(true);
        setOpenSecs((x) => new Set([...x, 'access']));
        return;
      }
      setServerErr((err as Error).message);
    };
    if (editing && editId) {
      update.mutate(
        { id: editId, patch: { ...body, status: active ? 'active' : 'disabled' } },
        {
          onSuccess: () => {
            toast.success(t('user.saved'));
            onSaved(editId);
          },
          onError,
        },
      );
    } else {
      create.mutate(
        { ...({ expire_at: null, on_hold_days: null } as Pick<UserInput, 'expire_at' | 'on_hold_days'>), ...(body as Partial<UserInput>) } as UserInput,
        {
          onSuccess: (u) => {
            toast.success(t('user.created', { name: u.username }));
            onSaved(u.id);
          },
          onError,
        },
      );
    }
  };

  const clockOpts = useMemo(() => CLOCKS.map((c) => ({ value: c, label: n(Number(c.slice(0, 2)), { minimumIntegerDigits: 2 }) + ':' + n(Number(c.slice(3)), { minimumIntegerDigits: 2 }), text: c })), [n]);
  const groupOpts = useMemo(() => [{ value: 0, label: t('form.noGroup') }, ...groups.map((g) => ({ value: g.id, label: g.name, hint: n(g.users) }))], [groups, t, n]);
  const sep = ' · ';
  const dataSum = [limitGb ? `${n(limitGb)} GB` : t('common.unlimited'), reset !== 'no_reset' ? t(`form.reset.${reset}`) : '', lim.traffic_multiplier !== 1 ? `×${n(lim.traffic_multiplier)}` : '', lim.daily_limit ? `${n(gb(lim.daily_limit))} GB/${t('form.reset.day')}` : '']
    .filter(Boolean)
    .join(sep);
  const timeSum = [
    expMode === 'keep' ? t('form.expiry.keep') : expMode === 'never' ? t('form.expiry.never') : expMode === 'hold' ? `${t('form.expiry.onHold')} (${n(holdDays)} ${t('common.days')})` : `${n(days)} ${t('common.days')}`,
    winOn ? (lim.window_from !== lim.window_to ? `\u2066${clockOpts.find((c) => c.value === lim.window_from)?.label}–${clockOpts.find((c) => c.value === lim.window_to)?.label}\u2069` : t('badge.window')) : '',
  ]
    .filter(Boolean)
    .join(sep);
  const connSum = [lim.ip_limit ? t('form.sum.ips', { n: n(lim.ip_limit) }) : '', lim.device_limit ? t('form.sum.devices', { n: n(lim.device_limit) }) : '', lim.require_hwid ? 'HWID' : ''].filter(Boolean).join(sep) || t('form.sum.none');
  const speedSum =
    lim.speed_limit_down || lim.speed_limit_up ? `↓ ${lim.speed_limit_down ? n(lim.speed_limit_down) : '∞'} · ↑ ${lim.speed_limit_up ? n(lim.speed_limit_up) : '∞'} Mbps` : t('form.sum.none');
  const accessSum = [groupId ? groups.find((g) => g.id === groupId)?.name ?? '' : '', t('form.sum.inbounds', { n: n(ibs.length) }), lim.block_torrent ? t('badge.torrent') : ''].filter(Boolean).join(sep);

  const fwSum = fw.length ? t('form.fwCount', { n: n(fw.length) }) : t('form.sum.none');
  const awgOn = ibs.some((id) => inbounds.find((i) => i.id === id)?.protocol === 'amneziawg');

  const modes: ExpMode[] = editing ? ['keep', 'days', 'never', 'hold'] : ['days', 'never', 'hold'];

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={editing ? t('user.edit') : t('users.new')}
      subtitle={editing && existing.data ? <span className="ltr">{existing.data.username}</span> : undefined}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="user-form" className="btn btn--primary" disabled={busy || (touched && !valid)}>
            {busy ? <Loader2 size={17} className="spin" /> : <Check size={17} />}
            {busy ? t('common.saving') : editing ? t('common.save') : t('common.create')}
          </button>
        </>
      }
    >
      {editing && !existing.data ? (
        <div style={{ display: 'grid', gap: 14 }}>
          <Skeleton h={70} r={16} />
          <Skeleton h={70} r={16} />
          <Skeleton h={120} r={16} />
        </div>
      ) : (
        <form id="user-form" className={s.form} onSubmit={submit} noValidate>
          {serverErr && (
            <div className={s.formErr} role="alert">
              {serverErr}
            </div>
          )}
          <Field label={t('form.username')} hint={t('form.usernameHint')} error={touched ? userErr ?? undefined : undefined}>
            <input
              className="input ltr"
              value={username}
              onChange={(e) => setUsername(e.target.value.replace(/\s/g, ''))}
              autoCapitalize="none"
              autoComplete="off"
              spellCheck={false}
              aria-invalid={touched && !!userErr}
              data-autofocus
              maxLength={32}
            />
          </Field>

          <Sec id="data" icon={HardDrive} title={t('form.sec.data')} summary={dataSum} open={openSecs.has('data')} onToggle={toggleSec}>
            <LimitField label={t('form.dataLimit')} unit="GB" decimals fallback={30} value={limitGb} onChange={setLimitGb} presets={LIMITS} hint={t('form.dataLimitHint')} />

            <fieldset className={s.fs}>
              <legend className="field__label">{t('user.resetStrategy')}</legend>
              <div className="seg seg--wrap" role="radiogroup" aria-label={t('user.resetStrategy')}>
                {RESETS.map((r) => (
                  <button type="button" role="radio" key={r} aria-checked={reset === r} onClick={() => setReset(r)}>
                    {t(`form.reset.${r}`)}
                  </button>
                ))}
              </div>
            </fieldset>

            <div className={s.fs}>
              <span className="field__label">{t('form.multiplier')}</span>
              <Stepper
                value={lim.traffic_multiplier}
                onChange={(v) => L('traffic_multiplier', v)}
                step={0.1}
                min={0.1}
                max={100}
                label={t('form.multiplier')}
                format={(v) => `×${n(v, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}`}
              />
              <span className="field__hint">{t('form.multiplierHint')}</span>
            </div>
            <LimitField label={t('form.dailyLimit')} unit="GB" decimals fallback={1} value={gb(lim.daily_limit)} onChange={(v) => L('daily_limit', Math.round(v * GB))} presets={DAILY} hint={t('form.dailyLimitHint')} />
          </Sec>

          <Sec id="time" icon={Clock} title={t('form.sec.time')} summary={timeSum} open={openSecs.has('time')} onToggle={toggleSec}>
            <fieldset className={s.fs}>
              <legend className="field__label">{t('form.expiry')}</legend>
              <div className="seg seg--wrap" role="radiogroup" aria-label={t('form.expiry')}>
                {modes.map((m) => (
                  <button type="button" role="radio" key={m} aria-checked={expMode === m} onClick={() => setExpMode(m)}>
                    {t(m === 'keep' ? 'form.expiry.keep' : m === 'never' ? 'form.expiry.never' : m === 'days' ? 'form.expiry.days' : 'form.expiry.onHold')}
                  </button>
                ))}
              </div>
              {expMode === 'days' && (
                <div className={s.chipRow}>
                  {DAYS.map((d) => (
                    <button type="button" key={d} className="pill" aria-pressed={days === d} onClick={() => setDays(d)}>
                      {n(d)} {t('common.days')}
                    </button>
                  ))}
                  <DaysInput value={days} onChange={setDays} />
                  {days > 0 ? (
                    <span className="field__hint" style={{ width: '100%' }}>
                      {t('form.expiresOn', { date: date(expDate, { year: 'numeric', month: 'long', day: 'numeric' }) })}
                    </span>
                  ) : (
                    touched && <span className="field__error" style={{ width: '100%' }}>{t('form.required')}</span>
                  )}
                </div>
              )}
              {expMode === 'hold' && (
                <div className={s.chipRow}>
                  {[7, 14, 30, 90].map((d) => (
                    <button type="button" key={d} className="pill" aria-pressed={holdDays === d} onClick={() => setHoldDays(d)}>
                      {n(d)} {t('common.days')}
                    </button>
                  ))}
                  <DaysInput value={holdDays} onChange={setHoldDays} />
                  {touched && holdDays <= 0 && <span className="field__error" style={{ width: '100%' }}>{t('form.required')}</span>}
                </div>
              )}
            </fieldset>

            <fieldset className={s.fs}>
              <legend className="field__label">{t('form.window')}</legend>
              <div className="seg seg--wrap" role="radiogroup" aria-label={t('form.window')}>
                <button type="button" role="radio" aria-checked={!winOn} onClick={() => setWinOn(false)}>
                  {t('form.windowAlways')}
                </button>
                <button type="button" role="radio" aria-checked={winOn} onClick={() => setWinOn(true)}>
                  {t('form.windowLimited')}
                </button>
              </div>
              {winOn && (
                <>
                  <div className={`${s.twoCol} ${s.twoColKeep}`}>
                    <Field label={t('form.from')}>
                      <Select value={lim.window_from} onChange={(v) => L('window_from', v)} options={clockOpts} ariaLabel={t('form.from')} ltr />
                    </Field>
                    <Field label={t('form.to')}>
                      <Select value={lim.window_to} onChange={(v) => L('window_to', v)} options={clockOpts} ariaLabel={t('form.to')} ltr />
                    </Field>
                  </div>
                  <span className="field__label">{t('form.weekdays')}</span>
                  <div className={s.chipRow} role="group" aria-label={t('form.weekdays')}>
                    {WEEK.map((d) => {
                      const on = (lim.window_days & (1 << d)) !== 0;
                      return (
                        <button type="button" key={d} className={`pill ${s.dayPill}`} aria-pressed={on} onClick={() => L('window_days', lim.window_days ^ (1 << d))}>
                          {t(`wd.${d}` as MessageKey)}
                        </button>
                      );
                    })}
                  </div>
                  {touched && daysErr ? <span className="field__error">{daysErr}</span> : <span className="field__hint">{t('form.windowHint')}</span>}
                </>
              )}
            </fieldset>
          </Sec>

          <Sec id="conn" icon={Cable} title={t('form.sec.conn')} summary={connSum} open={openSecs.has('conn')} onToggle={toggleSec}>
            <div className={s.twoCol}>
              <div className={s.fs}>
                <span className="field__label">{t('form.ipLimit')}</span>
                <Stepper value={lim.ip_limit} onChange={(v) => L('ip_limit', v)} label={t('form.ipLimit')} format={(v) => (v === 0 ? t('common.unlimited') : n(v))} />
                <span className="field__hint">{t('form.ipLimitHint')}</span>
              </div>
              <div className={s.fs}>
                <span className="field__label">{t('form.deviceLimit')}</span>
                <Stepper value={lim.device_limit} onChange={(v) => L('device_limit', v)} label={t('form.deviceLimit')} format={(v) => (v === 0 ? t('common.unlimited') : n(v))} />
                <span className="field__hint">{t('form.deviceLimitHint')}</span>
              </div>
            </div>
            <div className={s.switchRow}>
              <span>
                <strong>{t('form.requireHwid')}</strong>
                <small>{t('form.requireHwidHint')}</small>
              </span>
              <Switch checked={lim.require_hwid} onChange={(v) => L('require_hwid', v)} label={t('form.requireHwid')} />
            </div>
          </Sec>

          <Sec id="speed" icon={Gauge} title={t('form.sec.speed')} summary={speedSum} open={openSecs.has('speed')} onToggle={toggleSec}>
            <LimitField unit="Mbps" label={t('form.speedDown')} value={lim.speed_limit_down} onChange={(v) => L('speed_limit_down', v)} />
            <LimitField unit="Mbps" label={t('form.speedUp')} value={lim.speed_limit_up} onChange={(v) => L('speed_limit_up', v)} hint={t('form.speedHint')} />
          </Sec>

          <Sec id="access" icon={KeyRound} title={t('form.sec.access')} summary={accessSum} open={openSecs.has('access')} onToggle={toggleSec}>
            <Field label={t('form.group')} hint={!editing ? t('form.groupHint') : undefined}>
              <Select<number> value={groupId ?? 0} onChange={(v) => pickGroup(v === 0 ? null : v)} options={groupOpts} ariaLabel={t('form.group')} />
            </Field>
            <fieldset className={s.fs}>
              <legend className="field__label">{t('form.inbounds')}</legend>
              <div className={s.ibGrid}>
                {inbounds.map((ib) => {
                  const on = ibs.includes(ib.id);
                  return (
                    <button
                      type="button"
                      key={ib.id}
                      role="checkbox"
                      aria-checked={on}
                      className={`${s.ibOpt} ${on ? s.ibOn : ''}`}
                      onClick={() => setIbs((x) => (on ? x.filter((y) => y !== ib.id) : [...x, ib.id]))}
                    >
                      <span className={s.ibCheck}>{on && <Check size={13} strokeWidth={3} />}</span>
                      <span className={s.ibText}>
                        <strong>{ib.remark}</strong>
                        <small className="ltr">
                          {ib.protocol} · {ib.network} · :{ib.port}
                        </small>
                      </span>
                      {!ib.enabled && <span className="chip chip--disabled">{t('common.disabled')}</span>}
                    </button>
                  );
                })}
              </div>
              {touched && ibErr && <span className="field__error">{ibErr}</span>}
            </fieldset>

            {proxyOn && (
              <fieldset className={s.fs}>
                <legend className="field__label">{t('form.proxyLogin')}</legend>
                <span className="field__hint">{t('form.proxyLoginHint')}</span>
                <div className={s.twoCol}>
                  <Field label={t('form.proxyUser')} error={(touched || proxyTaken) && proxyUserErr ? proxyUserErr : undefined} optional>
                    <input
                      className="input ltr mono"
                      value={proxyUser}
                      onChange={(e) => {
                        setProxyUser(e.target.value.replace(/\s/g, ''));
                        setProxyTaken(false);
                      }}
                      placeholder={t('common.auto')}
                      autoCapitalize="none"
                      autoComplete="off"
                      spellCheck={false}
                      maxLength={32}
                      aria-invalid={(touched || proxyTaken) && !!proxyUserErr}
                    />
                  </Field>
                  <Field label={t('form.proxyPass')} error={touched && proxyPassErr ? proxyPassErr : undefined} optional>
                    <PasswordInput
                      className="input ltr mono"
                      value={proxyPass}
                      onChange={(e) => setProxyPass(e.target.value.replace(/\s/g, ''))}
                      placeholder={t('common.auto')}
                      autoComplete="new-password"
                      maxLength={64}
                      aria-invalid={touched && !!proxyPassErr}
                    />
                  </Field>
                </div>
              </fieldset>
            )}

            <div className={s.switchRow}>
              <span>
                <strong>{t('form.blockTorrent')}</strong>
                <small>{t('form.blockTorrentHint')}</small>
              </span>
              <Switch checked={lim.block_torrent} onChange={(v) => L('block_torrent', v)} label={t('form.blockTorrent')} />
            </div>
          </Sec>

          <Sec id="firewall" icon={ShieldBan} title={t('form.sec.firewall')} summary={fwSum} open={openSecs.has('firewall')} onToggle={toggleSec}>
            <FirewallField list={fw} onChange={setFw} draft={fwDraft} onDraft={setFwDraft} error={fwErr} onError={setFwErr} awg={awgOn} />
          </Sec>

          <Field label={t('form.note')} optional>
            <textarea className="input" value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('form.notePlaceholder')} rows={3} />
          </Field>

          {editing && (
            <div className={s.switchRow}>
              <span>
                <strong>{t('form.status')}</strong>
                <small>{active ? t('common.enabled') : t('common.disabled')}</small>
              </span>
              <Switch checked={active} onChange={setActive} label={t('form.status')} />
            </div>
          )}
        </form>
      )}
    </Sheet>
  );
}
