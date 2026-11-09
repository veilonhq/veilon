import { create } from 'zustand';

export type Theme = 'dark' | 'light';
export type Lang = 'fa' | 'en';
export type UsersView = 'cards' | 'list';
/** Card density for the users and inbounds grids. */
export type Density = 'compact' | 'comfortable';

declare global {
  interface Window {
    /** Runtime config the server injects into index.html. */
    __VEILON__?: { basePath?: string; apiBase?: string; lang?: string };
  }
}

/**
 * The language to start in: the browser's own choice if it made one,
 * otherwise the panel's default language from the server, otherwise Persian.
 */
export function initialLang(): Lang {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem('veilon.lang');
  } catch {
    /* storage blocked */
  }
  if (stored === 'fa' || stored === 'en') return stored;
  return window.__VEILON__?.lang === 'en' ? 'en' : 'fa';
}

/** True when this browser picked a language itself (it then wins over the server default). */
export function hasOwnLang() {
  try {
    const v = localStorage.getItem('veilon.lang');
    return v === 'fa' || v === 'en';
  } catch {
    return false;
  }
}

interface UiState {
  theme: Theme;
  lang: Lang;
  usersView: UsersView;
  density: Density;
  paletteOpen: boolean;
  /** The panel's time zone (settings); every absolute time is shown in it. */
  tz: string | undefined;
  setTz: (tz: string | undefined) => void;
  setTheme: (t: Theme) => void;
  toggleTheme: () => void;
  setLang: (l: Lang) => void;
  /** Follow the panel's default language unless this browser chose one. */
  applyDefaultLang: (l: Lang) => void;
  setDensity: (d: Density) => void;
  setUsersView: (v: UsersView) => void;
  setPalette: (open: boolean) => void;
}

const get = (k: string, d: string) => {
  try {
    return localStorage.getItem(k) ?? d;
  } catch {
    return d;
  }
};

function applyTheme(t: Theme) {
  document.documentElement.dataset.theme = t;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', t === 'dark' ? '#07080C' : '#F2F1EC');
}
function applyLang(l: Lang) {
  document.documentElement.lang = l;
  document.documentElement.dir = l === 'fa' ? 'rtl' : 'ltr';
}

/** The zone if the browser knows it, otherwise undefined (browser zone). */
function validTz(tz: string | undefined) {
  if (!tz) return undefined;
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: tz });
    return tz;
  } catch {
    return undefined;
  }
}

export const useUi = create<UiState>((set, getState) => {
  const theme = get('veilon.theme', 'dark') as Theme;
  const lang = initialLang();
  applyTheme(theme);
  applyLang(lang);
  return {
    theme,
    lang,
    usersView: get('veilon.usersView', 'cards') as UsersView,
    density: get('veilon.density', 'compact') === 'comfortable' ? 'comfortable' : 'compact',
    paletteOpen: false,
    tz: undefined,
    setTz: (tz) => {
      if (tz !== getState().tz) set({ tz: validTz(tz) });
    },
    setTheme: (t) => {
      localStorage.setItem('veilon.theme', t);
      applyTheme(t);
      set({ theme: t });
    },
    toggleTheme: () => getState().setTheme(getState().theme === 'dark' ? 'light' : 'dark'),
    setLang: (l) => {
      localStorage.setItem('veilon.lang', l);
      applyLang(l);
      set({ lang: l });
    },
    applyDefaultLang: (l) => {
      if (hasOwnLang() || getState().lang === l) return;
      applyLang(l);
      set({ lang: l });
    },
    setDensity: (d) => {
      try {
        localStorage.setItem('veilon.density', d);
      } catch {
        /* storage blocked */
      }
      set({ density: d });
    },
    setUsersView: (v) => {
      localStorage.setItem('veilon.usersView', v);
      set({ usersView: v });
    },
    setPalette: (open) => set({ paletteOpen: open }),
  };
});
