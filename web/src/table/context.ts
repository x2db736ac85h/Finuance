// The QueryContext of a view (PLAN task 10.1): accounts, search, column filters and period from
// the view store, plus a stable key for the query cache. Search is debounced where it is typed
// (120 ms, SearchBox) and conditions in the column menu (160 ms), so this is a pure derivation.

import { useMemo } from 'react';

import type { Schemas } from '../api/client';
import { addDays } from '../lib/dates';
import type { PeriodPref } from '../stores/prefs';
import { useViews, viewOf, type ViewState } from '../stores/viewStore';

export type QueryContext = Schemas['QueryContext'];
export type Sort = Schemas['Sort'];

/** The API period for a view's period preference ('all' → none). */
export function periodOf(p: PeriodPref, custom: ViewState['custom']): Schemas['Period'] | undefined {
  if (p === 'l30' || p === 'l3m' || p === 'l12m' || p === 'ytd') return { preset: p };
  if (p.startsWith('m:')) return { month: p.slice(2) };
  if (p.startsWith('y:')) return { year: Number(p.slice(2)) };
  // custom ranges are inclusive in the UI, the API's `to` is exclusive
  if (p === 'custom' && custom) return { from: custom.from, to: addDays(custom.to, 1) };
  return undefined;
}

export function contextOf(
  viewId: string,
  v: Pick<ViewState, 'search' | 'filters' | 'period' | 'custom'> &
    Partial<Pick<ViewState, 'toolFilter' | 'hideTransfers'>>,
): QueryContext {
  const filters: NonNullable<QueryContext['filters']> = [];
  for (const [column, f] of Object.entries(v.filters)) {
    if (!f || (!f.cond.trim() && !f.excluded.length)) continue;
    filters.push({
      column: column as Schemas['FilterColumn'],
      ...(f.cond.trim() ? { cond: f.cond } : {}),
      ...(f.excluded.length ? { excluded: f.excluded } : {}),
    });
  }
  filters.sort((a, b) => a.column.localeCompare(b.column));
  const period = periodOf(v.period, v.custom);
  return {
    ...(viewId === 'all' ? {} : { accountIds: [viewId] }),
    ...(v.search.trim() ? { search: v.search.trim() } : {}),
    ...(filters.length ? { filters } : {}),
    ...(period ? { period } : {}),
    ...(v.toolFilter ? { toolFilter: v.toolFilter } : {}),
    // the list shows transfers unless hidden; charts, the overview and the assistant leave them
    // out (their requests set includeTransfers: false, ADR 0018)
    includeTransfers: !v.hideTransfers,
  };
}

/** The context for charts, the overview and the assistant: transfers left out. */
export const withoutTransfers = (c: QueryContext): QueryContext => ({ ...c, includeTransfers: false });

export type ViewQuery = { context: QueryContext; ctxKey: string; sort: Sort | undefined; sortKey: string };

export function useQueryContext(viewId: string): ViewQuery {
  const search = useViews((s) => viewOf(s.views, viewId).search);
  const filters = useViews((s) => viewOf(s.views, viewId).filters);
  const period = useViews((s) => viewOf(s.views, viewId).period);
  const custom = useViews((s) => viewOf(s.views, viewId).custom);
  const sortPref = useViews((s) => viewOf(s.views, viewId).sort);
  const toolFilter = useViews((s) => viewOf(s.views, viewId).toolFilter);
  const hideTransfers = useViews((s) => viewOf(s.views, viewId).hideTransfers);
  return useMemo(() => {
    const context = contextOf(viewId, { search, filters, period, custom, toolFilter, hideTransfers });
    const sort = sortPref ? { column: sortPref.column as Schemas['FilterColumn'], dir: sortPref.dir } : undefined;
    return { context, ctxKey: JSON.stringify(context), sort, sortKey: JSON.stringify(sort ?? null) };
  }, [viewId, search, filters, period, custom, sortPref, toolFilter, hideTransfers]);
}
