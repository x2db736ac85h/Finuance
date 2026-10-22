// Transactions beyond the grid windows (table/data.ts): value checklists, export.

import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type DistinctValue = Schemas['DistinctValue'];
export type QueryContext = Schemas['QueryContext'];

/** The value checklist of a column: values with counts, most frequent first, at most 600. */
export function useDistinct(context: QueryContext, ctxKey: string, column: Schemas['FilterColumn'], search: string) {
  return useQuery({
    queryKey: keys.distinct(ctxKey, column, search),
    queryFn: async () =>
      data(
        await api.POST('/transactions/distinct', {
          body: { context, column, ...(search ? { search } : {}), limit: 600 },
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

export type ExportRequest = Schemas['ExportRequest'];

/**
 * POST /transactions/export: the CSV as a Blob with the server's file name (Content-Disposition).
 * The response is a stream, read here with fetch's blob(), so it doesn't go through openapi-fetch's
 * JSON parsing.
 */
export async function exportCsv(body: ExportRequest): Promise<{ blob: Blob; filename: string }> {
  const r = await api.POST('/transactions/export', { body, parseAs: 'blob' });
  const blob = data(r);
  const cd = r.response.headers.get('Content-Disposition') ?? '';
  const m = /filename="?([^";]+)"?/i.exec(cd);
  return { blob, filename: m?.[1] ?? 'transactions.csv' };
}
