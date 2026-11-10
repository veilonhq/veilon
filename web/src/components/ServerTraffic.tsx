import { Server, Sigma } from 'lucide-react';
import type { ServerTraffic as ST, TrafficRange } from '@/api/types';
import { useI18n } from '@/i18n';
import { Skeleton } from './misc';
import s from './ServerTraffic.module.css';

/**
 * Each server's share of users' usage in the chart's window: the main
 * server, every node and the total. Shown only when there are nodes (or
 * history from removed ones); with the main server alone it says nothing
 * the chart does not.
 */
export function ServerTraffic({ servers, range, showTitle = false }: { servers: ST[] | undefined; range: TrafficRange; showTitle?: boolean }) {
  const { t, bytes } = useI18n();
  if (!servers) return <Skeleton h={88} r={16} />;
  if (servers.length < 2) return null;
  const total = servers.reduce((a, x) => a + x.up + x.down, 0);
  const tu = servers.reduce((a, x) => a + x.up, 0);
  const td = servers.reduce((a, x) => a + x.down, 0);
  return (
    <div className={s.wrap}>
      {showTitle && (
        <div className={s.head}>
          <strong>{t('srv.title')}</strong>
          <small>{t(`srv.period.${range}`)}</small>
        </div>
      )}
      <ul className={s.list}>
        {servers.map((x) => {
          const v = x.up + x.down;
          const pct = total > 0 ? (v / total) * 100 : 0;
          const name = x.id === 0 ? t('srv.main') : x.removed ? t('srv.removed', { name: x.name }) : x.name;
          return (
            <li key={x.id} className={x.id === 0 ? s.main : ''}>
              <span className={s.name}>
                <Server size={14} aria-hidden="true" />
                <span title={name}>{name}</span>
              </span>
              <span className={s.bar} aria-hidden="true">
                <i style={{ inlineSize: `${pct}%` }} />
              </span>
              <span className={s.val}>
                <bdi>{bytes(v)}</bdi>
                <small>
                  ↓<bdi>{bytes(x.down)}</bdi> ↑<bdi>{bytes(x.up)}</bdi>
                </small>
              </span>
            </li>
          );
        })}
        <li className={s.total}>
          <span className={s.name}>
            <Sigma size={14} aria-hidden="true" />
            <span>{t('srv.total')}</span>
          </span>
          <span />
          <span className={s.val}>
            <bdi>{bytes(total)}</bdi>
            <small>
              ↓<bdi>{bytes(td)}</bdi> ↑<bdi>{bytes(tu)}</bdi>
            </small>
          </span>
        </li>
      </ul>
      <p className={s.hint}>{t('srv.hint')}</p>
    </div>
  );
}
