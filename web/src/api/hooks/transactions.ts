// Transactions beyond the grid windows (table/data.ts): value checklists, export, the inspector's
// detail and edits, categories.

import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

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

export type TransactionDetail = Schemas['TransactionDetail'];
export type TransactionPatch = Schemas['TransactionPatch'];
export type Category = Schemas['Category'];

/** GET /transactions/{id}: every field, the original row and its sources (the inspector). */
export function useTransaction(id: string | null) {
  return useQuery({
    queryKey: keys.transaction(id ?? ''),
    queryFn: async () => data(await api.GET('/transactions/{id}', { params: { path: { id: id ?? '' } } })),
    enabled: !!id,
  });
}

/**
 * PATCH /transactions/{id}: category (pick, create by name, or back to the bank's) and note. The
 * answer replaces the cached detail; grid windows and categories refetch.
 */
export function usePatchTransaction(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: TransactionPatch) =>
      data(await api.PATCH('/transactions/{id}', { params: { path: { id } }, body })),
    onSuccess: (detail) => {
      qc.setQueryData(keys.transaction(id), detail);
      invalidateRows(qc);
    },
  });
}

/** Everything a changed transaction shows up in: the grid, counts, charts and the overview. */
function invalidateRows(qc: ReturnType<typeof useQueryClient>) {
  for (const k of [keys.tx, keys.accounts, keys.categories, keys.insight, keys.overview, keys.ruleSuggestions])
    void qc.invalidateQueries({ queryKey: k });
}

export type ManualTransactionInput = Schemas['ManualTransactionInput'];

/** POST /transactions: a transaction typed into a cash account (ADR 0029). */
export function useAddTransaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: ManualTransactionInput) => data(await api.POST('/transactions', { body })),
    onSuccess: (detail) => {
      qc.setQueryData(keys.transaction(detail.id), detail);
      invalidateRows(qc);
    },
  });
}

/** PATCH /transactions/{id} without a fixed id (the add/edit dialog edits whichever row it opened). */
export function useEditTransaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: TransactionPatch }) =>
      data(await api.PATCH('/transactions/{id}', { params: { path: { id } }, body })),
    onSuccess: (detail) => {
      qc.setQueryData(keys.transaction(detail.id), detail);
      invalidateRows(qc);
    },
  });
}

/** DELETE /transactions/{id}: a transaction added by hand. */
export function useDeleteTransaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      data(await api.DELETE('/transactions/{id}', { params: { path: { id } } }));
      return id;
    },
    onSuccess: (id) => {
      qc.removeQueries({ queryKey: keys.transaction(id) });
      invalidateRows(qc);
    },
  });
}

export type MergeSuggestion = Schemas['MergeSuggestion'];

/** GET /transactions/merge-suggestions: typed card payments the bank's file has now (ADR 0032). */
export function useMergeSuggestions() {
  return useQuery({
    queryKey: keys.mergeSuggestions,
    queryFn: async () => data(await api.GET('/transactions/merge-suggestions')).suggestions,
    staleTime: 30_000,
  });
}

/** POST /transactions/{id}/merge: merge the typed row into the imported one, or keep them apart. */
export function useMergeTransaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ s, keepApart }: { s: MergeSuggestion; keepApart: boolean }) => {
      data(
        await api.POST('/transactions/{id}/merge', {
          params: { path: { id: s.typedId } },
          body: { into: s.importedId, keepApart },
        }),
      );
    },
    onSuccess: (_, { s, keepApart }) => {
      void qc.invalidateQueries({ queryKey: keys.mergeSuggestions });
      if (keepApart) return;
      qc.removeQueries({ queryKey: keys.transaction(s.typedId) });
      invalidateRows(qc);
      void qc.invalidateQueries({ queryKey: keys.shared });
    },
  });
}

/** GET /categories (all accounts): names for the inspector's category picker. */
export function useCategories() {
  return useQuery({
    queryKey: keys.categories,
    queryFn: async () => data(await api.GET('/categories')),
    staleTime: 60_000,
  });
}

/** POST /transactions/ids: ids, amounts and groups of a range of the ordered result. */
export async function fetchRange(body: Schemas['IdsRequest']) {
  return data(await api.POST('/transactions/ids', { body }));
}
