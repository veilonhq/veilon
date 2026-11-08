import { AnimatePresence, motion, useDragControls, type PanInfo } from 'framer-motion';
import { X } from 'lucide-react';
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from '@/i18n';
import s from './Overlay.module.css';

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

/** Traps Tab inside `ref`, closes on Escape, restores focus on unmount. */
export function useFocusTrap(ref: React.RefObject<HTMLElement>, active: boolean, onClose: () => void) {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    if (!active) return;
    const prev = document.activeElement as HTMLElement | null;
    const el = ref.current;
    const t = setTimeout(() => {
      // focus the marked field if there is one, otherwise the dialog itself (Tab then enters it)
      const auto = el?.querySelector<HTMLElement>('[data-autofocus]');
      (auto ?? el)?.focus({ preventScroll: true });
    }, 30);
    const onKey = (e: KeyboardEvent) => {
      // A select popover (portal) owns the keyboard while it is open.
      if (document.querySelector('[data-veilon-popover]')) return;
      if (e.key === 'Escape') {
        e.stopPropagation();
        closeRef.current();
        return;
      }
      if (e.key !== 'Tab' || !el) return;
      const items = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((x) => x.offsetParent !== null);
      if (!items.length) return;
      const first = items[0],
        last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKey, true);
    return () => {
      clearTimeout(t);
      document.removeEventListener('keydown', onKey, true);
      prev?.focus?.({ preventScroll: true });
    };
  }, [active, ref]);
}

/**
 * Scroll lock for open overlays. Toggling body overflow is what usually makes
 * a page jump (the scrollbar disappears and the layout widens); the root keeps
 * a stable gutter (global.css: scrollbar-gutter) and we lock with
 * overscroll-behavior + overflow on <html> only while something is open, with
 * a counter so stacked overlays don't unlock each other.
 */
let locks = 0;
export function useLockScroll(active: boolean) {
  useEffect(() => {
    if (!active) return;
    const el = document.documentElement;
    if (locks++ === 0) el.classList.add('scroll-locked');
    return () => {
      if (--locks === 0) el.classList.remove('scroll-locked');
    };
  }, [active]);
}

/**
 * Scrim colour pair for the current theme. Scrims animate background-color,
 * never opacity: a full-screen opacity layer forces its own compositing pass
 * over everything below it (including any blurred surfaces), which is the
 * dim-then-snap flash seen in Android WebViews.
 */
export function scrimColors(): [string, string] {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--scrim').trim() || 'rgba(3, 4, 7, 0.62)';
  const m = v.match(/rgba?\(([^)]+)\)/);
  if (!m) return ['rgba(0, 0, 0, 0)', v];
  const [r, g, b] = m[1].split(',').map((x) => x.trim());
  return [`rgba(${r}, ${g}, ${b}, 0)`, v];
}

function Scrim({ onClick }: { onClick: () => void }) {
  const [[from, to]] = useState(scrimColors);
  return (
    <motion.div
      className={s.scrim}
      initial={{ backgroundColor: from }}
      animate={{ backgroundColor: to }}
      exit={{ backgroundColor: from }}
      transition={{ duration: 0.2, ease: 'easeOut' }}
      onClick={onClick}
    />
  );
}

export function useIsMobile(q = '(max-width: 720px)') {
  const [m, setM] = useState(() => window.matchMedia(q).matches);
  useEffect(() => {
    const mq = window.matchMedia(q);
    const on = () => setM(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, [q]);
  return m;
}

interface SheetProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  headerExtra?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  width?: number;
  labelledBy?: string;
  /** 'tall' (default): fixed 94dvh on phones so loading/step changes never move the top edge. 'auto': fits content. */
  size?: 'tall' | 'auto';
}

/** Slide-over on desktop (from the inline end), bottom sheet with drag-to-dismiss on mobile. */
export function Sheet({ open, onClose, title, subtitle, headerExtra, footer, children, width = 560, size = 'tall' }: SheetProps) {
  const { t, dir } = useI18n();
  const mobile = useIsMobile();
  const ref = useRef<HTMLDivElement>(null);
  const id = useId();
  const drag = useDragControls();
  useFocusTrap(ref, open, onClose);
  useLockScroll(open);

  const fromX = dir === 'rtl' ? '-104%' : '104%';
  const variants = mobile
    ? { hidden: { y: '100%' }, shown: { y: 0 } }
    : { hidden: { x: fromX }, shown: { x: 0 } };

  const onDragEnd = (_: unknown, info: PanInfo) => {
    if (info.offset.y > 120 || info.velocity.y > 600) onClose();
  };

  return createPortal(
    <AnimatePresence>
      {open && (
        <div className={s.layer}>
          <Scrim onClick={onClose} />
          <motion.div
            ref={ref}
            role="dialog"
            aria-modal="true"
            aria-labelledby={id}
            tabIndex={-1}
            className={`${s.sheet} ${mobile ? s.bottom : s.side} ${mobile && size === 'tall' ? s.tall : ''}`}
            style={mobile ? undefined : { width: `min(${width}px, calc(100vw - 24px))` }}
            variants={variants}
            initial="hidden"
            animate="shown"
            exit="hidden"
            transition={{ type: 'spring', damping: 34, stiffness: 320, mass: 0.9 }}
            drag={mobile ? 'y' : false}
            dragControls={drag}
            dragListener={false}
            dragConstraints={{ top: 0, bottom: 0 }}
            dragElastic={{ top: 0, bottom: 0.6 }}
            onDragEnd={onDragEnd}
          >
            {mobile && (
              <div className={s.handleZone} onPointerDown={(e) => drag.start(e)}>
                <span className={s.handle} />
              </div>
            )}
            <header className={s.head} onPointerDown={mobile ? (e) => drag.start(e) : undefined}>
              <div className={s.headText}>
                <h2 id={id} className={`${s.title} display`}>
                  {title}
                </h2>
                {subtitle && <div className={s.subtitle}>{subtitle}</div>}
              </div>
              {headerExtra}
              <button className="btn btn--icon btn--ghost" onClick={onClose} aria-label={t('common.close')}>
                <X size={20} />
              </button>
            </header>
            <div className={s.body}>{children}</div>
            {footer && <footer className={s.foot}>{footer}</footer>}
          </motion.div>
        </div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

interface ModalProps {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
  label: string;
  className?: string;
  align?: 'center' | 'top';
}

export function Modal({ open, onClose, children, label, className, align = 'center' }: ModalProps) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref, open, onClose);
  useLockScroll(open);
  return createPortal(
    <AnimatePresence>
      {open && (
        <div className={`${s.layer} ${align === 'top' ? s.alignTop : s.alignCenter}`}>
          <Scrim onClick={onClose} />
          <motion.div
            ref={ref}
            role="dialog"
            aria-modal="true"
            aria-label={label}
            tabIndex={-1}
            className={`${s.modal} ${className ?? ''}`}
            // transform only (no opacity layer over the page); the scrim's colour fade softens it
            initial={{ y: 24, scale: 0.94 }}
            animate={{ y: 0, scale: 1 }}
            exit={{ y: 16, scale: 0.96, transition: { duration: 0.14 } }}
            transition={{ type: 'spring', damping: 30, stiffness: 380 }}
          >
            {children}
          </motion.div>
        </div>
      )}
    </AnimatePresence>,
    document.body,
  );
}
