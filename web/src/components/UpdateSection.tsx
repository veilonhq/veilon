import { useMutation, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, ArrowUpCircle, CheckCircle2, Loader2, RefreshCw } from 'lucide-react';
import { api, ApiError } from '@/api/client';
import { useI18n } from '@/i18n';
import { useSession } from '@/store/session';
import { Section } from './misc';
import { noteBlocks } from './updateNotes';
import { Notes, UPDATE_QUERY, useUpdateDialog, useUpdateInfo, type UpdateInfo } from './UpdateBanner';
import s from './UpdateSection.module.css';

/**
 * Settings › Panel update (sudo admins only): the running version, when the
 * server last looked for a release and when it looks next, and a button to
 * look right now. A newer release shows its tag, its notes in the panel's
 * language and an Update button that opens the usual update dialog. The
 * result area keeps one height in every state, so nothing below it moves.
 */
export function UpdateSection({ className }: { className?: string }) {
  const { t, lang, dateTime } = useI18n();
  const sudo = !!useSession((x) => x.admin)?.is_sudo;
  const qc = useQueryClient();
  const q = useUpdateInfo();
  const openDialog = useUpdateDialog((x) => x.setOpen);
  const check = useMutation({
    mutationFn: () => api<UpdateInfo>('/system/update/check', { method: 'POST' }),
    onSuccess: (data) => {
      const { cached: _cached, ...info } = data;
      qc.setQueryData(UPDATE_QUERY, info);
    },
  });
  if (!sudo) return null;
  const info = q.data;
  const rel = info?.available ?? null;
  const checking = check.isPending;
  const err = check.isError ? check.error : null;
  const errText =
    err instanceof ApiError && err.code === 'rate_limited'
      ? t('upd.errRate')
      : err instanceof ApiError && err.code === 'timeout'
        ? t('upd.errTimeout')
        : t('upd.errNetwork');

  let body;
  if (!info) {
    body = <span className={s.placeholder} aria-hidden="true" />;
  } else if (checking) {
    body = (
      <p className={s.state}>
        <Loader2 size={18} className="spin" aria-hidden="true" />
        <span>{t('upd.checking')}</span>
      </p>
    );
  } else if (err) {
    body = (
      <div className={`${s.state} ${s.err}`} role="alert">
        <AlertTriangle size={18} aria-hidden="true" />
        <span>
          <strong>{t('upd.errTitle')}</strong>
          <small>{errText}</small>
        </span>
        <button type="button" className="btn btn--sm" onClick={() => check.mutate()}>
          <RefreshCw size={14} aria-hidden="true" />
          {t('common.retry')}
        </button>
      </div>
    );
  } else if (rel) {
    body = (
      <div className={s.avail}>
        <div className={s.availHead}>
          <span className={s.availTitle}>
            <ArrowUpCircle size={18} aria-hidden="true" />
            <strong>{t('upd.newVersionTitle')}</strong>
            <code className="ltr" dir="ltr">
              {rel.tag}
            </code>
          </span>
          <button type="button" className="btn btn--sm btn--primary" onClick={() => openDialog(true)}>
            <ArrowUpCircle size={15} aria-hidden="true" />
            {t('upd.update')}
          </button>
        </div>
        {rel.notes && noteBlocks(rel.notes, lang).length ? <Notes notes={rel.notes} className={s.notes} /> : <p className={s.muted}>{t('upd.noNotes')}</p>}
      </div>
    );
  } else if (info.checked_at || check.isSuccess) {
    body = (
      <p className={`${s.state} ${s.ok}`} role="status">
        <CheckCircle2 size={18} aria-hidden="true" />
        <span>{t('upd.latest')}</span>
      </p>
    );
  } else {
    body = <p className={`${s.state} ${s.muted}`}>{t('upd.checkHint')}</p>;
  }

  return (
    <Section title={t('upd.section')} className={className}>
      <div id="panel-update" className={s.wrap}>
        <dl className={s.facts}>
          <div>
            <dt>{t('upd.current')}</dt>
            <dd className="ltr" dir="ltr">
              {info?.current ?? '—'}
            </dd>
          </div>
          <div>
            <dt>{t('upd.lastChecked')}</dt>
            <dd>{info?.checked_at ? dateTime(info.checked_at) : t('upd.never')}</dd>
          </div>
          <div>
            <dt>{t('upd.nextCheck')}</dt>
            <dd>{info?.next_check_at ? dateTime(info.next_check_at) : info ? t('upd.autoOff') : '—'}</dd>
          </div>
        </dl>
        <div className={s.result} aria-live="polite">
          {body}
        </div>
        <div className={s.foot}>
          <small className={s.muted}>{t('upd.checkHint')}</small>
          <button type="button" className={`btn ${s.checkBtn}`} disabled={checking || !info} aria-busy={checking} onClick={() => check.mutate()}>
            {checking ? <Loader2 size={16} className="spin" aria-hidden="true" /> : <RefreshCw size={16} aria-hidden="true" />}
            {t('upd.check')}
          </button>
        </div>
      </div>
    </Section>
  );
}
