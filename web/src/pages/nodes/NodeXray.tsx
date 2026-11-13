import { CheckCircle2, Download, Loader2, RotateCw, ShieldCheck, XCircle } from 'lucide-react';
import { useEffect, useRef, type ReactNode } from 'react';
import { qk, useNodeXray } from '@/api/hooks';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError } from '@/api/client';
import type { NodeUpdate, NodeUpdatePhase, VNode } from '@/api/types';
import { formatClock, formatIsoDay, useI18n } from '@/i18n';
import { toast } from '@/store/feedback';
import { XrayVersionSelect } from '../dashboard/XrayCore';
import { tagOf } from '../dashboard/xrayVersions';
import s from './Nodes.module.css';

const stamp = (v: string) => `${formatIsoDay(v)} ${formatClock(v)}`;

const STEPS: { phase: NodeUpdatePhase; icon: ReactNode }[] = [
  { phase: 'downloading', icon: <Download size={13} aria-hidden="true" /> },
  { phase: 'verifying', icon: <ShieldCheck size={13} aria-hidden="true" /> },
  { phase: 'restarting', icon: <RotateCw size={13} aria-hidden="true" /> },
];

const final = (u: NodeUpdate | null | undefined) => !!u && (u.phase === 'done' || u.phase === 'failed');

/** An Xray-core install is running on this node. */
export const installingXray = (x: VNode) => !!x.state.xray_update && !final(x.state.xray_update);

type TFn = ReturnType<typeof useI18n>['t'];
const KNOWN = new Set(['bad_version', 'agent_unsupported', 'offline', 'up_to_date', 'in_progress', 'rate_limited', 'node_refused']);

export function xrayErrText(t: TFn, code?: string, message?: string) {
  if (code && KNOWN.has(code)) return t(`node.xrayErr.${code}` as Parameters<TFn>[0]);
  return message ?? '';
}

/** Outcomes already announced (a node shows its row on the card and in its sheet). */
const announced = new Set<string>();

const RECENT_DONE = 30 * 60_000;
const RECENT_FAILED = 24 * 3_600_000;

/**
 * The node's Xray-core version as a picker (the same one as the main
 * server's Xray card), with the install's steps and its outcome. An agent
 * without Xray installs shows "needs agent update" instead.
 */
export function NodeXrayRow({ node: x }: { node: VNode }) {
  const { t } = useI18n();
  const inst = useNodeXray();
  const qc = useQueryClient();
  const st = x.state;
  const u = st.xray_update;
  const running = installingXray(x);
  const current = tagOf(st.xray_version);
  const live = x.enabled && (st.status === 'online' || st.status === 'degraded');

  // Announce the outcome of an install seen running.
  const seen = useRef<string | null>(null);
  useEffect(() => {
    if (!u) return;
    const key = `${x.id}:${u.started_at}`;
    if (!final(u)) {
      seen.current = key;
      return;
    }
    if (seen.current !== key) return;
    seen.current = null;
    // the node's new version and its returning inbounds
    qc.invalidateQueries({ queryKey: qk.nodes });
    qc.invalidateQueries({ queryKey: qk.inbounds });
    if (announced.has(key)) return;
    announced.add(key);
    if (u.phase === 'failed') toast.error(t('node.xrayRes.failed', { v: u.target, name: x.name }), u.rolled_back ? t('node.xrayRes.rolledBack', { v: tagOf(u.from) }) : u.error);
    else toast.success(t('node.xrayRes.done', { v: u.target, name: x.name }));
  }, [u, x.id, x.name, t, qc]);

  const install = (tag: string) =>
    inst.mutate(
      { id: x.id, version: tag },
      {
        onSuccess: () => toast.info(t('node.xrayStarted', { v: tag, name: x.name })),
        onError: (e) => {
          const ae = e instanceof ApiError ? e : null;
          toast.error(t('node.xrayRefused', { name: x.name }), xrayErrText(t, ae?.code, (e as Error).message));
        },
      },
    );

  let body: ReactNode = null;
  if (u && running) {
    const at = STEPS.findIndex((p) => p.phase === u.phase);
    body = (
      <div className={s.progress} role="status" aria-live="polite">
        <span className={s.progTitle}>
          <span>{t('node.xrayRes.running', { v: u.target })}</span>
          <small className="ltr">{t('node.upd.since', { t: stamp(u.started_at) })}</small>
        </span>
        <ol className={s.steps3}>
          {STEPS.map((p, i) => (
            <li key={p.phase} className={i < at ? s.stepDone : i === at ? s.stepNow : undefined} aria-current={i === at ? 'step' : undefined}>
              {i < at ? <CheckCircle2 size={13} aria-hidden="true" /> : i === at ? <Loader2 size={13} className="spin" aria-hidden="true" /> : p.icon}
              <span>{t(`node.upd.${p.phase}`)}</span>
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
          <strong>{t('node.xrayRes.done', { v: u.target, name: x.name })}</strong>
          <span className={`${s.when} ltr`}>{stamp(u.finished_at)}</span>
        </span>
      </div>
    );
  } else if (u && u.phase === 'failed' && u.finished_at && Date.now() - Date.parse(u.finished_at) < RECENT_FAILED) {
    body = (
      <div className={`${s.result} ${s.failRes}`} role="status">
        <XCircle size={16} aria-hidden="true" />
        <span>
          <strong>{t('node.xrayRes.failed', { v: u.target, name: x.name })}</strong>
          {u.rolled_back && <span>{t('node.xrayRes.rolledBack', { v: tagOf(u.from) })}</span>}
          {u.error && <span className={s.why}>{u.error}</span>}
          <span className={`${s.when} ltr`}>{stamp(u.finished_at)}</span>
        </span>
      </div>
    );
  }

  return (
    <div className={s.xrayBox}>
      <div className={s.updRow}>
        <span className={s.ver}>{t('node.xrayCore')}</span>
        {st.can_update_xray ? (
          <span className={s.xrayPick}>
            <XrayVersionSelect
              current={current}
              className={s.xraySelect}
              disabled={!live || running || inst.isPending}
              busyNote={null}
              confirmTitle={(tag) => t('node.xrayConfirm', { v: tag, name: x.name })}
              confirmBody={(older) => (older ? `${t('node.xrayBody')} ${t('node.xrayOlder')}` : t('node.xrayBody'))}
              onInstall={install}
            />
          </span>
        ) : (
          <span className={s.ver}>
            <b className="ltr">{current || '—'}</b>
            {!!st.version && <span className={s.needsAgent}>{t('node.xrayNeedsAgent')}</span>}
          </span>
        )}
      </div>
      {body}
    </div>
  );
}
