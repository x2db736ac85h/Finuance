// Whether the duplicates window is open, and for which accounts (the view's; none = all).

import { create } from 'zustand';

type Target = { accountIds: string[] | undefined; skipped: number };

type DupDialogStore = {
  target: Target | null;
  open: (t: Target) => void;
  close: () => void;
};

export const useDupDialog = create<DupDialogStore>()((set) => ({
  target: null,
  open: (target) => {
    set({ target });
  },
  close: () => {
    set({ target: null });
  },
}));
