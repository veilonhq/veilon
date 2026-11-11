import '@fontsource-variable/vazirmatn';
import '@fontsource-variable/inter';
import '@fontsource-variable/space-grotesk';
import '@fontsource-variable/jetbrains-mono';
import '../styles/global.css';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { SubApp, LANG_KEY } from './SubApp';
import { translate } from './i18n';
import type { Lang, SubData } from './format';

declare global {
  interface Window {
    /** Injected by the server: panel default language and the page data. */
    __VEILON_SUB__?: { lang?: string; data?: SubData };
  }
}

function fontsReady(lang: Lang): Promise<unknown> {
  if (!('fonts' in document)) return Promise.resolve();
  const families = lang === 'en' ? ['Inter Variable', 'Space Grotesk Variable'] : ['Vazirmatn Variable', 'Inter Variable'];
  const sample = lang === 'en' ? 'Aa1' : 'آب۱ Aa1';
  const loads = families.map((f) => document.fonts.load(`400 16px "${f}"`, sample));
  return Promise.race([Promise.allSettled(loads), new Promise((r) => setTimeout(r, 800))]);
}

/** The page data: inlined by the server, otherwise the JSON of ?format=status. */
async function loadData(): Promise<SubData> {
  const inl = window.__VEILON_SUB__?.data;
  if (inl) return inl;
  if (import.meta.env.DEV && !/\/[^/]+\/[^/]+/.test(location.pathname)) return (await import('./sample')).sample;
  const r = await fetch(`${location.pathname}?format=status`, { headers: { Accept: 'application/json' }, cache: 'no-store' });
  if (!r.ok) throw new Error(String(r.status));
  return r.json();
}

async function boot() {
  const serverLang: Lang = window.__VEILON_SUB__?.lang === 'en' ? 'en' : 'fa';
  let lang = serverLang;
  try {
    const l = localStorage.getItem(LANG_KEY);
    if (l === 'fa' || l === 'en') lang = l;
  } catch {
    /* storage blocked */
  }
  const fonts = fontsReady(lang);
  const root = document.getElementById('root')!;
  try {
    const data = await loadData();
    await fonts;
    createRoot(root).render(
      <StrictMode>
        <SubApp data={data} serverLang={serverLang} />
      </StrictMode>,
    );
  } catch {
    root.textContent = translate(lang, 'error.load');
    root.setAttribute('role', 'alert');
    root.style.padding = '32px';
  }
}
boot();
