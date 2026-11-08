import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useState } from 'react';
import { AUDIT_PER_PAGE, useAudit } from '@/api/hooks';
import { EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { useI18n } from '@/i18n';
import s from './pages.module.css';

const PER = AUDIT_PER_PAGE;

function tone(action: string) {
  if (action.includes('delete') || action.includes('revoke')) return 'danger';
  if (action.includes('create') || action.includes('acme') || action.includes('upload')) return 'accent';
  if (action.includes('restart') || action.includes('settings')) return 'warn';
  if (action.includes('login')) return 'violet';
  return 'muted';
}

export default function AuditPage() {
  const { t, n, dateTime, relative, dir } = useI18n();
  const [page, setPage] = useState(1);
  const q = useAudit(page);
  const pages = q.data ? Math.max(1, Math.ceil(q.data.total / PER)) : 1;
  const Prev = dir === 'rtl' ? ChevronRight : ChevronLeft;
  const Next = dir === 'rtl' ? ChevronLeft : ChevronRight;

  return (
    <div>
      <PageHeader title={t('audit.title')} subtitle={t('audit.subtitle')} />
      {q.isError && !q.data ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : q.isPending ? (
        <div className={s.stack}>
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} h={64} r={18} />
          ))}
        </div>
      ) : q.data.items.length === 0 ? (
        <EmptyState kind="logs" title={t('audit.empty')} />
      ) : (
        <>
          <ol className={`${s.audit} ${q.isPlaceholderData ? s.dim : ''}`}>
            {q.data.items.map((e) => (
              <li key={e.id} className={s.auditItem}>
                <span className={`${s.auditDot} ${s['dot_' + tone(e.action)]}`} aria-hidden="true" />
                <div className={`glass ${s.auditCard}`}>
                  <div className={s.auditMain}>
                    <code className={`mono ltr ${s.auditAction}`}>{e.action}</code>
                    <span className={s.auditTarget}>{e.target}</span>
                  </div>
                  <div className={s.auditMeta}>
                    <span>
                      <b className="ltr">{e.admin}</b> · <span className="mono ltr">{e.ip}</span>
                    </span>
                    <time dateTime={e.created_at} title={dateTime(e.created_at)}>
                      {relative(e.created_at)}
                    </time>
                  </div>
                </div>
              </li>
            ))}
          </ol>
          <nav className={s.pager} aria-label={t('audit.page', { n: page, total: pages })}>
            <button className="btn btn--sm btn--icon" disabled={page <= 1} onClick={() => setPage((p) => p - 1)} aria-label={t('audit.prev')}>
              <Prev size={17} />
            </button>
            <span className="num">{t('audit.page', { n: n(page), total: n(pages) })}</span>
            <button className="btn btn--sm btn--icon" disabled={page >= pages} onClick={() => setPage((p) => p + 1)} aria-label={t('audit.next')}>
              <Next size={17} />
            </button>
          </nav>
        </>
      )}
    </div>
  );
}
