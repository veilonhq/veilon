import { AnimatePresence, motion } from 'framer-motion';
import { Languages, LogOut, Moon, MoreHorizontal, Search, Sun, UserRound } from 'lucide-react';
import { Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useStats } from '@/api/hooks';
import { useI18n } from '@/i18n';
import { useSession } from '@/store/session';
import { useUi } from '@/store/ui';
import { CommandPalette } from './CommandPalette';
import { RouteBoundary } from './RouteBoundary';
import { PRIMARY, SECONDARY, allowed } from './nav';
import { Logo } from './Logo';
import s from './Shell.module.css';

/**
 * Language switch. The full form is a segmented "فارسی | English" control
 * with the current language highlighted; the compact form (top bar) is one
 * button that shows the current language and switches to the other.
 */
export function LangSwitch({ compact }: { compact?: boolean }) {
  const { t, lang } = useI18n();
  const setLang = useUi((x) => x.setLang);
  if (compact) {
    return (
      <button
        className="btn btn--sm btn--ghost"
        onClick={() => setLang(lang === 'fa' ? 'en' : 'fa')}
        aria-label={t('common.switchLangLabel')}
        title={t('common.switchLangLabel')}
      >
        <Languages size={17} />
        <span className={s.langCode}>{lang === 'fa' ? 'فا' : 'EN'}</span>
      </button>
    );
  }
  return (
    <div className={`seg ${s.langSeg}`} role="group" aria-label={t('common.language')} data-lang-switch="">
      <Languages size={15} aria-hidden="true" className={s.langIcon} />
      <button type="button" lang="fa" aria-pressed={lang === 'fa'} onClick={() => setLang('fa')}>
        فارسی
      </button>
      <button type="button" lang="en" aria-pressed={lang === 'en'} onClick={() => setLang('en')}>
        English
      </button>
    </div>
  );
}

export function ThemeLangToggles({ compact }: { compact?: boolean }) {
  const { t } = useI18n();
  const { theme, toggleTheme } = useUi();
  return (
    <>
      <LangSwitch compact={compact} />
      <button
        className="btn btn--sm btn--icon btn--ghost"
        onClick={toggleTheme}
        aria-label={theme === 'dark' ? t('common.theme.light') : t('common.theme.dark')}
        title={t('common.toggleTheme')}
      >
        <AnimatePresence mode="wait" initial={false}>
          <motion.span key={theme} initial={{ rotate: -90, scale: 0.6 }} animate={{ rotate: 0, scale: 1 }} exit={{ rotate: 90, scale: 0.6 }} transition={{ duration: 0.14 }} style={{ display: 'grid' }}>
            {theme === 'dark' ? <Sun size={17} /> : <Moon size={17} />}
          </motion.span>
        </AnimatePresence>
      </button>
    </>
  );
}

type Pos = React.CSSProperties | undefined;

/**
 * Fixed position for a popover anchored to `ref` (rendered in a portal so it
 * is never inside the blurred dock/top bar: an opacity animation inside a
 * backdrop-filter layer is what made Android flash the whole page).
 */
function useAnchor(ref: React.RefObject<HTMLElement>, open: boolean, side: 'above' | 'below'): Pos {
  const [pos, setPos] = useState<Pos>();
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = ref.current?.getBoundingClientRect();
      if (!r) return;
      const rtl = document.documentElement.dir === 'rtl';
      const edge = rtl ? { left: Math.max(8, r.left) } : { right: Math.max(8, window.innerWidth - r.right) };
      setPos(side === 'above' ? { position: 'fixed', bottom: window.innerHeight - r.top + 10, ...edge } : { position: 'fixed', top: r.bottom + 8, ...edge });
    };
    place();
    window.addEventListener('resize', place);
    return () => window.removeEventListener('resize', place);
  }, [open, ref, side]);
  return pos;
}

function useOutside(open: boolean, refs: React.RefObject<HTMLElement>[], close: () => void) {
  const cb = useRef(close);
  cb.current = close;
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (refs.some((r) => r.current?.contains(e.target as Node))) return;
      cb.current();
    };
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && cb.current();
    document.addEventListener('pointerdown', down, true);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', down, true);
      document.removeEventListener('keydown', esc);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
}

function TopBar() {
  const { t } = useI18n();
  const admin = useSession((x) => x.admin);
  const logout = useSession((x) => x.logout);
  const setPalette = useUi((x) => x.setPalette);
  const navigate = useNavigate();
  const stats = useStats(5000);
  const running = stats.data?.core.running;
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
  const [menu, setMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuPop = useRef<HTMLDivElement>(null);
  const menuPos = useAnchor(menuRef, menu, 'below');
  useOutside(menu, [menuRef, menuPop], () => setMenu(false));

  return (
    <div className={s.topbar}>
      <NavLink to="/" className={s.brand} aria-label={t('app.name')}>
        <Logo size={34} />
        <span className={s.brandText}>
          <strong className="display">Veilon</strong>
          <span className={s.brandSub}>
            <span className={`${s.liveDot} ${running === false ? s.liveDotOff : ''}`} />
            {running === false ? t('dash.coreStopped') : t('app.tagline')}
          </span>
        </span>
      </NavLink>
      <div className={s.topActions}>
        <button className={s.searchBtn} onClick={() => setPalette(true)} aria-label={t('cmd.open')}>
          <Search size={16} />
          <span>{t('cmd.placeholder')}</span>
          <span className={s.kbds}>
            <kbd className="kbd">{isMac ? '⌘' : 'Ctrl'}</kbd>
            <kbd className="kbd">K</kbd>
          </span>
        </button>
        <div className={s.toggles}>
          <ThemeLangToggles compact />
        </div>
        <div className={s.avatarWrap} ref={menuRef}>
          <button className={s.avatar} onClick={() => setMenu((m) => !m)} aria-haspopup="menu" aria-expanded={menu} aria-label={admin?.username ?? 'admin'}>
            {(admin?.username ?? 'A').slice(0, 1).toUpperCase()}
          </button>
          {createPortal(
          <AnimatePresence>
            {menu && (
              <motion.div
                ref={menuPop}
                className={s.menu}
                style={menuPos}
                role="menu"
                initial={{ clipPath: 'inset(-40px -40px 320px -40px)', y: -6 }}
                animate={{ clipPath: 'inset(-40px -40px -70px -40px)', y: 0 }}
                exit={{ clipPath: 'inset(-40px -40px 320px -40px)', y: -6 }}
                transition={{ duration: 0.16, ease: [0.22, 1, 0.36, 1] }}
              >
                <div className={s.menuHead}>
                  <strong className="ltr">{admin?.username}</strong>
                  <span>{admin?.is_sudo ? t('adm.sudo') : t('adm.regular')}</span>
                </div>
                <button
                  role="menuitem"
                  className={`${s.menuItem} ${s.menuItemPlain}`}
                  onClick={() => {
                    setMenu(false);
                    navigate('/settings#account');
                  }}
                >
                  <UserRound size={16} />
                  {t('nav.account')}
                </button>
                <button role="menuitem" className={s.menuItem} onClick={logout}>
                  <LogOut size={16} />
                  {t('common.logout')}
                </button>
              </motion.div>
            )}
          </AnimatePresence>,
          document.body,
          )}
        </div>
      </div>
    </div>
  );
}

function Dock() {
  const { t } = useI18n();
  const loc = useLocation();
  const nav = useNavigate();
  const setPalette = useUi((x) => x.setPalette);
  const [more, setMore] = useState(false);
  const moreRef = useRef<HTMLDivElement>(null);
  const morePop = useRef<HTMLDivElement>(null);
  const morePos = useAnchor(moreRef, more, 'above');
  const admin = useSession((x) => x.admin);
  const primary = allowed(PRIMARY, admin);
  const secondary = allowed(SECONDARY, admin);
  const isActive = (to: string) => (to === '/' ? loc.pathname === '/' : loc.pathname.startsWith(to));
  const secondaryActive = secondary.some((x) => isActive(x.to));

  useEffect(() => {
    setMore(false);
  }, [loc.pathname]);
  useOutside(more, [moreRef, morePop], () => setMore(false));

  return (
    <nav className={s.dockWrap} aria-label={t('nav.main')}>
      <div className={s.dock} ref={moreRef}>
        {primary.map((it) => {
          const active = isActive(it.to);
          return (
            <NavLink key={it.to} to={it.to} end={it.to === '/'} className={`${s.dockItem} ${active ? s.dockActive : ''}`} aria-current={active ? 'page' : undefined}>
              {active && <motion.span layoutId="dock-blob" className={s.blob} transition={{ type: 'spring', stiffness: 420, damping: 34 }} />}
              <it.icon size={21} strokeWidth={active ? 2.3 : 1.9} />
              <span className={s.dockLabel}>{t(it.short ?? it.key)}</span>
            </NavLink>
          );
        })}
        <button
          className={`${s.dockItem} ${secondaryActive ? s.dockActive : ''}`}
          onClick={() => setMore((m) => !m)}
          aria-haspopup="menu"
          aria-expanded={more}
        >
          {secondaryActive && <motion.span layoutId="dock-blob" className={s.blob} transition={{ type: 'spring', stiffness: 420, damping: 34 }} />}
          <MoreHorizontal size={21} />
          <span className={s.dockLabel}>{t('nav.more')}</span>
        </button>
        <span className={s.dockSep} aria-hidden="true" />
        <button className={`${s.dockItem} ${s.dockSearch}`} onClick={() => setPalette(true)} aria-label={t('cmd.open')}>
          <Search size={21} />
          <span className={s.dockLabel}>{t('nav.search')}</span>
        </button>

        {createPortal(
        <AnimatePresence>
          {more && (
            <motion.div
              ref={morePop}
              className={s.more}
              style={morePos}
              role="menu"
              // clip + transform only: no opacity layer next to the dock (Android
              // WebViews flashed the whole page when this faded in)
              initial={{ clipPath: 'inset(480px -40px -70px -40px)', y: 8 }}
              animate={{ clipPath: 'inset(-40px -40px -70px -40px)', y: 0 }}
              exit={{ clipPath: 'inset(480px -40px -70px -40px)', y: 8 }}
              transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
            >
              {secondary.map((it) => (
                <button key={it.to} role="menuitem" className={`${s.moreItem} ${isActive(it.to) ? s.moreActive : ''}`} onClick={() => nav(it.to)}>
                  <span className={s.moreIcon}>
                    <it.icon size={19} />
                  </span>
                  {t(it.key)}
                </button>
              ))}
              <div className={s.moreToggles}>
                <ThemeLangToggles />
              </div>
            </motion.div>
          )}
        </AnimatePresence>,
        document.body,
        )}
      </div>
    </nav>
  );
}

export function Shell({ onReady, fallback }: { onReady?: () => void; fallback?: React.ReactNode }) {
  const { t } = useI18n();
  const loc = useLocation();
  const setPalette = useUi((x) => x.setPalette);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPalette(!useUi.getState().paletteOpen);
      }
      if (e.key === '/' && !(e.target as HTMLElement).closest('input,textarea,select,[contenteditable]')) {
        e.preventDefault();
        setPalette(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [setPalette]);
  useEffect(() => {
    if (!loc.hash) window.scrollTo({ top: 0 });
  }, [loc.pathname, loc.hash]);
  // Warm the route chunks once the shell is up so dock taps render instantly.
  useEffect(() => {
    if (!onReady) return;
    const w = window as Window & { requestIdleCallback?: (cb: () => void, o?: { timeout: number }) => number };
    if (w.requestIdleCallback) w.requestIdleCallback(onReady, { timeout: 2500 });
    else setTimeout(onReady, 1200);
  }, [onReady]);

  return (
    <div className={s.shell}>
      <a href="#main" className={s.skip}>
        {t('common.skip')}
      </a>
      <TopBar />
      <main id="main" className={s.main} tabIndex={-1}>
        {/* Enter transition is CSS-only and never touches opacity: page content is
            visible even if an animation never runs (see Shell.module.css .page). */}
        {/* One Suspense boundary that outlives navigation: dock taps run in a
            transition (router v7_startTransition), so while a route chunk is
            still loading the current page stays on screen instead of being
            replaced by a skeleton for a frame. */}
        <Suspense fallback={fallback ?? null}>
          <div key={loc.pathname} className={s.page}>
            <RouteBoundary resetKey={loc.pathname}>
              <Outlet />
            </RouteBoundary>
          </div>
        </Suspense>
      </main>
      <Dock />
      <CommandPalette />
    </div>
  );
}

