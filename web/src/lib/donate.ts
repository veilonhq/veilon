import type { MessageKey } from '@/i18n';

/**
 * Donation wallets shown on the "Support the developer" page. Add an entry
 * to show another address; each one gets its own card, network label, copy
 * button and QR code (generated locally).
 */
export interface DonateWallet {
  /** Coins accepted at this address, shown as chips. */
  coin: string[];
  /** The only network this address works on. */
  network: string;
  address: string;
  /** Wallet app, shown as a hint. */
  wallet?: string;
  /** Extra network warning for this address. */
  noteKey?: MessageKey;
}

export const DONATE_WALLETS: DonateWallet[] = [
  {
    coin: ['TON', 'USDT (TON)'],
    network: 'TON',
    address: 'UQCr7FXmeLsKLplHAgb1HIT0rxmQJpvAh4w2pST-DBGicbi2',
    wallet: 'Tonkeeper',
    noteKey: 'donate.noteTon',
  },
];
