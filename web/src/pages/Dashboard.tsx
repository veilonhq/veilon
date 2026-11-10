import { useQuery } from '@tanstack/react-query';
import { ArrowDown, ArrowUp, Plus, ScrollText, Waypoints } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useStats, useTraffic } from '@/api/hooks';
import type { Paged, TrafficRange, User } from '@/api/types';
import { TrafficChart } from '@/components/TrafficChart';
import { ServerTraffic } from '@/components/ServerTraffic';
import { ErrorState, Skeleton } from '@/components/misc';
import { ArcGauge, CorePulse, CountUp, OnlineOrbit } from '@/components/viz';
import { LiveChart, useRateSeries } from '@/components/LiveChart';
import { splitBytes, useI18n } from '@/i18n';
import { useSession } from '@/store/session';
import { useUi } from '@/store/ui';
import type { CoreInfo } from '@/api/types';
import { VISIBLE_ENGINES, engineName } from '@/lib/protocols';
import { CoreActions, CorePowerSwitch, CoreVersionField } from './dashboard/XrayCore';
import s from './Dashboard.module.css';

const ratio = (used: number, total: number) => (total > 0 ? (used / total) * 100 : 0);
/** "Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz" → "Xeon Platinum 8259CL @ 2.50GHz" */
function shortCpu(m: string) {
  return m
    .replace(/\((R|TM|tm)\)/g, '')
    .replace(/^(Intel|AMD)\s+/i, '')
    .replace(/\s+CPU\b/i, '')
    .replace(/\s+(\d+-Core|Processor)\b/gi, '')
    .replace(/\s+/g, ' ')
    .trim();
}

function Big({ bytes }: { bytes: number }) {
  const { n } = useI18n();
  const [, unit] = splitBytes(bytes);
  return (
    <span className={s.big}>
      {/* tween only within one unit; crossing MB → GB is set directly so digits and unit never disagree */}
      <CountUp id="dash.trafficTotal" value={bytes} format={(v) => n(splitBytes(v)[0], { maximumFractionDigits: 1 })} canTween={(a, b) => splitBytes(a)[1] === splitBytes(b)[1]} />
      <small>{unit}</small>
    </span>
  );
}

export default function DashboardPage() {
  const { t, n, bps, bytes, duration, lang } = useI18n();
  const pct = (v: number) => n(v / 100, { style: 'percent', maximumFractionDigits: 0 });
  const admin = useSession((x) => x.admin);
  const theme = useUi((x) => x.theme);
  const stats = useStats(2000);
  const [range, setRange] = useState<TrafficRange>('24h');
  const traffic = useTraffic(range);
  const online = useQuery({
    queryKey: ['users', 'online-sample'],
    queryFn: () => api<Paged<User>>('/users', { query: { status: 'active', per_page: 80, sort: '-used_traffic' } }),
    refetchInterval: 15_000,
  });
  const d = stats.data;
  const rates = useRateSeries(d, stats.dataUpdatedAt);
  const onlineNames = (online.data?.items ?? []).filter((u) => u.online).map((u) => u.username);

  if (stats.isError && !d) return <ErrorState error={stats.error} onRetry={() => stats.refetch()} />;

  return (
    <div className={s.page}>
      <header className={s.head}>
        <div>
          <div className="eyebrow">{t('dash.title')}</div>
          <h1 className={`${s.title} display`}>{t('dash.greeting', { name: admin?.username ?? '' })}</h1>
        </div>
      </header>

      <div className={s.bento}>
        {/* HERO: live waveform */}
        <section className={`glass ${s.tile} ${s.hero}`} aria-labelledby="live-h">
          <div className={s.heroTop}>
            <div>
              <h2 id="live-h" className={s.tileLabel}>
                <span className={s.livePill}>
                  <span className={s.liveDot} />
                  {t('dash.live')}
                </span>
              </h2>
            </div>
            <div className={s.rates}>
              <div className={s.rate}>
                <span className={s.rateLabel}>
                  <ArrowDown size={14} /> {t('dash.down')}
                </span>
                <strong className={`num display ${s.rateDown}`} aria-live="polite">
                  {d ? bps(d.net_down_bps) : '—'}
                </strong>
              </div>
              <div className={s.rate}>
                <span className={s.rateLabel}>
                  <ArrowUp size={14} /> {t('dash.up')}
                </span>
                <strong className={`num display ${s.rateUp}`}>{d ? bps(d.net_up_bps) : '—'}</strong>
              </div>
            </div>
          </div>
          <div className={s.waveWrap}>{d ? <LiveChart samples={rates} height={200} theme={theme} lang={lang} /> : <Skeleton h={200} r={18} />}</div>
          <div className={s.heroFoot}>
            <div>
              <span className={s.footLabel}>{t('dash.totalTraffic')}</span>
              {d ? <Big bytes={d.traffic_total_up + d.traffic_total_down} /> : <Skeleton w={120} h={36} />}
            </div>
            <div className={s.splitBar} aria-hidden="true">
              {d && (
                <>
                  <span style={{ flex: d.traffic_total_down }} className={s.splitDown} />
                  <span style={{ flex: d.traffic_total_up }} className={s.splitUp} />
                </>
              )}
            </div>
            <div className={s.legend}>
              <span>
                <i className={s.legDown} />
                {t('dash.down')} {d ? bytes(d.traffic_total_down) : ''}
              </span>
              <span>
                <i className={s.legUp} />
                {t('dash.up')} {d ? bytes(d.traffic_total_up) : ''}
              </span>
            </div>
          </div>
        </section>

        {/* CORE PULSE */}
        <section className={`glass ${s.tile} ${s.core}`} aria-labelledby="core-h">
          <div className={s.coreBody}>
            <CorePulse running={!!d?.core.running} size={150} />
            <div className={s.coreInfo}>
              <h2 id="core-h" className={s.tileLabel}>
                {t('dash.core')}
              </h2>
              <strong className={`display ${s.coreState} ${d?.core.running ? s.on : s.off}`}>
                {d ? (d.core.running ? t('dash.coreRunning') : d.core.stopped ? t('xray.off') : t('dash.coreStopped')) : '—'}
              </strong>
              <CorePowerSwitch core={d?.core} stoppedCores={d?.stopped_cores} />
              <CoreVersionField core={d?.core} />
              {/* always one line tall (empty while loading or stopped), so data arriving never resizes the tile */}
              <span className={s.meta} aria-hidden={!(d?.core.running && d.core.started_at)}>
                {d?.core.running && d.core.started_at ? t('dash.coreSince', { time: duration(Date.now() - new Date(d.core.started_at).getTime()) }) : '\u00a0'}
              </span>
              {d?.core.error && (
                <span className={s.coreErr} title={d.core.error}>
                  {t('dash.coreError')}: <span className="ltr">{d.core.error}</span>
                </span>
              )}
            </div>
          </div>
          <ul className={s.engines} aria-label={t('dash.engines')}>
            {(d?.cores?.length ? d.cores.filter((c) => (VISIBLE_ENGINES as string[]).includes(c.name)) : d ? [d.core] : VISIBLE_ENGINES.map((e): CoreInfo => ({ name: e, version: '', running: false, started_at: null }))).map((c: CoreInfo) => {
              const idle = c.enabled === false;
              const warn = c.warnings ?? [];
              const state = idle ? 'idle' : c.running ? 'on' : 'off';
              return (
                <li key={c.name} className={s.engine} data-state={state} data-warn={!idle && c.running && warn.length > 0 ? '' : undefined} title={[c.error, ...warn].filter(Boolean).join('\n') || undefined}>
                  <i className={s.engineDot} aria-hidden="true" />
                  <span className={s.engineName}>{engineName(c.name)}</span>
                  <span className={`${s.engineVer} num ltr`}>{c.version || ''}</span>
                  <span className={s.engineState}>
                    {!d ? '—' : idle ? t('dash.engineIdle') : warn.length ? t('dash.engineWarnings', { n: warn.length }) : c.running ? t('dash.coreRunning') : c.stopped ? t('xray.off') : t('dash.coreStopped')}
                  </span>
                </li>
              );
            })}
          </ul>
          <CoreActions core={d?.core} />
        </section>

        {/* ONLINE ORBIT */}
        <section className={`glass ${s.tile} ${s.online}`} aria-labelledby="online-h">
          <div className={s.onlineText}>
            <h2 id="online-h" className={s.tileLabel}>
              {t('dash.online')}
            </h2>
            <strong className={`display ${s.huge}`}>{d ? <CountUp id="dash.usersOnline" value={d.users_online} format={(v) => n(Math.round(v))} /> : '—'}</strong>
            <span className={s.meta}>{d ? t('dash.onlineOf', { n: d.users_total }) : '\u00a0'}</span>
          </div>
          <div className={s.orbitWrap}>
            <OnlineOrbit names={onlineNames} total={d?.users_online ?? 0} size={190} />
          </div>
        </section>

        {/* GAUGES */}
        <section className={`glass ${s.tile} ${s.gauges}`} aria-label={t('dash.system')}>
          <div className={s.gaugeGrid}>
            {d ? (
              <>
                <ArcGauge
                  value={d.cpu_percent}
                  label={pct(d.cpu_percent)}
                  detail={t('dash.cpu')}
                  title={d.cpu_model || undefined}
                  lines={[
                    [d.cpu_cores ? (d.cpu_cores === 1 ? t('dash.coreOne') : t('dash.cores', { n: d.cpu_cores })) : null, d.load_avg ? t('dash.load', { v: n(d.load_avg[0], { maximumFractionDigits: 2 }) }) : null]
                      .filter(Boolean)
                      .join(' · '),
                    ...(d.cpu_model ? [<span className="ltr" key="m">{shortCpu(d.cpu_model)}</span>] : []),
                  ]}
                />
                <ArcGauge
                  value={ratio(d.mem_used, d.mem_total)}
                  label={pct(ratio(d.mem_used, d.mem_total))}
                  detail={t('dash.ram')}
                  lines={[t('dash.usedOf', { used: bytes(d.mem_used), total: bytes(d.mem_total) })]}
                />
                <ArcGauge
                  value={ratio(d.disk_used, d.disk_total)}
                  label={pct(ratio(d.disk_used, d.disk_total))}
                  detail={t('dash.disk')}
                  lines={[t('dash.usedOf', { used: bytes(d.disk_used), total: bytes(d.disk_total) })]}
                />
                {d.swap_total > 0 ? (
                  <ArcGauge
                    value={ratio(d.swap_used, d.swap_total)}
                    label={pct(ratio(d.swap_used, d.swap_total))}
                    detail={t('dash.swap')}
                    lines={[t('dash.usedOf', { used: bytes(d.swap_used), total: bytes(d.swap_total) })]}
                  />
                ) : (
                  <ArcGauge off value={0} label="—" detail={t('dash.swap')} lines={[t('dash.swapOff')]} />
                )}
              </>
            ) : (
              [0, 1, 2, 3].map((k) => (
                <div key={k} className={s.gaugeSkel}>
                  <Skeleton w="80%" h={96} r={48} />
                  <Skeleton w="60%" h={12} />
                </div>
              ))
            )}
          </div>
          <div className={s.uptime}>
            <span>{t('dash.uptime')}</span>
            <strong className="num">{d ? duration(d.uptime_sec * 1000) : '—'}</strong>
          </div>
        </section>

        {/* USERS SUMMARY */}
        <section className={`glass ${s.tile} ${s.users}`} aria-labelledby="users-h">
          <div className={s.usersHead}>
            <h2 id="users-h" className={s.tileLabel}>
              {t('dash.usersTotal')}
            </h2>
            <Link to="/users" className={s.tileLink}>
              {t('nav.users')}
            </Link>
          </div>
          <strong className={`display ${s.huge}`}>{d ? <CountUp id="dash.usersTotal" value={d.users_total} format={(v) => n(Math.round(v))} /> : '—'}</strong>
          {d && (
            <>
              <div className={s.stack} aria-hidden="true">
                <span className={s.stA} style={{ flex: d.users_active }} />
                <span className={s.stL} style={{ flex: d.users_limited }} />
                <span className={s.stE} style={{ flex: d.users_expired }} />
                <span className={s.stO} style={{ flex: Math.max(0, d.users_total - d.users_active - d.users_limited - d.users_expired) }} />
              </div>
              <ul className={s.statList}>
                <li>
                  <i className={s.stA} />
                  {t('dash.usersActive')}
                  <b className="num">{n(d.users_active)}</b>
                </li>
                <li>
                  <i className={s.stL} />
                  {t('dash.usersLimited')}
                  <b className="num">{n(d.users_limited)}</b>
                </li>
                <li>
                  <i className={s.stE} />
                  {t('dash.usersExpired')}
                  <b className="num">{n(d.users_expired)}</b>
                </li>
                <li>
                  <i className={s.stO} />
                  {t('dash.usersOther')}
                  <b className="num">{n(Math.max(0, d.users_total - d.users_active - d.users_limited - d.users_expired))}</b>
                </li>
              </ul>
            </>
          )}
        </section>

        {/* HISTORY */}
        <section className={`glass ${s.tile} ${s.history}`} aria-labelledby="hist-h">
          <div className={s.histHead}>
            <h2 id="hist-h" className={`${s.tileTitle} display`}>
              {t('dash.history')}
            </h2>
            <div className="seg" role="tablist" aria-label={t('dash.history')}>
              {(['24h', '7d', '30d'] as TrafficRange[]).map((r) => (
                <button key={r} role="tab" aria-selected={range === r} onClick={() => setRange(r)}>
                  {t(`dash.range.${r}`)}
                </button>
              ))}
            </div>
          </div>
          {traffic.data ? <TrafficChart points={traffic.data.points} range={range} bucketSec={traffic.data.bucket_sec} height={230} /> : <Skeleton h={230} r={16} />}
          {traffic.data?.servers && traffic.data.servers.length > 1 && <ServerTraffic servers={traffic.data.servers} range={range} />}
        </section>

        {/* SHORTCUTS */}
        <section className={`glass ${s.tile} ${s.quick}`} aria-labelledby="quick-h">
          <h2 id="quick-h" className={s.tileLabel}>
            {t('dash.quick')}
          </h2>
          <Link to="/users?new=1" className={`${s.quickBtn} ${s.quickPrimary}`}>
            <Plus size={20} />
            {t('dash.newUser')}
          </Link>
          <Link to="/inbounds?new=1" className={s.quickBtn}>
            <Waypoints size={20} />
            {t('dash.newInbound')}
          </Link>
          <Link to="/logs" className={s.quickBtn}>
            <ScrollText size={20} />
            {t('dash.viewLogs')}
          </Link>
        </section>
      </div>
    </div>
  );
}
