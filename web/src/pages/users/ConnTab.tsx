import { Ban, Laptop, Network, ShieldCheck, Smartphone, Trash2 } from 'lucide-react';
import { useBlockDevice, useBlockIP, useDeleteDevice, useDeleteIP, usePanelTz, useUserConnections } from '@/api/hooks';
import type { ConnDevice, ConnIP, User } from '@/api/types';
import { EmptyState, ErrorState, Skeleton } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import s from './Users.module.css';

/** Full Persian / English date and time in the panel's zone, as two isolated runs. */
function Stamp({ iso }: { iso: string }) {
  const { stampParts } = useI18n();
  const tz = usePanelTz();
  const [day, time] = stampParts(iso, tz);
  return (
    <span className={s.seenAt}>
      <bdi className="num">{day}</bdi>
      <span className={s.stampSep} aria-hidden="true">·</span>
      <bdi className="num">{time}</bdi>
    </span>
  );
}

function Seen({ first, last, count }: { first: string; last: string; count: number }) {
  const { t, n } = useI18n();
  return (
    <dl className={s.connSeen}>
      <div>
        <dt>{t('conn.firstSeen')}</dt>
        <dd>
          <Stamp iso={first} />
        </dd>
      </div>
      <div>
        <dt>{t('conn.lastSeen')}</dt>
        <dd>
          <Stamp iso={last} />
        </dd>
      </div>
      <div className={s.connCount}>
        <dd className="num">{t('conn.count', { n: n(count) })}</dd>
      </div>
    </dl>
  );
}

function Country({ code }: { code: string }) {
  if (!code) return null;
  return (
    <span className={`${s.connCc} ltr`} title={code}>
      {code.toUpperCase()}
    </span>
  );
}

function deviceName(d: ConnDevice, fallback: string) {
  const os = [d.os, d.os_version].filter(Boolean).join(' ');
  return [d.model, os].filter(Boolean).join(' · ') || fallback;
}

export function ConnTab({ u, mayEdit = true }: { u: User; mayEdit?: boolean }) {
  const { t, n } = useI18n();
  const q = useUserConnections(u.id);
  const blockIP = useBlockIP();
  const delIP = useDeleteIP();
  const blockDev = useBlockDevice();
  const delDev = useDeleteDevice();
  const err = (e: unknown) => toast.error(t('common.error'), (e as Error).message);

  if (q.isPending)
    return (
      <div style={{ display: 'grid', gap: 10 }}>
        <Skeleton h={92} r={18} />
        <Skeleton h={92} r={18} />
      </div>
    );
  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />;
  const { ips, devices } = q.data;
  const liveIps = ips.filter((x) => x.live && !x.blocked).length;
  const activeDevs = devices.filter((x) => !x.blocked).length;

  const toggleIP = (x: ConnIP) =>
    blockIP.mutate({ uid: u.id, ip: x.ip, blocked: !x.blocked }, { onSuccess: () => toast.success(x.blocked ? t('conn.unblockedDone') : t('conn.blockedDone')), onError: err });
  const removeIP = async (x: ConnIP) => {
    if (!(await confirmAction({ title: t('conn.deleteIp', { ip: x.ip }), body: t('conn.deleteBody'), danger: true, confirmLabel: t('common.delete') }))) return;
    delIP.mutate({ uid: u.id, id: x.id }, { onSuccess: () => toast.success(t('conn.deleted')), onError: err });
  };
  const toggleDev = (x: ConnDevice) =>
    blockDev.mutate({ uid: u.id, id: x.id, blocked: !x.blocked }, { onSuccess: () => toast.success(x.blocked ? t('conn.unblockedDone') : t('conn.blockedDone')), onError: err });
  const removeDev = async (x: ConnDevice) => {
    if (!(await confirmAction({ title: t('conn.deleteDevice', { name: deviceName(x, t('conn.unknownDevice')) }), body: t('conn.deleteBody'), danger: true, confirmLabel: t('common.delete') }))) return;
    delDev.mutate({ uid: u.id, id: x.id }, { onSuccess: () => toast.success(t('conn.deleted')), onError: err });
  };

  const actions = (blocked: boolean, onToggle: () => void, onDelete: () => void, label: string) =>
    !mayEdit ? null : (
    <div className={s.connBtns}>
      <button type="button" className={`btn btn--sm ${blocked ? '' : s.connBlockBtn}`} onClick={onToggle} title={blocked ? t('conn.unblock') : t('conn.block')} aria-label={`${blocked ? t('conn.unblock') : t('conn.block')} ${label}`}>
        {blocked ? <ShieldCheck size={15} /> : <Ban size={15} />}
        <span className={s.connBtnText}>{blocked ? t('conn.unblock') : t('conn.block')}</span>
      </button>
      <button type="button" className="btn btn--sm btn--icon" onClick={onDelete} aria-label={`${t('common.delete')} ${label}`} title={t('common.delete')}>
        <Trash2 size={15} />
      </button>
    </div>
  );

  return (
    <div className={s.conn}>
      <section className={s.connSec}>
        <header className={s.connHead}>
          <Network size={16} />
          <strong>{t('conn.ips')}</strong>
          <span className={s.tabCount}>{n(ips.length)}</span>
          {u.ip_limit > 0 && (
            <span className={`${s.connLimit} num`} title={t('badge.ip', { n: n(u.ip_limit) })}>
              {t('conn.limitOf', { used: n(liveIps), max: n(u.ip_limit) })}
            </span>
          )}
        </header>
        {ips.length === 0 ? (
          <p className={s.connEmpty}>{t('conn.noIps')}</p>
        ) : (
          <ul className={s.connList}>
            {ips.map((x) => (
              <li key={x.id} className={`${s.connItem} ${x.blocked ? s.connBlocked : ''}`}>
                <div className={s.connTop}>
                  <code className={`${s.connIp} mono ltr`}>{x.ip}</code>
                  <Country code={x.country} />
                  {x.blocked ? (
                    <span className="chip chip--expired">{t('conn.blocked')}</span>
                  ) : x.live ? (
                    <span className="chip chip--active">
                      <span className="chip__dot" />
                      {t('conn.live')}
                    </span>
                  ) : null}
                  {actions(x.blocked, () => toggleIP(x), () => removeIP(x), x.ip)}
                </div>
                <Seen first={x.first_seen} last={x.last_seen} count={x.count} />
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className={s.connSec}>
        <header className={s.connHead}>
          <Smartphone size={16} />
          <strong>{t('conn.devices')}</strong>
          <span className={s.tabCount}>{n(devices.length)}</span>
          {u.device_limit > 0 && (
            <span className={`${s.connLimit} num`} title={t('badge.device', { n: n(u.device_limit) })}>
              {t('conn.limitOf', { used: n(activeDevs), max: n(u.device_limit) })}
            </span>
          )}
        </header>
        {devices.length === 0 ? (
          <EmptyState kind="gates" title={t('conn.noDevices')} body={t('conn.noDevicesBody')} />
        ) : (
          <ul className={s.connList}>
            {devices.map((x) => {
              const name = deviceName(x, t('conn.unknownDevice'));
              const Icon = /windows|mac|linux/i.test(x.os) ? Laptop : Smartphone;
              return (
                <li key={x.id} className={`${s.connItem} ${x.blocked ? s.connBlocked : ''}`}>
                  <div className={s.connTop}>
                    <span className={s.connDev}>
                      <Icon size={15} aria-hidden="true" />
                      <bdi className={s.connDevName}>{name}</bdi>
                    </span>
                    {x.blocked && <span className="chip chip--expired">{t('conn.blocked')}</span>}
                    {actions(x.blocked, () => toggleDev(x), () => removeDev(x), name)}
                  </div>
                  <div className={s.connMeta}>
                    <code className="mono ltr" title={x.hwid}>
                      {x.hwid}
                    </code>
                    {x.ip && <code className="mono ltr">{x.ip}</code>}
                    <Country code={x.country} />
                    {x.user_agent && <span className="ltr">{x.user_agent}</span>}
                  </div>
                  <Seen first={x.first_seen} last={x.last_seen} count={x.count} />
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
