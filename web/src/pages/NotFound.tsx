import { Link } from 'react-router-dom';
import { EmptyState } from '@/components/misc';
import { useI18n } from '@/i18n';

export default function NotFound() {
  const { t } = useI18n();
  return (
    <EmptyState
      kind="search"
      title={t('common.notFound')}
      action={
        <Link className="btn btn--primary" to="/">
          {t('common.goHome')}
        </Link>
      }
    />
  );
}
