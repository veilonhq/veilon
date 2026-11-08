import { Lock } from 'lucide-react';
import type { ReactNode } from 'react';
import { PageHeader, Section } from '@/components/misc';
import { useI18n, type MessageKey } from '@/i18n';
import { can, type Need } from '@/lib/perms';
import { useSession } from '@/store/session';
import s from '@/pages/pages.module.css';

/** Renders the page only when the signed-in admin holds what it needs. */
export function Guard({ need, title, children }: { need: Need; title: MessageKey; children: ReactNode }) {
  const { t } = useI18n();
  const admin = useSession((x) => x.admin);
  if (can(admin, need)) return <>{children}</>;
  return (
    <div>
      <PageHeader title={t(title)} />
      <Section>
        <div className={s.locked}>
          <Lock size={28} />
          <p>{need === 'sudo' ? t('adm.sudoOnly') : t('perm.denied')}</p>
        </div>
      </Section>
    </div>
  );
}
