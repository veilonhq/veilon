import { Cable, Check, Clock, Gauge, HardDrive, KeyRound, Loader2, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useDeleteGroup, useSaveGroup } from '@/api/hooks';
import type { Group, GroupDefaults, Inbound, ResetStrategy, UserLimits } from '@/api/types';
import { Sheet } from '@/components/Overlay';
import { Field, Switch } from '@/components/misc';
import { useI18n } from '@/i18n';
import { GB } from '@/lib/user';
import { confirmAction, toast } from '@/store/feedback';
import { DAILY, DAYS, LIMITS, NO_LIMITS, NumChips, RESETS, Sec, Stepper, gb, round1 } from '../users/UserForm';
import { LimitField } from '../users/LimitField';
import u from '../users/Users.module.css';
import s from '../pages.module.css';

/** Preset group colours (the API stores #rrggbb). */
export const GROUP_COLORS = ['#8b7cff', '#4f8cff', '#22b8cf', '#34c37b', '#f2a93b', '#ff7a45', '#f0506e', '#9aa3b2'];

type Exp = 'never' | 'days' | 'hold';
type SecKey = 'access' | 'data' | 'time' | 'conn' | 'speed';
/** The default keys this form edits; anything else in `defaults` (a time window) is kept as is. */
type Lim = Pick<UserLimits, 'ip_limit' | 'device_limit' | 'require_hwid' | 'speed_limit_down' | 'speed_limit_up' | 'traffic_multiplier' | 'daily_limit' | 'block_torrent'>;

interface Props {
  open: boolean;
  group: Group | null;
  onClose: () => void;
  inbounds: Inbound[];
}

export function GroupForm({ open, group, onClose, inbounds }: Props) {
  const { t, n } = useI18n();
  const save = useSaveGroup();
  const del = useDeleteGroup();
  const editing = !!group;

  const [name, setName] = useState('');
  const [color, setColor] = useState(GROUP_COLORS[0]);
  const [adBlock, setAdBlock] = useState(false);
  const [ibs, setIbs] = useState<number[]>([]);
  const [limitGb, setLimitGb] = useState(0);
  const [reset, setReset] = useState<ResetStrategy>('no_reset');
  const [exp, setExp] = useState<Exp>('never');
  const [days, setDays] = useState(30);
  const [hold, setHold] = useState(30);
  const [lim, setLim] = useState<Lim>(NO_LIMITS);
  const [openSecs, setOpenSecs] = useState<Set<SecKey>>(new Set(['access', 'data']));
  const [touched, setTouched] = useState(false);
  const [serverErr, setServerErr] = useState<string | null>(null);
  const L = <K extends keyof Lim>(k: K, v: Lim[K]) => setLim((x) => ({ ...x, [k]: v }));
  const toggleSec = (k: SecKey) =>
    setOpenSecs((x) => {
      const y = new Set(x);
      if (y.has(k)) y.delete(k);
      else y.add(k);
      return y;
    });

  useEffect(() => {
    if (!open) return;
    setTouched(false);
    setServerErr(null);
    const d: GroupDefaults = group?.defaults ?? {};
    setName(group?.name ?? '');
    setColor(group?.color || GROUP_COLORS[0]);
    setAdBlock(!!group?.ad_block);
    setIbs(group ? group.inbound_ids : inbounds.filter((i) => i.enabled).map((i) => i.id));
    setLimitGb(d.data_limit ? gb(d.data_limit) : group ? 0 : 30);
    setReset(d.reset_strategy ?? 'no_reset');
    setExp(d.expire_days ? 'days' : d.on_hold_days ? 'hold' : group ? 'never' : 'days');
    setDays(d.expire_days || 30);
    setHold(d.on_hold_days || 30);
    setLim({
      ip_limit: d.ip_limit ?? 0,
      device_limit: d.device_limit ?? 0,
      require_hwid: !!d.require_hwid,
      speed_limit_down: d.speed_limit_down ?? 0,
      speed_limit_up: d.speed_limit_up ?? 0,
      traffic_multiplier: d.traffic_multiplier || 1,
      daily_limit: d.daily_limit ?? 0,
      block_torrent: !!d.block_torrent,
    });
    setOpenSecs(new Set(['access', 'data']));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, group?.id]);

  const nameErr = !name.trim() ? t('form.required') : name.trim().length > 48 ? t('grp.nameLong') : null;
  const valid = !nameErr && (exp !== 'days' || days > 0) && (exp !== 'hold' || hold > 0);

  const buildDefaults = (): GroupDefaults => {
    // start from what the group has, so keys this form does not show survive
    const d: GroupDefaults = { ...(group?.defaults ?? {}) };
    const put = <K extends keyof GroupDefaults>(k: K, v: GroupDefaults[K] | undefined) => {
      if (v === undefined) delete d[k];
      else d[k] = v;
    };
    put('data_limit', Math.round(limitGb * GB));
    put('reset_strategy', reset !== 'no_reset' ? reset : undefined);
    put('expire_days', exp === 'days' ? days : undefined);
    put('on_hold_days', exp === 'hold' ? hold : undefined);
    put('ip_limit', lim.ip_limit || undefined);
    put('device_limit', lim.device_limit || undefined);
    put('require_hwid', lim.require_hwid || undefined);
    put('speed_limit_down', lim.speed_limit_down || undefined);
    put('speed_limit_up', lim.speed_limit_up || undefined);
    put('traffic_multiplier', lim.traffic_multiplier && round1(lim.traffic_multiplier) !== 1 ? round1(lim.traffic_multiplier) : undefined);
    put('daily_limit', lim.daily_limit ? Math.round(lim.daily_limit) : undefined);
    put('block_torrent', lim.block_torrent || undefined);
    return d;
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!valid) return;
    setServerErr(null);
    save.mutate(
      {
        id: group?.id ?? null,
        body: {
          name: name.trim(),
          color,
          ad_block: adBlock,
          inbound_ids: ibs,
          defaults: buildDefaults(),
        },
      },
      {
        onSuccess: (g) => {
          toast.success(editing ? t('grp.saved') : t('grp.created', { name: g.name }));
          onClose();
        },
        onError: (x) => setServerErr((x as Error).message),
      },
    );
  };

  const remove = async () => {
    if (!group) return;
    const ok = await confirmAction({
      title: t('grp.deleteTitle', { name: group.name }),
      body: group.users ? t('grp.deleteBody', { n: group.users }) : undefined,
      danger: true,
      confirmLabel: t('common.delete'),
    });
    if (!ok) return;
    del.mutate(group.id, {
      onSuccess: () => toast.success(t('grp.deleted')),
      onError: (x) => toast.error(t('common.error'), (x as Error).message),
    });
    onClose();
  };

  const sep = ' · ';
  const accessSum = [t('form.sum.inbounds', { n: n(ibs.length) }), lim.block_torrent ? t('badge.torrent') : ''].filter(Boolean).join(sep);
  const dataSum = [
    limitGb ? `${n(limitGb)} GB` : t('common.unlimited'),
    reset !== 'no_reset' ? t(`form.reset.${reset}`) : '',
    lim.traffic_multiplier !== 1 ? `×${n(lim.traffic_multiplier)}` : '',
    lim.daily_limit ? `${n(gb(lim.daily_limit))} GB/${t('form.reset.day')}` : '',
  ]
    .filter(Boolean)
    .join(sep);
  const timeSum = exp === 'never' ? t('form.expiry.never') : exp === 'hold' ? `${t('form.expiry.onHold')} (${n(hold)} ${t('common.days')})` : `${n(days)} ${t('common.days')}`;
  const connSum =
    [lim.ip_limit ? t('form.sum.ips', { n: n(lim.ip_limit) }) : '', lim.device_limit ? t('form.sum.devices', { n: n(lim.device_limit) }) : '', lim.require_hwid ? 'HWID' : '']
      .filter(Boolean)
      .join(sep) || t('form.sum.none');
  const speedSum =
    lim.speed_limit_down || lim.speed_limit_up
      ? `↓ ${lim.speed_limit_down ? n(lim.speed_limit_down) : '∞'} · ↑ ${lim.speed_limit_up ? n(lim.speed_limit_up) : '∞'} Mbps`
      : t('form.sum.none');
  const busy = save.isPending;

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={editing ? t('grp.edit') : t('grp.new')}
      subtitle={editing ? group?.name : undefined}
      footer={
        <>
          {editing && (
            <button type="button" className="btn btn--danger-ghost" onClick={remove} style={{ marginInlineEnd: 'auto' }}>
              <Trash2 size={16} />
              {t('common.delete')}
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="group-form" className="btn btn--primary" disabled={busy || (touched && !valid)}>
            {busy ? <Loader2 size={17} className="spin" /> : <Check size={17} />}
            {busy ? t('common.saving') : editing ? t('common.save') : t('common.create')}
          </button>
        </>
      }
    >
      <form id="group-form" className={u.form} onSubmit={submit} noValidate>
        {serverErr && (
          <div className={u.formErr} role="alert">
            {serverErr}
          </div>
        )}
        <Field label={t('grp.name')} error={touched ? (nameErr ?? undefined) : undefined}>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={48} data-autofocus aria-invalid={touched && !!nameErr} />
        </Field>

        <fieldset className={u.fs}>
          <legend className="field__label">{t('grp.color')}</legend>
          <div className={s.swatches} role="radiogroup" aria-label={t('grp.color')}>
            {GROUP_COLORS.map((c) => (
              <button type="button" key={c} role="radio" aria-checked={color === c} aria-label={c} className={s.swatch} style={{ background: c }} onClick={() => setColor(c)}>
                {color === c && <Check size={15} strokeWidth={3} />}
              </button>
            ))}
          </div>
        </fieldset>

        <div className={u.switchRow}>
          <span>
            <strong>{t('grp.adBlock')}</strong>
            <small>{t('grp.adBlockHint')}</small>
          </span>
          <Switch checked={adBlock} onChange={setAdBlock} label={t('grp.adBlock')} />
        </div>

        <p className={s.grpNote}>{t('grp.defaultsHint')}</p>

        <Sec<SecKey> id="access" icon={KeyRound} title={t('grp.inbounds')} summary={accessSum} open={openSecs.has('access')} onToggle={toggleSec}>
          <div className={u.ibGrid}>
            {inbounds.map((ib) => {
              const on = ibs.includes(ib.id);
              return (
                <button
                  type="button"
                  key={ib.id}
                  role="checkbox"
                  aria-checked={on}
                  className={`${u.ibOpt} ${on ? u.ibOn : ''}`}
                  onClick={() => setIbs((x) => (on ? x.filter((y) => y !== ib.id) : [...x, ib.id]))}
                >
                  <span className={u.ibCheck}>{on && <Check size={13} strokeWidth={3} />}</span>
                  <span className={u.ibText}>
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
          <span className="field__hint">{t('grp.inboundsHint')}</span>
          <div className={u.switchRow}>
            <span>
              <strong>{t('form.blockTorrent')}</strong>
              <small>{t('form.blockTorrentHint')}</small>
            </span>
            <Switch checked={lim.block_torrent} onChange={(v) => L('block_torrent', v)} label={t('form.blockTorrent')} />
          </div>
        </Sec>

        <Sec<SecKey> id="data" icon={HardDrive} title={t('form.sec.data')} summary={dataSum} open={openSecs.has('data')} onToggle={toggleSec}>
          <LimitField label={t('form.dataLimit')} unit="GB" decimals fallback={30} value={limitGb} onChange={setLimitGb} presets={LIMITS} />
          <fieldset className={u.fs}>
            <legend className="field__label">{t('user.resetStrategy')}</legend>
            <div className="seg seg--wrap" role="radiogroup" aria-label={t('user.resetStrategy')}>
              {RESETS.map((r) => (
                <button type="button" role="radio" key={r} aria-checked={reset === r} onClick={() => setReset(r)}>
                  {t(`form.reset.${r}`)}
                </button>
              ))}
            </div>
          </fieldset>
          <div className={u.fs}>
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
          </div>
          <LimitField label={t('form.dailyLimit')} unit="GB" decimals fallback={1} value={gb(lim.daily_limit)} onChange={(v) => L('daily_limit', Math.round(v * GB))} presets={DAILY} />
        </Sec>

        <Sec<SecKey> id="time" icon={Clock} title={t('form.expiry')} summary={timeSum} open={openSecs.has('time')} onToggle={toggleSec}>
          <div className="seg seg--wrap" role="radiogroup" aria-label={t('form.expiry')}>
            {(['days', 'never', 'hold'] as Exp[]).map((m) => (
              <button type="button" role="radio" key={m} aria-checked={exp === m} onClick={() => setExp(m)}>
                {t(m === 'never' ? 'form.expiry.never' : m === 'days' ? 'form.expiry.days' : 'form.expiry.onHold')}
              </button>
            ))}
          </div>
          {exp !== 'never' && (
            <NumChips
              value={exp === 'days' ? days : hold}
              onChange={(v) => (exp === 'days' ? setDays(Math.round(v)) : setHold(Math.round(v)))}
              options={DAYS}
              zero=""
              unit={t('common.days')}
              label={t('form.daysLabel')}
              step="1"
            />
          )}
          <span className="field__hint">{t('grp.expiryHint')}</span>
        </Sec>

        <Sec<SecKey> id="conn" icon={Cable} title={t('form.sec.conn')} summary={connSum} open={openSecs.has('conn')} onToggle={toggleSec}>
          <div className={u.twoCol}>
            <div className={u.fs}>
              <span className="field__label">{t('form.ipLimit')}</span>
              <Stepper value={lim.ip_limit} onChange={(v) => L('ip_limit', v)} label={t('form.ipLimit')} format={(v) => (v === 0 ? t('common.unlimited') : n(v))} />
            </div>
            <div className={u.fs}>
              <span className="field__label">{t('form.deviceLimit')}</span>
              <Stepper value={lim.device_limit} onChange={(v) => L('device_limit', v)} label={t('form.deviceLimit')} format={(v) => (v === 0 ? t('common.unlimited') : n(v))} />
            </div>
          </div>
          <div className={u.switchRow}>
            <span>
              <strong>{t('form.requireHwid')}</strong>
              <small>{t('form.requireHwidHint')}</small>
            </span>
            <Switch checked={lim.require_hwid} onChange={(v) => L('require_hwid', v)} label={t('form.requireHwid')} />
          </div>
        </Sec>

        <Sec<SecKey> id="speed" icon={Gauge} title={t('form.sec.speed')} summary={speedSum} open={openSecs.has('speed')} onToggle={toggleSec}>
          <LimitField unit="Mbps" label={t('form.speedDown')} value={lim.speed_limit_down} onChange={(v) => L('speed_limit_down', v)} />
          <LimitField unit="Mbps" label={t('form.speedUp')} value={lim.speed_limit_up} onChange={(v) => L('speed_limit_up', v)} hint={t('form.speedHint')} />
        </Sec>
      </form>
    </Sheet>
  );
}
