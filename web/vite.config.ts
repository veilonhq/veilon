import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

/**
 * The panel is served under a random path (/<panel_path>/) and the app uses
 * client-side routes beneath it. Relative asset URLs (base './') would break on
 * deep links such as /<panel_path>/users/12, so a tiny inline script sets a
 * <base href="/<panel_path>/"> before any asset tag is parsed.
 */
function runtimeBase(): Plugin {
  return {
    name: 'veilon-runtime-base',
    transformIndexHtml: {
      order: 'pre',
      handler() {
        return [
          {
            tag: 'script',
            injectTo: 'head-prepend',
            children:
              "(function(){var s=location.pathname.split('/').filter(Boolean)[0];var b=document.createElement('base');b.href=s?'/'+s+'/':'/';document.head.prepend(b);window.__VEILON_BASE__=s?'/'+s:'';})();",
          },
        ];
      },
    },
  };
}

export default defineConfig(({ command }) => ({
  base: './',
  plugins: [react(), command === 'build' ? runtimeBase() : null],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { host: true, port: 5173, open: false },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 900, sourcemap: false },
}));
