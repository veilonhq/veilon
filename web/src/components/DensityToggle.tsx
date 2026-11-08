import { Rows2, Rows4 } from 'lucide-react';
import { useI18n } from '@/i18n';
import { useUi } from '@/store/ui';

/** Compact / comfortable card density (shared by users and inbounds, kept in localStorage). */
export function DensityToggle({ disabled, className }: { disabled?: boolean; className?: string }) {
  const { t } = useI18n();
  const density = useUi((x) => x.density);
  const setDensity = useUi((x) => x.setDensity);
  return (
    <div className={`seg ${className ?? ''}`} role="group" aria-label={t('density.label')} data-density-toggle="">
      <button type="button" aria-pressed={density === 'compact'} onClick={() => setDensity('compact')} disabled={disabled} aria-label={t('density.compact')} title={t('density.compact')}>
        <Rows4 size={16} />
      </button>
      <button type="button" aria-pressed={density === 'comfortable'} onClick={() => setDensity('comfortable')} disabled={disabled} aria-label={t('density.comfortable')} title={t('density.comfortable')}>
        <Rows2 size={16} />
      </button>
    </div>
  );
}
