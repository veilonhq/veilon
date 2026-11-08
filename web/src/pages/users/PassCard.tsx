import { motion } from 'framer-motion';
import { memo } from 'react';
import type { Group, Inbound, User } from '@/api/types';
import { StatusChip } from '@/components/misc';
import { QuotaRing } from '@/components/viz';
import { useI18n } from '@/i18n';
import { usePanelTz } from '@/api/hooks';
import { barcode, expiryInfo, usageRatio, usageTone } from '@/lib/user';
import { protoShort } from '@/lib/protocols';
import { LimitBadges } from './LimitBadges';
import s from './Users.module.css';

export function ExpiryText({ u, short }: { u: User; short?: boolean }) {
  const { t, duration, n } = useI18n();
  const e = expiryInfo(u);
  if (e.kind === 'none') return <>{t('users.noExpiry')}</>;
  if (e.kind === 'hold') return <>{short ? t('users.onHoldShort', { n: n(u.on_hold_days ?? 0) }) : t('users.onHoldFor', { n: n(u.on_hold_days ?? 0) })}</>;
  if (e.kind === 'past') return <>{short ? t('users.agoShort', { t: duration(e.ms) }) : t('users.expiredAgo', { t: duration(e.ms) })}</>;
  return <>{short ? duration(e.ms) : t('users.expiresIn', { t: duration(e.ms) })}</>;
}

/** "Online" while the client is sending traffic, otherwise the full last-connection stamp. */
export function LastSeen({ u, label = true }: { u: User; label?: boolean }) {
  const { t, stampParts } = useI18n();
  const tz = usePanelTz();
  if (u.online) {
    return (
      <span className={s.onlineTag}>
        <span className={s.onlineDot} />
        {t('common.online')}
      </span>
    );
  }
  if (!u.last_online_at) return <span>{t('users.neverSeen')}</span>;
  const [day, time] = stampParts(u.last_online_at, tz);
  // date and time are isolated runs: next to each other the digits would
  // otherwise be reordered as one number sequence
  return (
    <span className={s.seenAt}>
      {label && `${t('users.lastSeen')} `}
      <bdi className="num">{day}</bdi>
      <span className={s.stampSep} aria-hidden="true">·</span>
      <bdi className="num">{time}</bdi>
    </span>
  );
}

interface Props {
  u: User;
  inbounds: Map<number, Inbound>;
  selected: boolean;
  onToggle: (id: number, shift: boolean) => void;
  onOpen: (id: number) => void;
  index: number;
  /** the user's group, drawn as a dot (+ name on the full card) */
  group?: Group;
}

export const PassCard = memo(function PassCard({ u, inbounds, selected, onToggle, onOpen, index, group }: Props) {
  const { t, n, bytes, date } = useI18n();
  const ratio = usageRatio(u);
  const tone = usageTone(u);
  const exp = expiryInfo(u);
  const bars = barcode(u.sub_token, 24);
  const protos = Array.from(new Set(u.inbound_ids.map((id) => inbounds.get(id)?.protocol).filter(Boolean))) as string[];

  return (
    <motion.article
      initial={{ y: 14 }}
      animate={{ y: 0 }}
      transition={{ duration: 0.4, delay: Math.min(index % 24, 12) * 0.025, ease: [0.22, 1, 0.36, 1] }}
      className={`${s.pass} ${selected ? s.passSelected : ''} ${s['tone_' + tone]}`}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest('input,button,a')) return;
        if (e.shiftKey || e.metaKey || e.ctrlKey) onToggle(u.id, e.shiftKey);
        else onOpen(u.id);
      }}
    >
      <div className={s.passTop}>
        <div className={s.passMeta}>
          <input
            type="checkbox"
            className="check"
            checked={selected}
            onChange={(e) => onToggle(u.id, (e.nativeEvent as MouseEvent).shiftKey)}
            aria-label={t('users.select', { name: u.username })}
          />
          <span className={s.passNo}>
            {t('users.pass')} · <span className="num ltr">#{String(u.id).padStart(4, '0')}</span>
          </span>
          {group && (
            <span className={s.passGroup} title={`${t('form.group')}: ${group.name}`}>
              <span className={s.groupDot} style={{ background: group.color }} aria-hidden="true" />
              <span className={s.passGroupName}>{group.name}</span>
            </span>
          )}
        </div>
        <StatusChip status={u.status} />
      </div>
      <button className={`${s.passName} display`} onClick={() => onOpen(u.id)}>
        <span className="ltr">{u.username}</span>
      </button>
      <div className={s.passSeen}>
        <LastSeen u={u} />
        {u.note && <span className={s.passNote}>· {u.note}</span>}
        <LimitBadges u={u} max={4} className={s.lbEnd} />
      </div>

      <div className={s.perf} aria-hidden="true" />

      <div className={s.passBottom}>
        <QuotaRing ratio={ratio} size={62} stroke={6} tone={tone} label={`${t('users.used')} ${bytes(u.used_traffic)}`}>
          <span className={s.ringPct}>{ratio === null ? '∞' : n(ratio, { style: 'percent', maximumFractionDigits: 0 })}</span>
        </QuotaRing>
        <div className={s.passStat}>
          <span className={s.statLabel}>{t('users.used')}</span>
          <strong className="num">{bytes(u.used_traffic)}</strong>
          <span className={s.statSub}>
            {t('common.of')} {u.data_limit ? bytes(u.data_limit, 0) : t('common.unlimited')}
          </span>
        </div>
        <div className={`${s.passStat} ${s.passExp}`}>
          <span className={s.statLabel}>{exp.kind === 'past' ? t('users.expiredLabel') : exp.kind === 'future' ? t('users.expiresInLabel') : t('users.expires')}</span>
          <strong className={`${s['fg_' + exp.tone]}`}>
            <ExpiryText u={u} short />
          </strong>
          <span className={s.statSub}>{u.expire_at ? date(u.expire_at) : exp.kind === 'hold' ? t('users.afterFirst') : '—'}</span>
        </div>
      </div>
      <div className={s.passFoot}>
        <div className={s.gates}>
          {protos.slice(0, 2).map((p) => (
            <span key={p} className={s.gate}>
              {p === 'shadowsocks' ? 'SS' : p.toUpperCase()}
            </span>
          ))}
          {protos.length > 2 && <span className={s.gate}>+{n(protos.length - 2)}</span>}
          {protos.length === 0 && <span className={s.gate}>—</span>}
        </div>
        <div className={s.barcode} aria-hidden="true">
          {bars.map((w, i) => (
            <i key={i} style={{ width: w, opacity: i % 5 === 0 ? 0.5 : 1 }} />
          ))}
        </div>
      </div>
    </motion.article>
  );
});

/**
 * Compact pass: one short ticket per user. The quota ring sits in a stub
 * cut off by a perforation (notches top and bottom), the body holds name,
 * status, usage, time left and the protocols.
 */
export const CompactPass = memo(function CompactPass({ u, inbounds, selected, onToggle, onOpen, index, group }: Props) {
  const { t, n, bytes } = useI18n();
  const ratio = usageRatio(u);
  const tone = usageTone(u);
  const exp = expiryInfo(u);
  const protos = Array.from(new Set(u.inbound_ids.map((id) => inbounds.get(id)?.protocol).filter(Boolean))) as string[];

  return (
    <motion.article
      initial={{ y: 10 }}
      animate={{ y: 0 }}
      transition={{ duration: 0.35, delay: Math.min(index % 24, 12) * 0.02, ease: [0.22, 1, 0.36, 1] }}
      className={`${s.mini} ${selected ? s.passSelected : ''} ${s['tone_' + tone]}`}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest('input,button,a')) return;
        if (e.shiftKey || e.metaKey || e.ctrlKey) onToggle(u.id, e.shiftKey);
        else onOpen(u.id);
      }}
    >
      <div className={s.miniStub}>
        <QuotaRing ratio={ratio} size={46} stroke={4.5} tone={tone} label={`${t('users.used')} ${bytes(u.used_traffic)}`}>
          <span className={s.miniPct}>{ratio === null ? '∞' : n(ratio, { style: 'percent', maximumFractionDigits: 0 })}</span>
        </QuotaRing>
      </div>
      <div className={s.miniBody}>
        <div className={s.miniTop}>
          <input
            type="checkbox"
            className={`check ${s.miniCheck}`}
            checked={selected}
            onChange={(e) => onToggle(u.id, (e.nativeEvent as MouseEvent).shiftKey)}
            aria-label={t('users.select', { name: u.username })}
          />
          {group && <span className={s.groupDot} style={{ background: group.color }} title={`${t('form.group')}: ${group.name}`} role="img" aria-label={`${t('form.group')}: ${group.name}`} />}
          <button className={s.miniName} onClick={() => onOpen(u.id)}>
            <span className="ltr">{u.username}</span>
          </button>
          <LimitBadges u={u} max={2} className={s.lbEnd} />
          <span className={s.miniChip}>
            <StatusChip status={u.status} />
          </span>
        </div>
        <div className={s.miniBottom}>
          <span className={`${s.miniUse} num`} dir="ltr">
            <strong>{bytes(u.used_traffic)}</strong>
            <span className={s.miniOf}> / {u.data_limit ? bytes(u.data_limit, 0) : '∞'}</span>
          </span>
          <span className={s.miniSep} aria-hidden="true" />
          <span className={`${s.miniExp} ${s['fg_' + exp.tone]}`} title={exp.kind === 'future' ? t('users.expiresInLabel') : t('users.expires')}>
            <ExpiryText u={u} short />
          </span>
          <span className={s.miniGates}>
            {protos.slice(0, 1).map((p) => (
              <span key={p} className={s.gate} title={protos.map(protoShort).join(' · ')}>
                {protoShort(p)}
              </span>
            ))}
            {protos.length > 1 && <span className={s.gate}>+{n(protos.length - 1)}</span>}
          </span>
        </div>
        <div className={s.miniSeen}>
          <LastSeen u={u} />
        </div>
      </div>
    </motion.article>
  );
});

export const UserRow = memo(function UserRow({ u, inbounds, selected, onToggle, onOpen }: Omit<Props, 'index'>) {
  const { t, n, bytes } = useI18n();
  const ratio = usageRatio(u);
  const tone = usageTone(u);
  const exp = expiryInfo(u);
  return (
    <div className={`${s.row} ${selected ? s.rowSelected : ''}`} role="row" onClick={(e) => !(e.target as HTMLElement).closest('input,button') && onOpen(u.id)}>
      <span role="cell">
        <input type="checkbox" className="check" checked={selected} onChange={(e) => onToggle(u.id, (e.nativeEvent as MouseEvent).shiftKey)} aria-label={t('users.select', { name: u.username })} />
      </span>
      <span role="cell" className={s.rowName}>
        <span className={`${s.rowAvatar} ${u.online ? s.rowAvatarOn : ''}`}>{u.username.slice(0, 2).toUpperCase()}</span>
        <button className={s.rowNameBtn} onClick={() => onOpen(u.id)}>
          <span className="ltr">{u.username}</span>
        </button>
      </span>
      <span role="cell">
        <StatusChip status={u.status} />
      </span>
      <span role="cell" className={s.rowUsage}>
        <span className={s.bar}>
          <i className={s['bg_' + tone]} style={{ width: `${(ratio ?? 0) * 100}%` }} />
        </span>
        <span className="num">
          {bytes(u.used_traffic)} <small>/ {u.data_limit ? bytes(u.data_limit, 0) : '∞'}</small>
        </span>
      </span>
      <span role="cell" className={`${s.rowExp} ${s['fg_' + exp.tone]}`}>
        <ExpiryText u={u} />
      </span>
      <span role="cell" className={s.rowGates}>
        {n(u.inbound_ids.filter((id) => inbounds.has(id)).length)} {t('users.gate')}
      </span>
    </div>
  );
});
