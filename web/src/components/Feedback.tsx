import { AnimatePresence, motion } from 'framer-motion';
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react';
import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from '@/i18n';
import { useConfirm, useToasts } from '@/store/feedback';
import { Modal } from './Overlay';
import s from './Feedback.module.css';

export function Toaster() {
  const { toasts, dismiss } = useToasts();
  const { t } = useI18n();
  return createPortal(
    <div className={s.toasts} role="region" aria-live="polite" aria-label="Notifications">
      <AnimatePresence initial={false}>
        {toasts.map((x) => (
          <motion.div
            key={x.id}
            layout
            // transform + clip only: no opacity layer over the page
            initial={{ y: -18, scale: 0.96, clipPath: 'inset(-30px -30px 120px -30px)' }}
            animate={{ y: 0, scale: 1, clipPath: 'inset(-30px -30px -60px -30px)' }}
            exit={{ scale: 0.95, clipPath: 'inset(-30px -30px 120px -30px)', transition: { duration: 0.15 } }}
            className={`${s.toast} ${s[x.kind]}`}
            role={x.kind === 'error' ? 'alert' : 'status'}
          >
            <span className={s.icon}>
              {x.kind === 'success' ? <CheckCircle2 size={18} /> : x.kind === 'error' ? <XCircle size={18} /> : <Info size={18} />}
            </span>
            <div className={s.text}>
              <strong>{x.title}</strong>
              {x.body && <span>{x.body}</span>}
            </div>
            <button className={s.close} onClick={() => dismiss(x.id)} aria-label={t('common.close')}>
              <X size={15} />
            </button>
          </motion.div>
        ))}
      </AnimatePresence>
    </div>,
    document.body,
  );
}

export function ConfirmHost() {
  const { req, close } = useConfirm();
  const { t, n } = useI18n();
  const [typed, setTyped] = useState('');
  const [num, setNum] = useState(0);
  useEffect(() => {
    setTyped('');
    setNum(req?.numberInput?.initial ?? req?.choiceInput?.initial ?? 0);
  }, [req]);
  const blocked = !!req?.typeToConfirm && typed.trim() !== req.typeToConfirm;
  const submit = () => {
    if (blocked) return;
    close(req?.numberInput || req?.choiceInput ? { value: num } : {});
  };
  return (
    <Modal open={!!req} onClose={() => close(false)} label={req?.title ?? ''}>
      {req && (
        <form
          className={s.confirm}
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div className={`${s.badge} ${req.danger ? s.badgeDanger : ''}`} aria-hidden="true">
            {req.danger ? <AlertTriangle size={26} /> : <Info size={26} />}
          </div>
          <h2 className={`${s.title} display`}>{req.title}</h2>
          {req.body && <p className={s.body}>{req.body}</p>}
          {req.numberInput && (
            <label className="field">
              <span className="field__label">{req.numberInput.label}</span>
              <div className={s.stepper}>
                {[7, 30, 90].map((d) => (
                  <button type="button" key={d} className="pill" aria-pressed={num === d} onClick={() => setNum(d)}>
                    {n(d)} {t('common.days')}
                  </button>
                ))}
                <input
                  className="input num"
                  type="number"
                  inputMode="numeric"
                  min={req.numberInput.min ?? 1}
                  max={req.numberInput.max ?? 3650}
                  value={num}
                  onChange={(e) => setNum(Number(e.target.value))}
                  data-autofocus
                  aria-label={req.numberInput.label}
                />
              </div>
            </label>
          )}
          {req.choiceInput && (
            <div className="field" role="radiogroup" aria-label={req.choiceInput.label}>
              <span className="field__label">{req.choiceInput.label}</span>
              <div className={s.stepper}>
                {req.choiceInput.options.map((o) => (
                  <button type="button" role="radio" key={o.value} className="pill" aria-checked={num === o.value} aria-pressed={num === o.value} onClick={() => setNum(o.value)}>
                    {o.color && <span className={s.choiceDot} style={{ background: o.color }} aria-hidden="true" />}
                    {o.label}
                  </button>
                ))}
              </div>
            </div>
          )}
          {req.typeToConfirm && (
            <label className="field">
              <span className="field__label">{t('common.typeToConfirm', { word: req.typeToConfirm })}</span>
              <input className="input ltr" value={typed} onChange={(e) => setTyped(e.target.value)} data-autofocus autoComplete="off" spellCheck={false} />
            </label>
          )}
          <div className={s.actions}>
            <button type="button" className="btn" onClick={() => close(false)} data-autofocus={!req.typeToConfirm && !req.numberInput && !req.choiceInput ? true : undefined}>
              {t('common.cancel')}
            </button>
            <button type="submit" className={`btn ${req.danger ? 'btn--solid-danger' : 'btn--primary'}`} disabled={blocked}>
              {req.confirmLabel ?? t('common.confirm')}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}
