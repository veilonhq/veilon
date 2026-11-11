import { ArrowUpCircle, CheckCircle2, Download, Loader2, RotateCw, ShieldCheck, Terminal, XCircle } from 'lucide-react';
import type { ReactNode } from 'react';
import { useNodeUpdate } from '@/api/hooks';
import { ApiError } from '@/api/client';
import type { NodeUpdate, NodeUpdatePhase, VNode } from '@/api/types';
import { formatClock, formatIsoDay, useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import s from './Nodes.module.css';

/** "2026-11-10 18:20:05" in the panel's zone. */
const stamp = (v: string) => `${formatIsoDay(v)} ${formatClock(v)}`;

const STEPS: { phase: NodeUpdatePhase; icon: ReactNode }[] = [
  { phase: 'downloading', icon: <Download size={13} aria-hidden="true" /> },
  { phase: 'verifying', icon: <ShieldCheck size={13} aria-hidden="true" /> },
  { phase: 'restarting', icon: <RotateCw size={13} aria-hidden="true" /> },
];

const final = (u: NodeUpdate | null | undefined) => !!u && (u.phase === 'done' || u.phase === 'failed');

/** An agent update is running on this node. */
export const updating = (x: VNode) => !!x.state.update && !final(x.state.update);

/** The node's agent is older than the main server and can update itself. */
export const canUpdate = (x: VNode) => !!x.update_to && x.state.can_update && !updating(x);

/** The node's agent is older and cannot update itself (before v0.3.5). */
export const needsManual = (x: VNode) => !!x.update_to && !x.state.can_update && !!x.state.version;

type TFn = ReturnType<typeof useI18n>['t'];

const KNOWN = new Set(['agent_unsupported', 'arch_unknown', 'not_release', 'up_to_date', 'downgrade', 'in_progress', 'rate_limited', 'offline', 'release_unavailable', 'node_refused']);

/** A refused update in the panel's language (the server's text otherwise). */
export function updateErrText(t: TFn, code?: string, message?: string) {
  if (code && KNOWN.has(code)) return t(`node.updErr.${code}` as Parameters<TFn>[0]);
  return message ?? '';
}

/** Results recent enough to show on the card. */
const RECENT_DONE = 30 * 60_000;
const RECENT_FAILED = 24 * 3_600_000;

export function NodeUpdateRow({ node: x, onInstall }: { node: VNode; onInstall: () => void }) {
  const { t, n } = useI18n();
  const upd = useNodeUpdate();
  const u = x.state.update;
  const ver = x.state.version;

  const start = async () => {
    const ok = await confirmAction({
      title: t('node.updTitle', { name: x.name }),
      body: t('node.updBody', { from: ver, to: x.update_to }),
      confirmLabel: t('node.updateBtn'),
    });
    if (!ok) return;
    upd.mutate(x.id, {
      onSuccess: () => toast.success(t('node.updStarted', { name: x.name })),
      onError: (e) => {
        const ae = e instanceof ApiError ? e : null;
        toast.error(t('node.updRefused', { name: x.name }), updateErrText(t, ae?.code, (e as Error).message));
      },
    });
  };

  let body: ReactNode = null;
  if (u && !final(u)) {
    const at = STEPS.findIndex((st) => st.phase === u.phase);
    body = (
      <div className={s.progress} role="status" aria-live="polite">
        <span className={s.progTitle}>
          <span>{t('node.upd.running', { v: u.target })}</span>
          <small className="ltr">{t('node.upd.since', { t: stamp(u.started_at) })}</small>
        </span>
        <ol className={s.steps3}>
          {STEPS.map((st, i) => (
            <li key={st.phase} className={i < at ? s.stepDone : i === at ? s.stepNow : undefined} aria-current={i === at ? 'step' : undefined}>
              {i < at ? <CheckCircle2 size={13} aria-hidden="true" /> : i === at ? <Loader2 size={13} className="spin" aria-hidden="true" /> : st.icon}
              <span>{t(`node.upd.${st.phase}`)}</span>
            </li>
          ))}
        </ol>
      </div>
    );
  } else if (u && u.phase === 'done' && u.finished_at && Date.now() - Date.parse(u.finished_at) < RECENT_DONE) {
    body = (
      <div className={`${s.result} ${s.okRes}`} role="status">
        <CheckCircle2 size={16} aria-hidden="true" />
        <span>
          <strong>{t('node.upd.done', { v: u.target })}</strong>
          <span className={`${s.when} ltr`}>{stamp(u.finished_at)}</span>
        </span>
      </div>
    );
  } else if (u && u.phase === 'failed' && u.finished_at && Date.now() - Date.parse(u.finished_at) < RECENT_FAILED && x.update_to) {
    body = (
      <div className={`${s.result} ${s.failRes}`} role="status">
        <XCircle size={16} aria-hidden="true" />
        <span>
          <strong>{t('node.upd.failed', { v: u.target })}</strong>
          {u.rolled_back ? <span>{t('node.upd.rolledBack', { v: u.from })}</span> : u.error && <span className={s.why}>{u.error}</span>}
          <span className={`${s.when} ltr`}>{stamp(u.finished_at)}</span>
        </span>
      </div>
    );
  }

  const manual = needsManual(x);
  return (
    <div className={s.upd}>
      <div className={s.updRow}>
        <span className={s.ver}>
          {t('node.agent')}
          <b className="ltr">{ver || '—'}</b>
          {x.update_to && !updating(x) && (
            <span className={s.newVer} title={t('node.updAvail', { v: x.update_to })}>
              <ArrowUpCircle size={12} aria-hidden="true" />
              <bdi dir="ltr">{x.update_to}</bdi>
            </span>
          )}
        </span>
        {canUpdate(x) && (
          <button type="button" className={`btn btn--primary btn--sm ${s.updBtn}`} onClick={start} disabled={upd.isPending}>
            {upd.isPending ? <Loader2 size={15} className="spin" /> : <ArrowUpCircle size={15} />}
            {t('node.updateBtn')}
          </button>
        )}
      </div>
      {body}
      {manual && (
        <div className={s.manual} role="note">
          <strong>{t('node.manualTitle')}</strong>
          <span>{t('node.manualBody', { v: ver, to: x.update_to, n: n(1) })}</span>
          <button type="button" className="btn btn--sm" onClick={onInstall}>
            <Terminal size={15} />
            {t('node.installAgain')}
          </button>
        </div>
      )}
    </div>
  );
}
