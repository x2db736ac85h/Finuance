// Toast queue (PLAN §7.6, legacy toast(msg, ms = 2200)): one toast at a time, the rest wait.

import { create } from 'zustand';

export type ToastKind = 'ok' | 'error';
export type ToastItem = { id: number; msg: string; kind: ToastKind; ms: number };

type ToastStore = {
  queue: ToastItem[];
  push: (msg: string, opts?: { kind?: ToastKind; ms?: number }) => void;
  /** The shown toast is done; the next one (if any) comes up. */
  shift: () => void;
};

let next = 1;

export const useToasts = create<ToastStore>()((set) => ({
  queue: [],
  push: (msg, opts) => {
    // the same message twice in a row is shown once
    set((s) =>
      s.queue.at(-1)?.msg === msg
        ? s
        : { queue: [...s.queue, { id: next++, msg, kind: opts?.kind ?? 'ok', ms: opts?.ms ?? 2200 }] },
    );
  },
  shift: () => {
    set((s) => ({ queue: s.queue.slice(1) }));
  },
}));

/** Shows a short message at the bottom of the window. Errors stay longer. */
export function toast(msg: string, opts?: { kind?: ToastKind; ms?: number }): void {
  useToasts.getState().push(msg, opts?.kind === 'error' && opts.ms === undefined ? { ...opts, ms: 5000 } : opts);
}
