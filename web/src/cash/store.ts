// Which of these dialogs is open (ADR 0029): a new cash account, or adding/editing a transaction
// typed by hand (into any account since Phase 27). The toolbar, the inspector, the sidebar and the `N` key open them; the shell renders them.

import { create } from 'zustand';

/**
 * `pick`: opened where no one account is in view (All accounts), so the form asks which account;
 * `accountId` is then only the one it starts on.
 */
export type TransactionDialog = { accountId: string; currency: string; editId?: string; pick?: boolean };

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
