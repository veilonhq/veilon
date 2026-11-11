import { PageHeader } from '@/components/misc';
import { UpdateSection } from '@/components/UpdateSection';
import { useI18n } from '@/i18n';
import s from './pages.module.css';

/** Panel update (sudo admins): its own page in the More menu, moved out of Settings. */
export default function UpdatePage() {
  const { t } = useI18n();
  return (
    <div>
      <PageHeader title={t('nav.update')} subtitle={t('upd.pageSub')} />
      <div className={s.updatePage}>
        <UpdateSection />
      </div>
    </div>
  );
}
