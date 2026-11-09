import { FileJson, Pause, Play, RotateCw, Search } from 'lucide-react';
import { memo, useEffect, useMemo, useRef, useState } from 'react';
import { useCoreConfig, useCoreLogs, useRestartCore, useStats, type LogEntry } from '@/api/hooks';
import type { EngineName } from '@/api/types';
import { Select } from '@/components/Select';
import { ENGINES, VISIBLE_ENGINES, engineName } from '@/lib/protocols';
import { useSearchParams } from 'react-router-dom';
import { Sheet } from '@/components/Overlay';
import { CopyButton, EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { formatClock, formatIsoDay, useI18n } from '@/i18n';
import { formatOffset } from '@/lib/timezones';
import { confirmAction, toast } from '@/store/feedback';
import s from './pages.module.css';

type Source = EngineName | 'all' | 'panel';

/** "Asia/Tehran (UTC+03:30)" */
function tzLabel(tz: string | undefined) {
  const zone = tz || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  try {
    const name = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' }).formatToParts(new Date()).find((p) => p.type === 'timeZoneName')?.value ?? '';
    const m = /([+-])(\d{1,2})(?::?(\d{2}))?/.exec(name);
    const min = m ? (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3] ?? 0)) : 0;
    return `\u2066${zone} (${formatOffset(min)})\u2069`;
  } catch {
    return zone;
  }
}

type Level = 'all' | 'info' | 'warning' | 'error';

/** The level filter a line passes (debug lines count as info). */
const passes = (lvl: LogEntry['level'], f: Level) => f === 'all' || (f === 'info' ? lvl === 'info' || lvl === 'debug' : lvl === f);

// tz is passed so memoized lines re-render when the panel's zone arrives or changes
const Line = memo(function Line({ e, showSource, today }: { e: LogEntry; showSource: boolean; today: string; tz?: string }) {
  const day = e.t ? formatIsoDay(e.t) : '';
  const parts = e.msg.split(/(accepted|tcp:[^\s]+|udp:[^\s]+|email: \S+|\[[^\]]+\])/g);
  return (
    <div className={`${s.logLine} ${s['lv_' + (e.level === 'debug' ? 'info' : e.level)]}`}>
      <span className={s.logTs} title={e.t ? `${day} ${formatClock(e.t, true)}` : undefined}>
        {e.t ? (
          <>
            {day !== today && <span>{day.slice(5)} </span>}
            {formatClock(e.t)}
            <span className={s.logMs}>{formatClock(e.t, true).slice(8)}</span>
          </>
        ) : (
          <span aria-hidden="true">{'--:--:--'}</span>
        )}
      </span>
      {showSource && <span className={s.logSrc}>{e.source}</span>}
      <span className={s.logText}>
        {parts.map((p, i) =>
          p === 'accepted' ? (
            <b key={i} className={s.logOk}>
              {p}
            </b>
          ) : /^(tcp|udp):/.test(p) ? (
            <span key={i} className={s.logHost}>
              {p}
            </span>
          ) : /^email: /.test(p) ? (
            <span key={i} className={s.logUser}>
              {p}
            </span>
          ) : /^\[/.test(p) ? (
            <span key={i} className={s.logTag}>
              {p}
            </span>
          ) : (
            <span key={i}>{p}</span>
          ),
        )}
      </span>
    </div>
  );
});

export default function LogsPage() {
  const { t, n, tz } = useI18n();
  const [live, setLive] = useState(true);
  const [filter, setFilter] = useState('');
  const [level, setLevel] = useState<Level>('all');
  const [auto, setAuto] = useState(true);
  const [cfgOpen, setCfgOpen] = useState(false);
  const [params, setParams] = useSearchParams();
  const stats = useStats(10_000);
  // every engine that has inbounds, plus the panel's own log; "all" merges them by time
  const engines = ENGINES.filter((e) => VISIBLE_ENGINES.includes(e) || stats.data?.cores?.some((c) => c.name === e && c.enabled !== false));
  const sources: Source[] = ['all', ...engines, 'panel'];
  const wanted = params.get('core') as Source | null;
  const core: Source = wanted && (wanted === 'all' || wanted === 'panel' || ENGINES.includes(wanted as EngineName)) ? wanted : 'all';
  const setCore = (c: Source) => setParams(c === 'all' ? {} : { core: c }, { replace: true });
  const q = useCoreLogs(live, core);
  const cfgCore: EngineName = core === 'all' || core === 'panel' ? 'xray' : core;
  const cfg = useCoreConfig(cfgOpen, cfgCore);
  const restart = useRestartCore();
  const box = useRef<HTMLDivElement>(null);

  const lines = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return (q.data?.entries ?? []).filter((e) => passes(e.level, level) && (!f || e.msg.toLowerCase().includes(f) || e.source.includes(f)));
  }, [q.data, filter, level]);
  const today = formatIsoDay(new Date());

  useEffect(() => {
    if (auto && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines, auto]);

  const doRestart = async () => {
    if (!(await confirmAction({ title: t('dash.restartConfirm'), body: t('dash.restartBody'), confirmLabel: t('dash.restart') }))) return;
    restart.mutate(undefined, { onSuccess: () => toast.success(t('dash.restarted')), onError: (e) => toast.error(t('common.error'), (e as Error).message) });
  };
  const cfgText = cfg.data ? (typeof cfg.data === 'string' ? cfg.data : JSON.stringify(cfg.data, null, 2)) : '';
  const engineOpts = sources.map((e) => {
    if (e === 'all') return { value: e, label: t('logs.src.all') };
    if (e === 'panel') return { value: e, label: t('logs.src.panel'), hint: 'Veilon' };
    const c = stats.data?.cores?.find((x) => x.name === e);
    return { value: e, label: engineName(e), hint: c ? (c.enabled === false ? t('dash.engineIdle') : c.running ? t('dash.coreRunning') : t('dash.coreStopped')) : undefined };
  });

  return (
    <div>
      <PageHeader
        title={t('logs.title')}
        actions={
          <>
            <div className={s.enginePick}>
              <Select<Source> value={core} onChange={setCore} options={engineOpts} ariaLabel={t('logs.source')} />
            </div>
            <button className="btn" onClick={() => setCfgOpen(true)}>
              <FileJson size={17} />
              {t('logs.config')}
            </button>
            <button className="btn" onClick={doRestart} disabled={restart.isPending}>
              <RotateCw size={17} className={restart.isPending ? 'spin' : undefined} />
              {t('dash.restart')}
            </button>
          </>
        }
      />
      <div className={s.term}>
        <div className={s.termBar}>
          <div className={s.lights} aria-hidden="true">
            <i />
            <i />
            <i />
          </div>
          <button className={`${s.livePill} ${live ? s.liveOn : ''}`} onClick={() => setLive((x) => !x)} aria-pressed={live} aria-label={live ? t('logs.pause') : t('logs.resume')}>
            {live ? <Pause size={13} /> : <Play size={13} />}
            {live ? t('logs.live') : t('logs.paused')}
          </button>
          <div className={s.termSearch}>
            <Search size={15} aria-hidden="true" />
            <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('logs.filter')} aria-label={t('logs.filter')} className="ltr" />
          </div>
          <div className={s.levels} role="group" aria-label="level">
            {(['all', 'info', 'warning', 'error'] as Level[]).map((l) => (
              <button key={l} aria-pressed={level === l} onClick={() => setLevel(l)} className={s['lvBtn_' + l]}>
                {t(`logs.level.${l}`)}
              </button>
            ))}
          </div>
          <label className={s.autoscroll}>
            <input type="checkbox" className="check" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
            {t('logs.autoscroll')}
          </label>
        </div>
        <div className={s.termBody} ref={box} role="log" aria-live={live ? 'polite' : 'off'} aria-label={t('logs.title')} dir="ltr" tabIndex={0}>
          {q.isError && !q.data ? (
            <ErrorState error={q.error} onRetry={() => q.refetch()} />
          ) : q.isPending ? (
            <div style={{ display: 'grid', gap: 8 }}>
              {Array.from({ length: 12 }, (_, i) => (
                <Skeleton key={i} h={14} w={`${50 + ((i * 37) % 45)}%`} />
              ))}
            </div>
          ) : lines.length === 0 ? (
            <EmptyState kind="logs" title={t('logs.empty')} />
          ) : (
            lines.map((e, i) => <Line key={e.seq || `${i}:${e.t}`} e={e} showSource={core === 'all'} today={today} tz={tz} />)
          )}
          {live && <span className={s.cursor} aria-hidden="true" />}
        </div>
        <div className={s.termFoot}>
          <span>{t('logs.lines', { n: n(lines.length) })}</span>
          <span>{t('logs.tz', { tz: tzLabel(tz) })}</span>
        </div>
      </div>

      <Sheet open={cfgOpen} onClose={() => setCfgOpen(false)} title={`${t('logs.config')} · ${engineName(cfgCore)}`} width={720} headerExtra={cfgText ? <CopyButton text={cfgText} compact /> : null}>
        {cfg.isPending ? <Skeleton h={400} r={16} /> : cfg.isError ? <ErrorState error={cfg.error} /> : <pre className={`${s.json} mono`} dir="ltr">{cfgText}</pre>}
      </Sheet>
    </div>
  );
}
