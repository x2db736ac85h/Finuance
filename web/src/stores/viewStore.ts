// View state (PLAN §7.4): one entry per viewId ('all' or an account id), mirroring the legacy
// ViewState. Only {hidden, widths, statColumn, sort, period, custom} is persisted; search, filters
// and the selection are session-only, as in the legacy app.

import { create } from 'zustand';

import type { Column, ParsedPrefs, PersistedView } from './prefs';

export type FilterState = { cond: string; excluded: string[] };

export type ViewState = PersistedView & {
  filters: Partial<Record<Column, FilterState>>;
  search: string;
  selectedId: string | null;
  /** The selected row's position in the result, when known (status bar, inspector). */
  selectedIndex: number | null;
  ovMonth: string | null;
};

/**
 * Columns hidden in a view nobody has customised yet (PLAN §7.7). The Account column shows only in
 * the "all" view.
 */
export const DEFAULT_HIDDEN: readonly Column[] = [
  'valueOn',
  'balanceAfter',
  'currency',
  'bankCategory',
  'txType',
  'reference',
  'counterpartyAccount',
  'note',
];

export const newView = (id = 'all'): ViewState => ({
  sort: null,
  filters: {},
  search: '',
  hidden: id === 'all' ? [...DEFAULT_HIDDEN] : [...DEFAULT_HIDDEN, 'account'],
  widths: {},
  selectedId: null,
  selectedIndex: null,
  statColumn: 'amount',
  period: 'all',
  custom: null,
  ovMonth: null,
});

type ViewStore = {
  views: Record<string, ViewState>;
  /** Merges a change into a view, creating it with defaults first. */
  patch: (id: string, change: Partial<ViewState>) => void;
  hydrate: (p: ParsedPrefs) => void;
  reset: () => void;
};

export const useViews = create<ViewStore>()((set) => ({
  views: {},
  patch: (id, change) => {
    set((s) => ({ views: { ...s.views, [id]: { ...(s.views[id] ?? newView(id)), ...change } } }));
  },
  // Stored views replace the persisted fields; session-only fields of open views survive.
  hydrate: (p) => {
    if (!p.views) return;
    const stored = p.views;
    set((s) => {
      const views = { ...s.views };
      for (const [id, pv] of Object.entries(stored)) views[id] = { ...(views[id] ?? newView(id)), ...pv };
      return { views };
    });
  },
  reset: () => {
    set({ views: {} });
  },
}));

/** A view's state, or the defaults for a view not opened yet (does not create it). */
export function viewOf(views: Record<string, ViewState>, id: string): ViewState {
  return views[id] ?? (id === 'all' ? DEFAULT_ALL : DEFAULT_ACCOUNT);
}
const DEFAULT_ALL = newView('all');
const DEFAULT_ACCOUNT = newView('account');

/** The persisted view slices, in preferences-document form. */
export function collectViews(views: Record<string, ViewState>): Record<string, PersistedView> {
  const out: Record<string, PersistedView> = {};
  for (const [id, { hidden, widths, statColumn, sort, period, custom }] of Object.entries(views)) {
    out[id] = { hidden, widths, statColumn, sort, period, custom };
  }
  return out;
}
