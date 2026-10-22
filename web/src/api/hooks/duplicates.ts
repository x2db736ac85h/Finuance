// The duplicates window (PLAN §7.14, task 10.13): groups 60 at a time with a text filter, and the
// two decisions (use a version / keep both) plus going back to the default merge. A decision
// rebuilds the transactions on the server before it answers, so the table, the status bar and the
// insights refetch right after.

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type DuplicateGroup = Schemas['DuplicateGroup'];
export type DuplicateDecision = Schemas['DuplicateDecision'];
export type DuplicateFields = Schemas['DuplicateFields'];

export const DUP_PAGE = 60;

const listDuplicates = async (accountIds: readonly string[] | undefined, q: string, offset: number, limit: number) =>
  data(
    await api.GET('/duplicates', {
      params: {
        query: { ...(accountIds?.length ? { accountIds: [...accountIds] } : {}), ...(q ? { q } : {}), offset, limit },
      },
    }),
  );

export function useDuplicates(accountIds: readonly string[] | undefined, q: string, enabled = true) {
  return useInfiniteQuery({
    queryKey: [...keys.duplicates, accountIds ?? null, q],
    queryFn: ({ pageParam }) => listDuplicates(accountIds, q, pageParam, DUP_PAGE),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.groups.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    enabled,
  });
}

/** Whether the accounts have any duplicate group (the status bar's "duplicates reviewed"). */
export function useHasDuplicates(accountIds: readonly string[] | undefined, enabled: boolean) {
  return useQuery({
    queryKey: [...keys.duplicates, accountIds ?? null, 'any'],
    queryFn: async () => (await listDuplicates(accountIds, '', 0, 1)).total > 0,
    enabled,
  });
}

function invalidateAfterDecision(qc: ReturnType<typeof useQueryClient>) {
  for (const k of [
    keys.duplicates,
    keys.tx,
    ['transaction'],
    ['distinct'],
    keys.categories,
    keys.groups,
    keys.accounts,
    keys.insight,
    keys.overview,
  ]) {
    void qc.invalidateQueries({ queryKey: k });
  }
}

type Target = { accountId: string; fingerprint: string };

export function useDecideDuplicate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ accountId, fingerprint, ...body }: Target & DuplicateDecision) =>
      data(
        await api.PUT('/duplicates/{accountId}/{fingerprint}', { params: { path: { accountId, fingerprint } }, body }),
      ),
    onSuccess: () => {
      invalidateAfterDecision(qc);
    },
  });
}

export function useResetDuplicate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ accountId, fingerprint }: Target) =>
      data(await api.DELETE('/duplicates/{accountId}/{fingerprint}', { params: { path: { accountId, fingerprint } } })),
    onSuccess: () => {
      invalidateAfterDecision(qc);
    },
  });
}
