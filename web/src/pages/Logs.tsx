import { FileJson, Pause, Play, RotateCw, Search } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useCoreConfig, useCoreLogs, useRestartCore, useStats } from '@/api/hooks';
import type { EngineName } from '@/api/types';
import { Select } from '@/components/Select';
import { VISIBLE_ENGINES, engineName } from '@/lib/protocols';
import { useSearchParams } from 'react-router-dom';
import { Sheet } from '@/components/Overlay';
import { CopyButton, EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import s from './pages.module.css';

type Level = 'all' | 'info' | 'warning' | 'error';

function levelOf(line: string): Exclude<Level, 'all'> {
  if (/\[Error\]|error/i.test(line) && !/accepted/.test(line)) return 'error';
  if (/\[Warning\]/i.test(line)) return 'warning';
  return 'info';
}

function Line({ text }: { text: string }) {
  const m = text.match(/^(\S+ \S+)\s(.*)$/);
  const ts = m?.[1] ?? '';
  const rest = m?.[2] ?? text;
  const lvl = levelOf(text);
  const parts = rest.split(/(accepted|tcp:[^\s]+|udp:[^\s]+|email: \S+|\[[^\]]+\])/g);
  return (
    <div className={`${s.logLine} ${s['lv_' + lvl]}`}>
      <span className={s.logTs}>{ts.split(' ')[1] ?? ts}</span>
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
}

export default function LogsPage() {
  const { t, n } = useI18n();
  const [live, setLive] = useState(true);
  const [filter, setFilter] = useState('');
  const [level, setLevel] = useState<Level>('all');
  const [auto, setAuto] = useState(true);
  const [cfgOpen, setCfgOpen] = useState(false);
  const [params, setParams] = useSearchParams();
  const core = VISIBLE_ENGINES.includes(params.get('core') as EngineName) ? (params.get('core') as EngineName) : 'xray';
  const setCore = (c: EngineName) => setParams(c === 'xray' ? {} : { core: c }, { replace: true });
  const stats = useStats(10_000);
  const q = useCoreLogs(live, core);
  const cfg = useCoreConfig(cfgOpen, core);
  const restart = useRestartCore();
  const box = useRef<HTMLDivElement>(null);

  const lines = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return (q.data?.lines ?? []).filter((l) => (level === 'all' || levelOf(l) === level) && (!f || l.toLowerCase().includes(f)));
  }, [q.data, filter, level]);

  useEffect(() => {
    if (auto && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines, auto]);

  const doRestart = async () => {
    if (!(await confirmAction({ title: t('dash.restartConfirm'), body: t('dash.restartBody'), confirmLabel: t('dash.restart') }))) return;
    restart.mutate(undefined, { onSuccess: () => toast.success(t('dash.restarted')), onError: (e) => toast.error(t('common.error'), (e as Error).message) });
  };
  const cfgText = cfg.data ? (typeof cfg.data === 'string' ? cfg.data : JSON.stringify(cfg.data, null, 2)) : '';
  const engineOpts = VISIBLE_ENGINES.map((e) => {
    const c = stats.data?.cores?.find((x) => x.name === e);
    return { value: e, label: engineName(e), hint: c ? (c.enabled === false ? t('dash.engineIdle') : c.running ? t('dash.coreRunning') : t('dash.coreStopped')) : undefined };
  });

  return (
    <div>
      <PageHeader
        title={t('logs.title')}
        actions={
          <>
            {engineOpts.length > 1 && (
              <div className={s.enginePick}>
                <Select<EngineName> value={core} onChange={setCore} options={engineOpts} ariaLabel={t('logs.engine')} />
              </div>
            )}
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
            lines.map((l, i) => <Line key={i + l.slice(0, 30)} text={l} />)
          )}
          {live && <span className={s.cursor} aria-hidden="true" />}
        </div>
        <div className={s.termFoot}>
          <span>{t('logs.lines', { n: n(lines.length) })}</span>
          <span className="mono ltr">GET /core/logs?core={core}&amp;lines=200</span>
        </div>
      </div>

      <Sheet open={cfgOpen} onClose={() => setCfgOpen(false)} title={`${t('logs.config')} · ${engineName(core)}`} width={720} headerExtra={cfgText ? <CopyButton text={cfgText} compact /> : null}>
        {cfg.isPending ? <Skeleton h={400} r={16} /> : cfg.isError ? <ErrorState error={cfg.error} /> : <pre className={`${s.json} mono`} dir="ltr">{cfgText}</pre>}
      </Sheet>
    </div>
  );
}
