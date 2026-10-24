// The insight charts' data (PLAN §6.2, POST /insights/*). Keys start with keys.insight, so edits
// that change numbers (transaction patch, imports, groups) invalidate every chart at once.

import { keepPreviousData, useQuery } from '@tanstack/react-query';

import type { ChartCfg } from '../../stores/prefs';
import { api, data, type Schemas } from '../client';
import { withoutTransfers } from '../../table/context';
import { keys } from '../keys';

export type QueryContext = Schemas['QueryContext'];
export type InsightSummary = Schemas['InsightSummary'];
export type InsightCategories = Schemas['InsightCategories'];
export type InsightCashflow = Schemas['InsightCashflow'];
export type InsightPace = Schemas['InsightPace'];
export type InsightPayees = Schemas['InsightPayees'];
export type InsightDaily = Schemas['InsightDaily'];

const opts = { placeholderData: keepPreviousData, staleTime: 30_000 } as const;

// `enabled` is false until the list's first window has arrived (the pane waits, so the table
// paints first: browsers allow six parallel requests per host over HTTP/1.1).

export function useSummary(context: QueryContext, ctxKey: string, enabled = true) {
  return useQuery({
    queryKey: [...keys.insight, 'summary', ctxKey],
    queryFn: async () => data(await api.POST('/insights/summary', { body: { context: withoutTransfers(context) } })),
    ...opts,
    enabled,
  });
}

export function useCategorySpend(context: QueryContext, ctxKey: string, enabled = true) {
  return useQuery({
    queryKey: [...keys.insight, 'categories', ctxKey],
    queryFn: async () => data(await api.POST('/insights/categories', { body: { context: withoutTransfers(context) } })),
    ...opts,
    enabled,
  });
}

export function useCashflow(context: QueryContext, ctxKey: string, enabled = true) {
  return useQuery({
    queryKey: [...keys.insight, 'cashflow', ctxKey],
    queryFn: async () => data(await api.POST('/insights/cashflow', { body: { context: withoutTransfers(context) } })),
    ...opts,
    enabled,
  });
}

export function usePace(context: QueryContext, ctxKey: string, compare: ChartCfg['paceCmp'], enabled = true) {
  return useQuery({
    queryKey: [...keys.insight, 'pace', ctxKey, compare],
    queryFn: async () =>
      data(await api.POST('/insights/pace', { body: { context: withoutTransfers(context), compare } })),
    ...opts,
    enabled,
  });
}

/** The API's top values; anything else stored in the preferences reads as the default 25. */
const TOPS = [0, 10, 25, 50, 100] as const;
type Top = (typeof TOPS)[number];
export const payeeTop = (n: number): Top => TOPS.find((t) => t === n) ?? 25;

export function usePayees(
  context: QueryContext,
  ctxKey: string,
  top: number,
  scope: ChartCfg['payeeScope'],
  enabled = true,
) {
  const t = payeeTop(top);
  const s = scope === 'all' ? 'all' : 'period';
  return useQuery({
    queryKey: [...keys.insight, 'payees', ctxKey, t, s],
    queryFn: async () =>
      data(await api.POST('/insights/payees', { body: { context: withoutTransfers(context), top: t, scope: s } })),
    ...opts,
    enabled,
  });
}

/** The calendar range of a heatRange setting (period, all, or 3/6/12 months). */
export function dailyRange(r: ChartCfg['heatRange']): Schemas['DailyRequest']['range'] {
  if (r === 3 || r === 6 || r === 12) return `${r}m`;
  return r === 'all' ? 'all' : 'period';
}

export function useDaily(context: QueryContext, ctxKey: string, heatRange: ChartCfg['heatRange'], enabled = true) {
  const range = dailyRange(heatRange);
  return useQuery({
    queryKey: [...keys.insight, 'daily', ctxKey, range],
    queryFn: async () =>
      data(await api.POST('/insights/daily', { body: { context: withoutTransfers(context), range } })),
    ...opts,
    enabled,
  });
}
