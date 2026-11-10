import { Check, ChevronDown, Search } from 'lucide-react';
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from '@/i18n';
import s from './Select.module.css';

export interface SelectOption<V extends string | number = string> {
  value: V;
  label: ReactNode;
  /** plain text used for search / typeahead (defaults to String(label)) */
  text?: string;
  /** secondary text shown at the inline end (e.g. a UTC offset) */
  hint?: ReactNode;
  disabled?: boolean;
  /** options with the same group are listed under a small heading */
  group?: string;
}

interface SelectProps<V extends string | number> {
  value: V | null;
  onChange: (v: V) => void;
  options: SelectOption<V>[];
  /** shown when value is null / not in options */
  placeholder?: ReactNode;
  searchable?: boolean;
  searchPlaceholder?: string;
  ariaLabel?: string;
  id?: string;
  className?: string;
  /** render the value left-to-right (domains, codes) */
  ltr?: boolean;
  mono?: boolean;
  invalid?: boolean;
  disabled?: boolean;
  /** show the selected option's hint inside the closed field (default true) */
  valueHint?: boolean;
}

const MOBILE_Q = '(max-width: 720px)';

/**
 * One select for the whole UI: a button with a single custom chevron that sits
 * at the inline end (left in RTL, right in LTR) in its own reserved space, and
 * a listbox popover. On phones the list opens as a bottom panel. Popovers are
 * solid (no backdrop-filter) and animate transform/opacity only, so opening
 * never repaints or dims the page behind them.
 */
export function Select<V extends string | number>({
  value,
  onChange,
  options,
  placeholder,
  searchable,
  searchPlaceholder,
  ariaLabel,
  id,
  className,
  ltr,
  mono,
  invalid,
  disabled,
  valueHint = true,
}: SelectProps<V>) {
  const { t, dir } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const [pos, setPos] = useState<{ top?: number; bottom?: number; left: number; width: number; maxH: number } | null>(null);
  const [mobile, setMobile] = useState(() => window.matchMedia(MOBILE_Q).matches);
  const btn = useRef<HTMLButtonElement>(null);
  const pop = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const typeahead = useRef({ s: '', t: 0 });
  const uid = useId();
  const listId = `${uid}-list`;

  const selected = options.find((o) => o.value === value) ?? null;
  const textOf = (o: SelectOption<V>) => (o.text ?? (typeof o.label === 'string' ? o.label : String(o.value))).toLowerCase();
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => textOf(o).includes(q) || String(o.value).toLowerCase().includes(q));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options, query]);

  const place = useCallback(() => {
    const b = btn.current?.getBoundingClientRect();
    if (!b) return;
    const vh = window.innerHeight;
    const below = vh - b.bottom - 12;
    const above = b.top - 12;
    const width = Math.max(b.width, 220);
    let left = dir === 'rtl' ? b.right - width : b.left;
    left = Math.min(Math.max(8, left), window.innerWidth - width - 8);
    if (below >= 240 || below >= above) setPos({ top: b.bottom + 6, left, width, maxH: Math.min(360, below) });
    else setPos({ bottom: vh - b.top + 6, left, width, maxH: Math.min(360, above) });
  }, [dir]);

  const openList = () => {
    if (disabled) return;
    const m = window.matchMedia(MOBILE_Q).matches;
    setMobile(m);
    setQuery('');
    const i = Math.max(0, options.findIndex((o) => o.value === value));
    setActive(i);
    if (!m) place();
    setOpen(true);
  };
  const close = (refocus = true) => {
    setOpen(false);
    if (refocus) btn.current?.focus({ preventScroll: true });
  };
  const pick = (o: SelectOption<V>) => {
    if (o.disabled) return;
    onChange(o.value);
    close();
  };

  // Outside press / resize / scroll handling while open.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      const tg = e.target as Node;
      if (pop.current?.contains(tg) || btn.current?.contains(tg)) return;
      close(false);
    };
    const onScroll = (e: Event) => {
      if (pop.current?.contains(e.target as Node)) return;
      if (!mobile) place();
    };
    const onResize = () => !mobile && place();
    document.addEventListener('pointerdown', onDown, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, mobile, place]);

  // Focus the search box (desktop only: on phones the keyboard would cover the list) or the list.
  useLayoutEffect(() => {
    if (!open) return;
    if (searchable && !mobile) searchRef.current?.focus({ preventScroll: true });
    else listRef.current?.focus({ preventScroll: true });
  }, [open, searchable, mobile]);

  // Keep the active option in view.
  useEffect(() => {
    if (!open) return;
    const el = listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`);
    el?.scrollIntoView({ block: 'nearest' });
  }, [active, open, filtered]);

  const move = (d: number) => {
    if (!filtered.length) return;
    let i = active;
    for (let n = 0; n < filtered.length; n++) {
      i = (i + d + filtered.length) % filtered.length;
      if (!filtered[i].disabled) break;
    }
    setActive(i);
  };
  const onListKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      setActive(0);
    } else if (e.key === 'End') {
      e.preventDefault();
      setActive(filtered.length - 1);
    } else if (e.key === 'Enter' || (e.key === ' ' && !searchable)) {
      e.preventDefault();
      if (filtered[active]) pick(filtered[active]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab') {
      close(false);
    } else if (!searchable && e.key.length === 1) {
      const now = Date.now();
      const ta = typeahead.current;
      ta.s = now - ta.t > 700 ? e.key.toLowerCase() : ta.s + e.key.toLowerCase();
      ta.t = now;
      const i = filtered.findIndex((o) => textOf(o).startsWith(ta.s));
      if (i >= 0) setActive(i);
    }
  };
  const onBtnKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openList();
    }
  };

  let lastGroup: string | undefined;
  const list = (
    <div
      ref={listRef}
      id={listId}
      role="listbox"
      tabIndex={-1}
      aria-label={ariaLabel}
      aria-activedescendant={filtered[active] ? `${uid}-o${active}` : undefined}
      className={s.list}
      onKeyDown={onListKey}
    >
      {filtered.length === 0 && <div className={s.empty}>{t('common.noResults')}</div>}
      {filtered.map((o, i) => {
        const head = o.group && o.group !== lastGroup ? o.group : null;
        lastGroup = o.group;
        const sel = o.value === value;
        return (
          <div key={String(o.value)}>
            {head && <div className={s.group}>{head}</div>}
            <div
              id={`${uid}-o${i}`}
              data-idx={i}
              role="option"
              aria-selected={sel}
              aria-disabled={o.disabled || undefined}
              className={`${s.option} ${i === active ? s.optionActive : ''} ${sel ? s.optionSel : ''} ${o.disabled ? s.optionOff : ''}`}
              onPointerEnter={() => setActive(i)}
              onClick={() => pick(o)}
            >
              <span className={`${s.optLabel} ${ltr ? 'ltr' : ''} ${mono ? 'mono' : ''}`}>{o.label}</span>
              {o.hint && <span className={`${s.hint} ltr`}>{o.hint}</span>}
              <span className={s.tick} aria-hidden="true">
                {sel && <Check size={16} strokeWidth={2.6} />}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );

  return (
    <>
      <button
        ref={btn}
        id={id}
        type="button"
        className={`input ${s.trigger} ${className ?? ''}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-label={ariaLabel}
        aria-invalid={invalid || undefined}
        disabled={disabled}
        onClick={() => (open ? close() : openList())}
        onKeyDown={onBtnKey}
      >
        <span className={`${s.value} ${selected ? '' : s.placeholder} ${ltr && selected ? 'ltr' : ''} ${mono && selected ? 'mono' : ''}`}>
          {selected ? selected.label : placeholder ?? t('common.choose')}
        </span>
        {valueHint && selected?.hint && <span className={`${s.valueHint} ltr`}>{selected.hint}</span>}
        <ChevronDown size={18} className={`${s.chev} ${open ? s.chevOpen : ''}`} aria-hidden="true" />
      </button>
      {open &&
        createPortal(
          <div className={s.layer} data-veilon-popover="" dir={dir}>
            {mobile && <div className={s.scrim} onClick={() => close(false)} />}
            <div
              ref={pop}
              className={`${s.pop} ${mobile ? s.popMobile : ''}`}
              style={
                mobile || !pos
                  ? undefined
                  : { top: pos.top, bottom: pos.bottom, left: pos.left, width: pos.width, maxHeight: pos.maxH, transformOrigin: pos.top !== undefined ? 'top center' : 'bottom center' }
              }
            >
              {mobile && <div className={s.sheetHead}>{ariaLabel}</div>}
              {searchable && (
                <label className={s.search}>
                  <Search size={16} aria-hidden="true" />
                  <input
                    ref={searchRef}
                    value={query}
                    onChange={(e) => {
                      setQuery(e.target.value);
                      setActive(0);
                    }}
                    onKeyDown={onListKey}
                    placeholder={searchPlaceholder ?? t('common.search')}
                    aria-controls={listId}
                    aria-label={searchPlaceholder ?? t('common.search')}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </label>
              )}
              {list}
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
