import '@fontsource-variable/vazirmatn';
import '@fontsource-variable/inter';
import '@fontsource-variable/space-grotesk';
import '@fontsource-variable/jetbrains-mono';
import 'uplot/dist/uPlot.min.css';
import './styles/global.css';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { API_BASE, IS_MOCK } from './app/base';
import { setTransport } from './api/client';

// Wait (briefly) for the UI fonts before the first render: painting with a
// fallback font and then swapping wraps text differently and shifts the layout.
// Fonts are served by the panel itself, so this normally takes a few ms.
function fontsReady(): Promise<unknown> {
  if (!('fonts' in document)) return Promise.resolve();
  const lang = localStorage.getItem('veilon.lang') ?? document.documentElement.lang ?? 'fa';
  const families = lang === 'en' ? ['Inter Variable', 'Space Grotesk Variable'] : ['Vazirmatn Variable', 'Inter Variable'];
  // the sample text picks the unicode-range subsets to fetch (Arabic script + Latin)
  const sample = lang === 'en' ? 'Aa1' : 'آب۱ Aa1';
  const loads = families.map((f) => document.fonts.load(`400 16px "${f}"`, sample));
  return Promise.race([Promise.allSettled(loads), new Promise((r) => setTimeout(r, 800))]);
}

async function boot() {
  const fonts = fontsReady();
  if (IS_MOCK) {
    const { mockTransport } = await import('./mock');
    setTransport(mockTransport(API_BASE));
  }
  const { App } = await import('./app/App');
  await fonts;
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
boot();
