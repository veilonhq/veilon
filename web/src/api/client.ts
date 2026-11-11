import { API_BASE, IS_MOCK } from '@/app/base';
import { useSession } from '@/store/session';

export class ApiError extends Error {
  status: number;
  code: string;
  /** the whole error object of the response (e.g. field, server, port) */
  details?: Record<string, unknown>;
  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

export interface RequestInit2 {
  method?: Method;
  body?: unknown;
  query?: Record<string, string | number | undefined | null>;
  signal?: AbortSignal;
}

/** A transport takes a fully-built request and returns a Response. */
type Transport = (url: string, init: RequestInit) => Promise<Response>;
let transport: Transport = (url, init) => fetch(url, init);

export function setTransport(t: Transport) {
  transport = t;
}

export async function api<T>(path: string, opts: RequestInit2 = {}): Promise<T> {
  const qs = new URLSearchParams();
  if (opts.query) {
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined && v !== null && v !== '') qs.set(k, String(v));
    }
  }
  const url = API_BASE + path + (qs.toString() ? `?${qs}` : '');
  const headers: Record<string, string> = { Accept: 'application/json' };
  const token = useSession.getState().token;
  if (token) headers.Authorization = `Bearer ${token}`;
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await transport(url, {
      method: opts.method ?? 'GET',
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    });
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'network', (e as Error).message || 'Network error');
  }

  if (res.status === 401 && !path.startsWith('/auth/login')) {
    useSession.getState().logout();
  }
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string } & Record<string, unknown> } | null)?.error;
    throw new ApiError(res.status, err?.code ?? `http_${res.status}`, err?.message ?? res.statusText, err ?? undefined);
  }
  return data as T;
}

export const isMock = IS_MOCK;
