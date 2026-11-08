import { create } from 'zustand';
import type { Admin } from '@/api/types';

interface SessionState {
  token: string | null;
  expiresAt: string | null;
  admin: Admin | null;
  login: (token: string, expiresAt: string, admin: Admin) => void;
  setAdmin: (a: Admin) => void;
  logout: () => void;
}

const KEY = 'veilon.session';

function load(): Pick<SessionState, 'token' | 'expiresAt' | 'admin'> {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) {
      const s = JSON.parse(raw);
      if (s.expiresAt && new Date(s.expiresAt).getTime() < Date.now()) return { token: null, expiresAt: null, admin: null };
      return s;
    }
  } catch {
    /* ignore */
  }
  return { token: null, expiresAt: null, admin: null };
}

export const useSession = create<SessionState>((set, get) => ({
  ...load(),
  login: (token, expiresAt, admin) => {
    localStorage.setItem(KEY, JSON.stringify({ token, expiresAt, admin }));
    set({ token, expiresAt, admin });
  },
  setAdmin: (admin) => {
    const { token, expiresAt } = get();
    localStorage.setItem(KEY, JSON.stringify({ token, expiresAt, admin }));
    set({ admin });
  },
  logout: () => {
    localStorage.removeItem(KEY);
    set({ token: null, expiresAt: null, admin: null });
  },
}));
