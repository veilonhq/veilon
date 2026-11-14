import { Info, Plus, X } from 'lucide-react';
import { useI18n, type MessageKey } from '@/i18n';
import { FW_MAX, addEntries, entryKind, type FwReason } from '@/lib/firewall';
import s from './Users.module.css';

export interface FwError {
  entry: string;
  reason: FwReason;
}

/** Text of a refused entry ("entry: why"). */
export function useFwErrText() {
  const { t, n } = useI18n();
  return (e: FwError | null) => (e ? `${e.entry ? `\u2066${e.entry}\u2069: ` : ''}${t(`form.fwErr.${e.reason}` as MessageKey, { n: n(FW_MAX) })}` : null);
}

const FORMATS: { v: string; k: MessageKey }[] = [
  { v: 'example.com', k: 'form.fwKind.domain' },
  { v: 'full:api.example.com', k: 'form.fwKind.full' },
  { v: 'regexp:\\.example\\.net$', k: 'form.fwKind.regexp' },
  { v: '1.2.3.4 · 10.0.0.0/8 · 2001:db8::/32', k: 'form.fwKind.ip' },
];

/**
 * The user's own block list: a box to type or paste one or more entries
 * (lines, spaces or commas), the list with a remove button per entry, and
 * a note when an AmneziaWG inbound is selected (only addresses apply there).
 */
export function FirewallField({
  list,
  onChange,
  draft,
  onDraft,
  error,
  onError,
  awg,
}: {
  list: string[];
  onChange: (l: string[]) => void;
  draft: string;
  onDraft: (v: string) => void;
  error: FwError | null;
  onError: (e: FwError | null) => void;
  awg: boolean;
}) {
  const { t, n } = useI18n();
  const errText = useFwErrText();
  const add = () => {
    if (!draft.trim()) return;
    const r = addEntries(list, draft);
    if (r.error) {
      onError(r.error);
      return;
    }
    onError(null);
    onChange(r.list);
    onDraft('');
  };
  const msg = errText(error);
  return (
    <div className={s.fs}>
      <span className="field__hint">{t('form.fwHint')}</span>
      <div className={s.fwAddRow}>
        <textarea
          className={`input ltr mono ${s.fwInput}`}
          value={draft}
          rows={1}
          onChange={(e) => {
            onDraft(e.target.value);
            if (error) onError(null);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              add();
            }
          }}
          placeholder={t('form.fwPlaceholder')}
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          aria-label={t('form.sec.firewall')}
          aria-invalid={!!error}
        />
        <button type="button" className="btn" onClick={add} disabled={!draft.trim()}>
          <Plus size={16} />
          {t('form.fwAdd')}
        </button>
      </div>
      {msg && (
        <span className="field__error" role="alert">
          {msg}
        </span>
      )}
      {list.length === 0 ? (
        <span className={s.fwEmpty}>{t('form.fwEmpty')}</span>
      ) : (
        <>
          <div className={s.fwHead}>
            <span className="num">{t('form.fwCount', { n: n(list.length) })}</span>
            <button type="button" className={s.fwClear} onClick={() => onChange([])}>
              {t('form.fwClear')}
            </button>
          </div>
          <ul className={s.fwList}>
            {list.map((v) => (
              <li key={v} className={s.fwItem}>
                <span className={`${s.fwVal} ltr mono`} title={v}>
                  {v}
                </span>
                <span className={s.fwKind}>{t(`form.fwKind.${entryKind(v)}` as MessageKey)}</span>
                <button type="button" className={s.fwDel} onClick={() => onChange(list.filter((x) => x !== v))} aria-label={t('form.fwRemove', { v })}>
                  <X size={15} />
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {awg && (
        <div className={s.fwNote}>
          <Info size={15} />
          <span>{t('form.fwAwgNote')}</span>
        </div>
      )}
      <details className={s.fwFormats}>
        <summary>{t('form.fwFormats')}</summary>
        <dl>
          {FORMATS.map((f) => (
            <div key={f.k}>
              <dt className="ltr mono">{f.v}</dt>
              <dd>{t(f.k)}</dd>
            </div>
          ))}
        </dl>
      </details>
    </div>
  );
}
