// Toast queue (PLAN §7.6, legacy toast(msg, ms = 2200)): one toast at a time, the rest wait.
// A toast can carry actions (Undo, Review, Open); it then stays longer and takes clicks.

import { create } from 'zustand';

export type ToastKind = 'ok' | 'error';
export type ToastAction = { label: string; run: () => void };
export type ToastItem = { id: number; msg: string; kind: ToastKind; ms: number; actions: ToastAction[] };
export type ToastOptions = { kind?: ToastKind; ms?: number; actions?: ToastAction[] };

type ToastStore = {
  queue: ToastItem[];
  push: (msg: string, opts?: ToastOptions) => void;
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
        : {
            queue: [
              ...s.queue,
              { id: next++, msg, kind: opts?.kind ?? 'ok', ms: opts?.ms ?? 2200, actions: opts?.actions ?? [] },
            ],
          },
    );
  },
  shift: () => {
    set((s) => ({ queue: s.queue.slice(1) }));
  },
}));

/** Shows a short message at the bottom of the window. Errors and toasts with actions stay longer. */
export function toast(msg: string, opts: ToastOptions = {}): void {
  const long = opts.kind === 'error' ? 5000 : opts.actions?.length ? 8000 : undefined;
  useToasts.getState().push(msg, opts.ms === undefined && long ? { ...opts, ms: long } : opts);
}
