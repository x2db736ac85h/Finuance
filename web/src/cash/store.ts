// Which cash dialog is open (ADR 0029): a new cash account, or adding/editing a transaction typed
// by hand. The toolbar, the inspector, the sidebar and the `N` key open them; the shell renders them.

import { create } from 'zustand';

export type TransactionDialog = { accountId: string; currency: string; editId?: string };

type CashDialogs = {
  newAccount: boolean;
  transaction: TransactionDialog | null;
  openNewAccount: () => void;
  openTransaction: (d: TransactionDialog) => void;
  close: () => void;
};

export const useCashDialogs = create<CashDialogs>()((set) => ({
  newAccount: false,
  transaction: null,
  openNewAccount: () => {
    set({ newAccount: true, transaction: null });
  },
  openTransaction: (d) => {
    set({ transaction: d, newAccount: false });
  },
  close: () => {
    set({ newAccount: false, transaction: null });
  },
}));
