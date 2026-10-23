// The overview's shared data and helpers (PLAN §7.9, legacy ovCtx). Every card is a ChartDef whose
// use() reads the same cached POST /overview document; the overview never applies the list's
// period, so its query context leaves the period out (changing the period doesn't refetch).

import { useMemo } from 'react';

import { useOverview, type Overview, type OverviewData } from '../api/hooks/overview';
import { slotColor } from '../charts/lib/colors';
import { money } from '../charts/lib/money';
import { Empty, partsOf, type ChartEnv, type ChartParts } from '../charts/types';
import { contextOf, type ViewQuery } from '../table/context';
import { useViews, viewOf } from '../stores/viewStore';

export type { OverviewData };

/** The view's query context without its period (the overview reads every month of the rows). */
export function useOverviewQuery(viewId: string): ViewQuery {
  const search = useViews((s) => viewOf(s.views, viewId).search);
  const filters = useViews((s) => viewOf(s.views, viewId).filters);
  return useMemo(() => {
    const context = contextOf(viewId, { search, filters, period: 'all', custom: null });
    return { context, ctxKey: JSON.stringify(context), sort: undefined, sortKey: 'null' };
  }, [viewId, search, filters]);
}

/** The overview document of the view's month and months setting (one request for every card). */
export function useOverviewData(q: ViewQuery, env: Pick<ChartEnv, 'ovMonth' | 'cfg' | 'ready'>) {
  return useOverview(q.context, q.ctxKey, env.ovMonth, env.cfg.ovMonths, env.ready);
}

const NO_DATA: ChartParts = {
  loading: false,
  error: false,
  sub: '',
  body: <Empty>Not enough data for this chart</Empty>,
  table: null,
};

/** A card's parts from the overview document (legacy: "Not enough data" without dated rows). */
export function useOverviewParts(
  q: ViewQuery,
  env: ChartEnv,
  render: (d: OverviewData, currency: string, env: ChartEnv) => ChartParts,
): ChartParts {
  return partsOf(useOverviewData(q, env), (o: Overview) => (o.data ? render(o.data, o.currency, env) : NO_DATA));
}

/** Category colours by name (legacy catColorMap): the 7 largest all-time spenders own a slot. */
export function colorsOf(d: OverviewData): (name: string) => string {
  const m = new Map<string, number>();
  for (const s of d.colorSlots) m.set(s.category?.name ?? UNCATEGORIZED, s.slot);
  return (name) => slotColor(m.get(name));
}

/** The legacy name of spending without a category. */
export const UNCATEGORIZED = '(uncategorized)';

/** The filter condition for a category name: `=` alone matches the uncategorised rows (11.4). */
export const categoryCond = (name: string) => (name === UNCATEGORIZED ? '=' : `=${name}`);

/** Savings rate of a month in percent, null without income. */
export const rateOf = (m: { incomeMinor: number; expenseMinor: number }) =>
  m.incomeMinor > 0 ? ((m.incomeMinor - m.expenseMinor) / m.incomeMinor) * 100 : null;

/** JS toFixed of a percentage with the sign kept for positives (legacy pctS on ratios). */
export const pctS = (x: number | undefined) =>
  x === undefined || !Number.isFinite(x) ? '–' : `${x >= 0 ? '+' : ''}${(x * 100).toFixed(1)}%`;

/** The "Range" setting of the cash-flow, savings-rate and balance cards (legacy ovMonths). */
export const monthsControl = {
  key: 'ovMonths',
  label: 'Range',
  options: [
    [6, 'Last 6 months'],
    [12, 'Last 12 months'],
    [24, 'Last 24 months'],
    [36, 'Last 36 months'],
  ],
} as const;

/** The legacy money(-x) of a positive amount: always signed, "-0.00 PLN" for zero. */
export const negMoney = (x: number, cur: string) => `-${money(x, cur)}`;
