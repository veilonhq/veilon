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

async function boot() {
  if (IS_MOCK) {
    const { mockTransport } = await import('./mock');
    setTransport(mockTransport(API_BASE));
  }
  const { App } = await import('./app/App');
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
boot();
