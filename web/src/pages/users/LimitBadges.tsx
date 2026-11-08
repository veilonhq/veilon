import { CalendarClock, CirclePause, Gauge, Magnet, MonitorSmartphone, Network, Sun, type LucideIcon } from 'lucide-react';
import type { User } from '@/api/types';
import { useI18n, type TFn } from '@/i18n';
import s from './Users.module.css';

export interface LimitBadge {
  key: string;
  Icon?: LucideIcon;
  /** short text drawn inside the badge (e.g. ×1.5) instead of / next to the icon */
  text?: string;
  label: string;
  /** a state the user is in right now (paused), not just a configured limit */
  alert?: boolean;
}

type Fmt = { t: TFn; n: (v: number, o?: Intl.NumberFormatOptions) => string; bytes: (v: number, d?: number) => string };

/** Active limits and temporary pauses of a user, most important first. */
export function limitBadges(u: User, { t, n, bytes }: Fmt): LimitBadge[] {
  const out: LimitBadge[] = [];
  if (u.suspend_reason === 'window') out.push({ key: 'sw', Icon: CirclePause, label: t('badge.suspendWindow'), alert: true });
  if (u.suspend_reason === 'daily') out.push({ key: 'sd', Icon: CirclePause, label: t('badge.suspendDaily'), alert: true });
  if (u.ip_limit > 0) out.push({ key: 'ip', Icon: Network, text: n(u.ip_limit), label: t('badge.ip', { n: n(u.ip_limit) }) });
  if (u.device_limit > 0) out.push({ key: 'dev', Icon: MonitorSmartphone, text: n(u.device_limit), label: t('badge.device', { n: n(u.device_limit) }) });
  if (u.speed_limit_down > 0 || u.speed_limit_up > 0) out.push({ key: 'spd', Icon: Gauge, label: t('badge.speed') });
  if (u.daily_limit > 0 && u.suspend_reason !== 'daily') out.push({ key: 'day', Icon: Sun, label: t('badge.daily', { v: bytes(u.daily_limit, 0) }) });
  if ((u.window_from && u.window_to) || (u.window_days !== 0 && (u.window_days & 0x7f) !== 0x7f)) {
    if (u.suspend_reason !== 'window') out.push({ key: 'win', Icon: CalendarClock, label: t('badge.window') });
  }
  if (u.block_torrent) out.push({ key: 'tor', Icon: Magnet, label: t('badge.torrent') });
  if (u.traffic_multiplier && u.traffic_multiplier !== 1) {
    const v = n(u.traffic_multiplier, { maximumFractionDigits: 1 });
    out.push({ key: 'mul', text: `×${v}`, label: t('badge.mult', { v }) });
  }
  return out;
}

/**
 * Tiny icon badges sized to sit on an existing text line (≤ 16px tall), so a
 * card never grows. Shows at most `max` badges; the rest fold into +N with
 * the full list in its tooltip.
 */
export function LimitBadges({ u, max = 3, className }: { u: User; max?: number; className?: string }) {
  const { t, n, bytes } = useI18n();
  const list = limitBadges(u, { t, n, bytes });
  if (!list.length) return null;
  // pauses always stay visible
  const shown = list.slice(0, max);
  const rest = list.slice(max);
  return (
    <span className={`${s.lb} ${className ?? ''}`} role="list" aria-label={t('user.limits')}>
      {shown.map((b) => (
        <span key={b.key} role="listitem" className={`${s.lbItem} ${b.alert ? s.lbAlert : ''}`} title={b.label} aria-label={b.label}>
          {b.Icon && <b.Icon size={11} strokeWidth={2.4} aria-hidden="true" />}
          {b.text && <span className={`${s.lbText} num`}>{b.text}</span>}
        </span>
      ))}
      {rest.length > 0 && (
        <span role="listitem" className={s.lbItem} title={rest.map((b) => b.label).join(' · ')} aria-label={rest.map((b) => b.label).join(' · ')}>
          <span className={`${s.lbText} num`}>+{n(rest.length)}</span>
        </span>
      )}
    </span>
  );
}
