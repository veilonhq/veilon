import { AlertTriangle, Heart, Wallet } from 'lucide-react';
import { CopyButton, PageHeader, QR } from '@/components/misc';
import { useI18n } from '@/i18n';
import { DONATE_WALLETS, type DonateWallet } from '@/lib/donate';
import s from './Donate.module.css';

function WalletCard({ w }: { w: DonateWallet }) {
  const { t } = useI18n();
  return (
    <article className={`glass ${s.card}`}>
      <div className={s.qr}>
        <QR value={w.address} size={176} label={t('donate.qr', { coin: w.coin.join(' / ') })} />
      </div>
      <div className={s.info}>
        <div className={s.top}>
          <span className={s.net}>
            <Wallet size={15} />
            {t('donate.network', { network: w.network })}
          </span>
          {w.wallet && <span className={s.app}>{t('donate.wallet', { wallet: w.wallet })}</span>}
        </div>
        <div className={s.coins} aria-label={t('donate.accepts')}>
          {w.coin.map((c) => (
            <span key={c} className="chip chip--violet ltr">
              {c}
            </span>
          ))}
        </div>
        <span className="field__label">{t('donate.address')}</span>
        <code className={`${s.addr} mono ltr`}>{w.address}</code>
        <div className={s.actions}>
          <CopyButton text={w.address} label={t('donate.copy')} />
        </div>
        {w.noteKey && (
          <p className={s.note}>
            <AlertTriangle size={15} />
            <span>{t(w.noteKey)}</span>
          </p>
        )}
      </div>
    </article>
  );
}

export default function DonatePage() {
  const { t } = useI18n();
  return (
    <div className={s.page}>
      <PageHeader title={t('donate.title')} />
      <section className={`glass ${s.hero}`}>
        <span className={s.heart} aria-hidden="true">
          <Heart size={22} />
        </span>
        <div>
          <p className={s.lead}>{t('donate.lead')}</p>
          <p className={s.thanks}>{t('donate.thanks')}</p>
        </div>
      </section>
      <div className={s.warn} role="note">
        <AlertTriangle size={18} />
        <div>
          <strong>{t('donate.warnTitle')}</strong>
          <span>{t('donate.warn')}</span>
        </div>
      </div>
      <div className={s.list}>
        {DONATE_WALLETS.map((w) => (
          <WalletCard key={w.address} w={w} />
        ))}
      </div>
    </div>
  );
}
