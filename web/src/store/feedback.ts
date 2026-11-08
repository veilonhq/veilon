import { create } from 'zustand';

export type ToastKind = 'success' | 'error' | 'info';
export interface Toast {
  id: number;
  kind: ToastKind;
  title: string;
  body?: string;
}

interface ToastState {
  toasts: Toast[];
  push: (t: Omit<Toast, 'id'>) => void;
  dismiss: (id: number) => void;
}

let seq = 1;
export const useToasts = create<ToastState>((set, get) => ({
  toasts: [],
  push: (t) => {
    const id = seq++;
    set({ toasts: [...get().toasts.slice(-3), { ...t, id }] });
    setTimeout(() => get().dismiss(id), t.kind === 'error' ? 6000 : 3600);
  },
  dismiss: (id) => set({ toasts: get().toasts.filter((x) => x.id !== id) }),
}));

export const toast = {
  success: (title: string, body?: string) => useToasts.getState().push({ kind: 'success', title, body }),
  error: (title: string, body?: string) => useToasts.getState().push({ kind: 'error', title, body }),
  info: (title: string, body?: string) => useToasts.getState().push({ kind: 'info', title, body }),
};

export interface ConfirmRequest {
  title: string;
  body?: string;
  confirmLabel?: string;
  danger?: boolean;
  /** When set, the user must type this word to enable the confirm button. */
  typeToConfirm?: string;
  /** Optional numeric input (e.g. days to extend). */
  numberInput?: { label: string; initial: number; min?: number; max?: number };
  /** Optional single choice (e.g. a group); the picked value is returned. */
  choiceInput?: { label: string; initial: number; options: Array<{ value: number; label: string; color?: string }> };
}

interface ConfirmState {
  req: (ConfirmRequest & { resolve: (v: false | { value?: number }) => void }) | null;
  open: (r: ConfirmRequest) => Promise<false | { value?: number }>;
  close: (v: false | { value?: number }) => void;
}

export const useConfirm = create<ConfirmState>((set, get) => ({
  req: null,
  open: (r) =>
    new Promise((resolve) => {
      set({ req: { ...r, resolve } });
    }),
  close: (v) => {
    get().req?.resolve(v);
    set({ req: null });
  },
}));

export const confirmAction = (r: ConfirmRequest) => useConfirm.getState().open(r);
