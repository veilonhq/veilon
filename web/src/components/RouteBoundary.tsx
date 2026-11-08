import { RotateCw } from 'lucide-react';
import { Component, createElement, type ComponentType, type ReactNode } from 'react';
import { translate } from '@/i18n';
import { useUi } from '@/store/ui';

const RELOAD_KEY = 'veilon.chunkReload';

/** A failed dynamic import: the chunk is missing (panel upgraded) or the network dropped. */
export function isChunkError(e: unknown) {
  const m = e instanceof Error ? `${e.name} ${e.message}` : String(e);
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|ChunkLoadError|Loading chunk|Unable to preload CSS/i.test(m);
}

/**
 * Lazy route component with one retry. Unlike React.lazy, a chunk that has
 * already been loaded (the shell warms them after sign-in) renders
 * synchronously, so a dock tap swaps pages in the same frame instead of
 * flashing a skeleton for one tick. When the chunk still cannot be loaded
 * (most often because the binary was upgraded while this tab kept the old
 * index.html), reload the page once so the new asset names are picked up.
 */
export function lazyPage<T extends ComponentType<object>>(factory: () => Promise<{ default: T }>) {
  let mod: T | undefined;
  let pending: Promise<void> | undefined;
  let failure: unknown;
  const attempt = async () => {
    try {
      return await factory();
    } catch (e) {
      await new Promise((r) => setTimeout(r, 400));
      try {
        return await factory();
      } catch (e2) {
        if (isChunkError(e2) && !sessionStorage.getItem(RELOAD_KEY)) {
          sessionStorage.setItem(RELOAD_KEY, '1');
          window.location.reload();
          return await new Promise<never>(() => undefined);
        }
        throw e2 ?? e;
      }
    }
  };
  const load = () =>
    (pending ??= attempt().then(
      (m) => {
        mod = m.default;
        sessionStorage.removeItem(RELOAD_KEY);
      },
      (e) => {
        failure = e;
        pending = undefined;
      },
    ));
  function LazyPage(props: object) {
    if (mod) return createElement(mod, props);
    if (failure) {
      const e = failure;
      failure = undefined;
      throw e;
    }
    // Suspense protocol: the nearest boundary (Shell) shows its fallback, or —
    // inside a navigation transition — keeps the previous page on screen.
    throw load();
  }
  LazyPage.preload = () => load();
  return LazyPage as ((props: object) => JSX.Element) & { preload: () => Promise<void> };
}

interface State {
  error: unknown;
}

/** Keeps a crashing page from blanking the whole app: shows a reload card inside the shell instead. */
export class RouteBoundary extends Component<{ children: ReactNode; resetKey?: string }, State> {
  state: State = { error: null };
  static getDerivedStateFromError(error: unknown): State {
    return { error };
  }
  componentDidCatch(error: unknown) {
    console.error('[veilon] page crashed', error);
  }
  componentDidUpdate(prev: { resetKey?: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null });
  }
  render() {
    if (!this.state.error) return this.props.children;
    const lang = useUi.getState().lang;
    return (
      <div className="glass" role="alert" style={{ padding: 24, marginTop: 20, display: 'grid', gap: 12, justifyItems: 'start', borderRadius: 24 }}>
        <strong style={{ fontSize: 18 }}>{translate(lang, 'common.pageError')}</strong>
        <span style={{ color: 'var(--muted)' }}>{translate(lang, 'common.pageErrorBody')}</span>
        <code style={{ fontSize: 12, color: 'var(--muted)', direction: 'ltr', overflowWrap: 'anywhere' }}>{String((this.state.error as Error)?.message ?? this.state.error)}</code>
        <button className="btn btn--primary" onClick={() => window.location.reload()}>
          <RotateCw size={16} />
          {translate(lang, 'common.reload')}
        </button>
      </div>
    );
  }
}
