// Grid data (PLAN §7.7): rows come in windows of 200. The first window carries the totals (total,
// stats, period, data range, duplicates skipped); the others ask for rows only (includeTotals:
// false). Only the windows around the viewport are fetched.

import { keepPreviousData, useQueries, useQuery } from '@tanstack/react-query';

import { api, data } from '../api/client';
import type { Schemas } from '../api/client';
import { keys } from '../api/keys';
import type { Row } from './columns';
import type { ViewQuery } from './context';

export const WINDOW = 200;

export type Page = Schemas['TransactionPage'];
type StatColumn = 'amount' | 'balanceAfter';

async function fetchWindow(q: ViewQuery, index: number, statColumn: StatColumn, includeTotals: boolean): Promise<Page> {
  return data(
    await api.POST('/transactions/query', {
      body: {
        context: q.context,
        ...(q.sort ? { sort: q.sort } : {}),
        offset: index * WINDOW,
        limit: WINDOW,
        statColumn,
        includeTotals,
      },
    }),
  );
}

/** The head window: rows 0–199 and the totals of the view. */
export function useHead(q: ViewQuery, statColumn: StatColumn) {
  return useQuery({
    queryKey: [...keys.tx, q.ctxKey, q.sortKey, statColumn, 'head'],
    queryFn: () => fetchWindow(q, 0, statColumn, true),
    placeholderData: keepPreviousData,
  });
}

/** Window indexes to load for the visible rows [start, end] (±1 window). */
export function windowsFor(start: number, end: number, total: number): number[] {
  if (total <= 0 || end < start) return [];
  const last = Math.ceil(total / WINDOW) - 1;
  const from = Math.max(0, Math.floor(start / WINDOW) - 1);
  const to = Math.min(last, Math.floor(end / WINDOW) + 1);
  const out: number[] = [];
  for (let w = from; w <= to; w++) out.push(w);
  return out;
}

/** The rows of the windows around [start, end]; rowAt(i) is undefined while loading. */
export function useRows(q: ViewQuery, head: Page | undefined, start: number, end: number) {
  const total = head?.total ?? 0;
  const indexes = windowsFor(start, end, total).filter((w) => w > 0);
  const results = useQueries({
    queries: indexes.map((w) => ({
      queryKey: [...keys.tx, q.ctxKey, q.sortKey, 'w', w],
      queryFn: () => fetchWindow(q, w, 'amount', false),
      staleTime: 30_000,
    })),
  });
  const windows = new Map<number, Row[]>();
  if (head) windows.set(0, head.rows);
  indexes.forEach((w, k) => {
    const rows = results[k]?.data?.rows;
    if (rows) windows.set(w, rows);
  });
  return {
    total,
    rowAt: (i: number): Row | undefined => windows.get(Math.floor(i / WINDOW))?.[i % WINDOW],
    /** The position of a loaded row, or -1. */
    indexOf: (id: string): number => {
      for (const [w, rows] of windows) {
        const k = rows.findIndex((r) => r.id === id);
        if (k >= 0) return w * WINDOW + k;
      }
      return -1;
    },
  };
}

export type RowSource = ReturnType<typeof useRows>;
