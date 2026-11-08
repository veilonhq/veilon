# Veilon web UI

Vite + React 18 + TypeScript. "Signal Observatory" design: dark ink + aurora/grain backdrop, floating dock, ⌘K/Ctrl+K command palette, Persian (RTL, default) and English.

```
npm install
npm run dev:mock     # fully clickable UI with an in-memory fake API (any login works), open http://localhost:5173/panel/
npm run dev          # against a real panel (proxy or same origin)
npm run build        # → dist/ (relative asset base, served by the Go panel under /<panel_path>/)
npm run preview:mock # mock build served under /x7k2p9/ like the real panel
```

- Router basename = first path segment of `location.pathname`; API base = `<basename>/api/v1` (see `src/app/base.ts`).
  A tiny inline script in the built `index.html` sets `<base href="/<panel_path>/">` so deep links such as `/<panel_path>/users` still load `./assets/*`.
  The Go server must serve `index.html` for any unknown path under `/<panel_path>/` (SPA fallback).
- API contract: `../docs/api-v1.md` — hooks in `src/api/hooks.ts`, types in `src/api/types.ts`, mock in `src/mock/index.ts` (never bundled into the production build).
- Strings: `src/i18n/fa.ts` (source of keys) and `src/i18n/en.ts`. Numbers/dates use `Intl` (`fa-IR-u-ca-persian` → Persian digits + Jalali).

Regression check against the real binary (dock navigation incl. a "stalled animations" pass that emulates throttled Android WebViews): see the header of `scripts/e2e-nav.mjs`.

UI conventions: Persian strings use a normal space, never a ZWNJ (U+200C). Page-level enter animations must never animate opacity from 0 (content has to stay visible even if an animation never runs).
