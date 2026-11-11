import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from 'react-router-dom';
import { create } from 'zustand';
import { AlertTriangle, ArrowUpCircle, Check, ChevronLeft, ChevronRight, Loader2, RotateCcw, ShieldCheck, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api } from '@/api/client';
import { API_BASE } from '@/app/base';
import { useI18n, type MessageKey } from '@/i18n';
import { useSession } from '@/store/session';
import { toast } from '@/store/feedback';
import { Modal } from './Overlay';
import s from './UpdateBanner.module.css';
import { noteBlocks, noteLine, type Span } from './updateNotes';

export interface UpdateStatus {
  tag: string;
  from: string;
  phase: 'queued' | 'download' | 'verify' | 'backup' | 'install' | 'restart' | 'health' | 'done' | 'rolled_back' | 'failed';
  running: boolean;
  error?: string;
  backup_dir?: string;
  started_at: string;
  finished_at?: string;
}
export interface UpdateInfo {
  current: string;
  available: { tag: string; name: string; notes: string; url: string; published_at: string } | null;
  can_update: boolean;
  checked_at: string | null;
  /** next automatic check (null when automatic checks are off) */
  next_check_at?: string | null;
  status: UpdateStatus | null;
  /** only on POST /system/update/check */
  cached?: boolean;
}

const STEPS: UpdateStatus['phase'][] = ['download', 'verify', 'backup', 'install', 'restart', 'health'];
const SEEN_KEY = 'veilon.update.seen';
/** The success notice closes itself after this long. */
export const SUCCESS_NOTICE_MS = 10_000;

/** Opens the update dialog from anywhere (the banner and the Panel update page). */
export const useUpdateDialog = create<{ open: boolean; setOpen: (v: boolean) => void }>((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
}));

export const UPDATE_QUERY = ['panel-update'] as const;

/** GET /system/update, shared by the banner and the Panel update page. */
export function useUpdateInfo(watching = false) {
  const sudo = !!useSession((x) => x.admin)?.is_sudo;
  return useQuery({
    queryKey: UPDATE_QUERY,
    queryFn: () => api<UpdateInfo>('/system/update'),
    enabled: sudo,
    staleTime: 60_000,
    retry: false,
    refetchInterval: (qq) => (watching || qq.state.data?.status?.running ? 2000 : 10 * 60_000),
  });
}

/**
 * A slim strip above every page for sudo admins when a newer stable release
 * exists (checked by the server every 6 hours, or from the Panel update page). Its Update
 * button opens the install dialog; the update itself runs on the server and
 * the dialog follows it across the restart. After a successful update a
 * short notice says so; it closes itself after 10 seconds or with its X, and
 * does not come back for that update.
 */
export function UpdateBanner() {
  const { t, dir, lang } = useI18n();
  const admin = useSession((x) => x.admin);
  const sudo = !!admin?.is_sudo;
  const open = useUpdateDialog((x) => x.open);
  const setOpen = useUpdateDialog((x) => x.setOpen);
  const [watching, setWatching] = useState(false);
  const [seen, setSeen] = useState(() => localStorage.getItem(SEEN_KEY) ?? '');
  const onUpdatePage = /\/update\/?$/.test(useLocation().pathname);
  const q = useUpdateInfo(watching);
  const info = q.data;
  const st = info?.status ?? null;
  const finishedKey = st?.finished_at ? `${st.tag}@${st.finished_at}` : '';
  // A finished update the admin has not seen yet (e.g. after the page reloaded).
  const unseenResult = !!st && !st.running && !!finishedKey && finishedKey !== seen && Date.now() - new Date(st.finished_at!).getTime() < 3_600_000;
  const success = unseenResult && st?.phase === 'done';
  const markSeen = useCallback(() => {
    if (!finishedKey) return;
    localStorage.setItem(SEEN_KEY, finishedKey);
    setSeen(finishedKey);
  }, [finishedKey]);
  useEffect(() => {
    if (st?.running) setWatching(true);
  }, [st?.running]);
  // The success notice closes itself (and stays closed for this update).
  useEffect(() => {
    if (!success || open) return;
    const id = window.setTimeout(markSeen, SUCCESS_NOTICE_MS);
    return () => window.clearTimeout(id);
  }, [success, open, markSeen]);

  if (!sudo || !info) return null;
  const busy = !!st?.running || watching;
  // The Panel update page shows the available release itself.
  const show = (!!info.available && !onUpdatePage) || !!st?.running || unseenResult;
  const dialog = (
    <UpdateDialog
      open={open}
      info={info}
      result={unseenResult}
      onClose={() => {
        setOpen(false);
        if (finishedKey && !st?.running) markSeen();
      }}
      onStarted={() => setWatching(true)}
      onSettled={() => setWatching(false)}
    />
  );
  if (!show) return dialog;
  const Chev = dir === 'rtl' ? ChevronLeft : ChevronRight;
  let label: string;
  if (busy) label = t('upd.running', { v: st?.tag ?? info.available?.tag ?? '' });
  else if (success) label = t('upd.doneShort', { v: st!.tag });
  else if (unseenResult) label = t('upd.failedShort', { v: st!.tag });
  else label = t('upd.availableShort', { v: info.available!.tag });
  const line = !busy && !unseenResult && info.available ? noteLine(info.available.notes, lang) : '';
  const tone = unseenResult && !success ? s.warn : success ? s.ok : '';
  const icon = busy ? <Loader2 size={15} className="spin" /> : unseenResult && !success ? <AlertTriangle size={15} /> : success ? <Check size={15} /> : <ArrowUpCircle size={15} />;

  return (
    <>
      {success && !busy ? (
        <div className={`${s.banner} ${s.ok}`} role="status">
          <button type="button" className={s.main} onClick={() => setOpen(true)}>
            <span className={s.icon} aria-hidden="true">
              {icon}
            </span>
            <span className={s.text}>
              <strong>{label}</strong>
            </span>
          </button>
          <button type="button" className={s.close} onClick={markSeen} aria-label={t('upd.dismiss')} title={t('upd.dismiss')}>
            <X size={16} aria-hidden="true" />
          </button>
        </div>
      ) : (
        <button type="button" className={`${s.banner} ${s.main} ${tone}`} onClick={() => setOpen(true)}>
          <span className={s.icon} aria-hidden="true">
            {icon}
          </span>
          <span className={s.text}>
            <strong>{label}</strong>
            {line && (
              <small>
                <bdi>{line}</bdi>
              </small>
            )}
          </span>
          {!busy && !unseenResult ? (
            <span className={s.cta}>{t('upd.update')}</span>
          ) : (
            <Chev size={16} className={s.chev} aria-hidden="true" />
          )}
        </button>
      )}
      {dialog}
    </>
  );
}

function Spans({ spans }: { spans: Span[] }) {
  return (
    <>
      {spans.map((x, i) =>
        x.code ? (
          <code key={i} className="ltr" dir="ltr">
            {x.text}
          </code>
        ) : (
          <span key={i}>{x.text}</span>
        ),
      )}
    </>
  );
}

/** Release notes in the panel's language, rendered from their markdown. */
export function Notes({ notes, className }: { notes: string; className?: string }) {
  const { lang, dir } = useI18n();
  const blocks = noteBlocks(notes, lang);
  const out: ReactNode[] = [];
  let items: ReactNode[] = [];
  const flushItems = () => {
    if (items.length) out.push(<ul key={`u${out.length}`}>{items}</ul>);
    items = [];
  };
  blocks.forEach((b, i) => {
    if (b.kind === 'item') {
      items.push(
        <li key={i}>
          <Spans spans={b.spans} />
        </li>,
      );
      return;
    }
    flushItems();
    out.push(
      b.kind === 'heading' ? (
        <h3 key={i}>
          <Spans spans={b.spans} />
        </h3>
      ) : (
        <p key={i}>
          <Spans spans={b.spans} />
        </p>
      ),
    );
  });
  flushItems();
  return (
    <div className={`${s.notes} ${className ?? ''}`} dir={dir}>
      {out}
    </div>
  );
}

function UpdateDialog({ open, info, result, onClose, onStarted, onSettled }: { open: boolean; info: UpdateInfo; result: boolean; onClose: () => void; onStarted: () => void; onSettled: () => void }) {
  const { t, lang } = useI18n();
  const qc = useQueryClient();
  const [starting, setStarting] = useState(false);
  const [down, setDown] = useState(false);
  const [ran, setRan] = useState(false);
  const st = info.status;
  const rel = info.available;
  const running = !!st?.running || starting;
  const target = st?.running ? st.tag : rel?.tag;
  useEffect(() => {
    if (st?.running) setRan(true);
  }, [st?.running]);
  const finished = (ran || result) && !starting && !!st && !st.running && !!st.finished_at;
  const reloaded = useRef(false);

  // While the panel restarts the API is unreachable: follow /health until
  // the server answers again, then refresh the update status.
  useEffect(() => {
    if (!open || !running) return;
    let stop = false;
    const tick = async () => {
      try {
        const r = await fetch(`${API_BASE}/health`, { cache: 'no-store' });
        const h = (await r.json()) as { version?: string };
        setDown(false);
        if (h.version === target) qc.invalidateQueries({ queryKey: UPDATE_QUERY });
      } catch {
        setDown(true);
      }
      if (!stop) setTimeout(tick, 1500);
    };
    tick();
    return () => {
      stop = true;
    };
  }, [open, running, target, qc]);

  useEffect(() => {
    if (st && !st.running && st.finished_at && starting) setStarting(false);
    if (st && !st.running && st.finished_at) onSettled();
  }, [st, starting, onSettled]);

  // A successful update brings new UI files: reload once it is done.
  useEffect(() => {
    if (open && st?.phase === 'done' && st.tag === target && !reloaded.current && info.current === st.tag) {
      reloaded.current = true;
      setTimeout(() => window.location.reload(), 1800);
    }
  }, [open, st, target, info.current]);

  const install = async () => {
    if (!rel) return;
    setStarting(true);
    setRan(true);
    onStarted();
    try {
      await api('/system/update', { method: 'POST', body: { tag: rel.tag } });
      qc.invalidateQueries({ queryKey: UPDATE_QUERY });
    } catch (e) {
      setStarting(false);
      onSettled();
      toast.error(t('upd.startFailed'), (e as Error).message);
    }
  };

  const phaseIdx = st ? STEPS.indexOf(st.phase) : -1;
  const showProgress = running || (finished && st?.phase === 'done');
  // after a failed or rolled back update the admin sees why, and may try again
  const failedBefore = finished && !!st && st.phase !== 'done';
  return (
    <Modal open={open} onClose={running ? () => undefined : onClose} label={t('upd.title')} className={s.modal}>
      <div className={s.dialog}>
        <div className={s.head}>
          <span className={s.headIcon} aria-hidden="true">
            <ArrowUpCircle size={24} />
          </span>
          <div>
            <h2 className="display">{t('upd.title')}</h2>
            <p className="ltr">
              {st?.from && st.tag === (target ?? st.tag) ? st.from : info.current} → <strong>{target ?? st?.tag}</strong>
            </p>
          </div>
        </div>
        {failedBefore && st && (
          <div className={s.errBox} role="alert">
            <strong>{st.phase === 'rolled_back' ? t('upd.rolledBack', { v: st.from }) : t('upd.failed')}</strong>
            {st.error && <pre className="ltr">{st.error}</pre>}
            {st.backup_dir && <small className="ltr">{t('upd.backupAt', { dir: st.backup_dir })}</small>}
          </div>
        )}
        {!showProgress && rel && (
          <>
            {rel.notes && noteBlocks(rel.notes, lang).length ? (
              <Notes notes={rel.notes} />
            ) : (
              <p className={s.muted}>{t('upd.noNotes')}</p>
            )}
            <p className={s.safety}>
              <ShieldCheck size={16} aria-hidden="true" />
              <span>{t('upd.safety')}</span>
            </p>
            {!info.can_update && (
              <p className={s.blocked}>
                <AlertTriangle size={16} aria-hidden="true" />
                <span>
                  {t('upd.noUpdater')} <code className="ltr">sudo veilon update</code>
                </span>
              </p>
            )}
          </>
        )}
        {showProgress && (
          <ol className={s.steps}>
            {STEPS.map((p, i) => {
              const done = st?.phase === 'done' || (phaseIdx > i && running) || (finished && st?.phase !== 'failed' && st?.phase !== 'rolled_back');
              const cur = running && (phaseIdx === i || (phaseIdx < 0 && i === 0));
              const stepDown = p === 'restart' && cur && down;
              return (
                <li key={p} className={done ? s.stepDone : cur ? s.stepCur : s.stepTodo}>
                  <span className={s.dot} aria-hidden="true">
                    {done ? <Check size={13} /> : cur ? <Loader2 size={13} className="spin" /> : null}
                  </span>
                  <span>{t(`upd.step.${p}` as MessageKey)}</span>
                  {stepDown && <small>{t('upd.offline')}</small>}
                </li>
              );
            })}
          </ol>
        )}
        {finished && st?.phase === 'done' && (
          <p className={s.okText}>
            <Check size={16} /> {t('upd.done', { v: st.tag })}
          </p>
        )}
        <div className={s.actions}>
          {!showProgress && rel && (
            <>
              <button type="button" className="btn btn--ghost" onClick={onClose}>
                {t('upd.later')}
              </button>
              <button type="button" className="btn btn--primary" disabled={!info.can_update || !rel} onClick={install}>
                <ArrowUpCircle size={16} />
                {t('upd.install')}
              </button>
            </>
          )}
          {running && <span className={s.muted}>{t('upd.wait')}</span>}
          {finished && st?.phase === 'done' && (
            <button type="button" className="btn btn--primary" onClick={() => window.location.reload()}>
              <RotateCcw size={16} />
              {t('upd.reload')}
            </button>
          )}
          {failedBefore && !rel && (
            <button type="button" className="btn btn--primary" onClick={onClose}>
              {t('common.close')}
            </button>
          )}
        </div>
      </div>
    </Modal>
  );
}
