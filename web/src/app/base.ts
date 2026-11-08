declare global {
  interface Window {
    __VEILON_BASE__?: string;
  }
}

/**
 * The panel lives under a random first path segment (/<panel_path>/).
 * The router basename is that segment and the API sits at <basename>/api/v1.
 */
function detectBasename(): string {
  if (typeof window.__VEILON_BASE__ === 'string') return window.__VEILON_BASE__;
  const seg = window.location.pathname.split('/').filter(Boolean)[0];
  if (!seg) {
    // Dev convenience: when opened at the bare origin, move under a stable prefix
    // so that reloads on nested routes keep the same basename.
    if (import.meta.env.DEV) {
      window.history.replaceState(null, '', '/panel/' + window.location.search + window.location.hash);
      return '/panel';
    }
    return '';
  }
  return '/' + seg;
}

export const BASENAME = detectBasename();
export const API_BASE = `${BASENAME}/api/v1`;
export const IS_MOCK = import.meta.env.VITE_MOCK === '1';
