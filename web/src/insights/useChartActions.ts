// Chart → table actions (PLAN task 11.7, legacy toggleFilter / setPeriod): category and payee
// clicks set a column condition, and the same click again clears it; month and day clicks set the
// period; the largest expense is located in the list. Every action goes through the view store, so
// the table, the status bar and every chart refresh together.

import { useMemo } from 'react';

import { api, data } from '../api/client';
import type { ChartActions, FilterColumn } from '../charts/types';
import type { CustomRange, PeriodPref } from '../stores/prefs';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf, type FilterState } from '../stores/viewStore';
import { contextOf } from '../table/context';
import { scrollToRow } from '../table/controller';
import { WINDOW } from '../table/data';
import { hideTip } from '../ui/Tooltip';

/**
 * The filters after a chart click (legacy toggleFilter): an identical condition is cleared (the
 * column filter goes away unless values are unticked); anything else replaces the condition and
 * keeps the unticked values.
 */
export function toggled(
  filters: Partial<Record<string, FilterState>>,
  column: string,
  cond: string,
): Partial<Record<string, FilterState>> {
  const f = filters[column];
  const out = Object.fromEntries(Object.entries(filters).filter(([k]) => k !== column));
  if (!f || f.cond !== cond) out[column] = { cond, excluded: f ? f.excluded : [] };
  else if (f.excluded.length) out[column] = { ...f, cond: '' };
  return out;
}

export function useChartActions(viewId: string): ChartActions {
  return useMemo(() => {
    const view = () => viewOf(useViews.getState().views, viewId);
    const patch = useViews.getState().patch;
    return {
      toggleFilter: (column: FilterColumn, cond: string) => {
        hideTip();
        patch(viewId, { filters: toggled(view().filters, column, cond) });
      },
      setPeriod: (period: PeriodPref, custom?: CustomRange) => {
        hideTip();
        patch(viewId, custom ? { period, custom } : { period });
      },
      locate: (id: string) => {
        hideTip();
        const v = view();
        const sort = v.sort ? { column: v.sort.column as FilterColumn, dir: v.sort.dir } : undefined;
        void api
          .POST('/transactions/query', {
            body: {
              context: contextOf(viewId, v),
              ...(sort ? { sort } : {}),
              offset: 0,
              limit: WINDOW,
              locateId: id,
              includeTotals: false,
            },
          })
          .then((r) => {
            const n = data(r).locatedOffset;
            if (n === undefined) return;
            patch(viewId, { selectedId: id, selectedIndex: n });
            scrollToRow(n);
          });
      },
      setCatMode: (m) => {
        useUi.getState().setCatMode(m);
      },
    };
  }, [viewId]);
}
