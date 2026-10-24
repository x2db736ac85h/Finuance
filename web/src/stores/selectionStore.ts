// Multi-selection of the table (PLAN §7.7, legacy view().msel): transaction ids with their amounts
// (for the quick bar's Σ) and whether they have a group (for Ungroup). It belongs to one view and
// is cleared when the result changes, on Esc, on a plain click and when the view closes. `anchor`
// is the position Shift-click ranges start from; `head` is the moving end of a Shift+↑/↓ range.

import { create } from 'zustand';

export type Picked = { amountMinor: number; grouped: boolean };

type SelectionStore = {
  viewId: string | null;
  rows: ReadonlyMap<string, Picked>;
  anchor: number | null;
  head: number | null;
  /** A range is being resolved on the server (POST /transactions/ids). */
  loading: boolean;
  set: (viewId: string, rows: ReadonlyMap<string, Picked>) => void;
  setAnchor: (viewId: string, anchor: number) => void;
  /** A keyboard range: the rows between anchor and head, both kept. */
  setRange: (viewId: string, anchor: number, head: number, rows: ReadonlyMap<string, Picked>) => void;
  setLoading: (loading: boolean) => void;
  clear: (viewId?: string) => void;
};

const EMPTY: ReadonlyMap<string, Picked> = new Map();

export const useSelection = create<SelectionStore>()((set, get) => ({
  viewId: null,
  rows: EMPTY,
  anchor: null,
  head: null,
  loading: false,
  set: (viewId, rows) => {
    set({ viewId, rows: rows.size ? rows : EMPTY, head: null, ...(get().viewId === viewId ? {} : { anchor: null }) });
  },
  setAnchor: (viewId, anchor) => {
    set(get().viewId === viewId ? { anchor, head: null } : { viewId, anchor, head: null, rows: EMPTY });
  },
  setRange: (viewId, anchor, head, rows) => {
    set({ viewId, anchor, head, rows: rows.size ? rows : EMPTY });
  },
  setLoading: (loading) => {
    set({ loading });
  },
  clear: (viewId) => {
    if (viewId !== undefined && get().viewId !== viewId) return;
    if (get().rows.size || get().anchor !== null || get().loading)
      set({ rows: EMPTY, anchor: null, head: null, loading: false });
  },
}));

/** The selected rows of a view (empty for any other view). */
export function selectedIn(s: { viewId: string | null; rows: ReadonlyMap<string, Picked> }, viewId: string) {
  return s.viewId === viewId ? s.rows : EMPTY;
}
