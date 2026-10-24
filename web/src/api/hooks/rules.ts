// Category management and rules (PLAN tasks 15.1, 15.2). Rule and category changes are applied by
// the server's reclassification job, a moment after the request answers: the views that show
// categories and transfers refetch right away and again once the job has had time to run.

import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type CategoryStat = Schemas['CategoryStat'];
export type Rule = Schemas['Rule'];
export type RuleInput = Schemas['RuleInput'];
export type RuleMatch = Schemas['RuleMatch'];
export type RulePreview = Schemas['RulePreview'];

/** How long after a change the reclassification has usually finished (refetch again then). */
export const RECLASSIFY_SETTLE_MS = [1_200, 4_000];

const RESULT_KEYS = [
  keys.tx,
  ['transaction'],
  ['distinct'],
  keys.categories,
  keys.categoryStats,
  keys.insight,
  keys.overview,
  ['groupReport'],
];

/** Refetch everything that shows categories or transfers, now and after the background job. */
export function invalidateClassified(qc: QueryClient): void {
  const run = () => {
    for (const k of RESULT_KEYS) void qc.invalidateQueries({ queryKey: k });
  };
  run();
  for (const ms of RECLASSIFY_SETTLE_MS) setTimeout(run, ms);
}

export function useCategoryStats() {
  return useQuery({ queryKey: keys.categoryStats, queryFn: async () => data(await api.GET('/categories/stats')) });
}

export function useCreateCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => data(await api.POST('/categories', { body: { name } })),
    onSuccess: () => {
      invalidateClassified(qc);
    },
  });
}

export function useRenameCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) =>
      data(await api.PATCH('/categories/{id}', { params: { path: { id } }, body: { name } })),
    onSuccess: () => {
      invalidateClassified(qc);
      void qc.invalidateQueries({ queryKey: keys.rules }); // rules show their category's name
    },
  });
}

export function useMergeCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, intoId }: { id: string; intoId: string }) =>
      data(await api.POST('/categories/{id}/merge', { params: { path: { id } }, body: { intoId } })),
    onSuccess: () => {
      invalidateClassified(qc);
      void qc.invalidateQueries({ queryKey: keys.rules });
    },
  });
}

export function useDeleteCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE('/categories/{id}', { params: { path: { id } } });
    },
    onSuccess: () => {
      invalidateClassified(qc);
      void qc.invalidateQueries({ queryKey: keys.rules });
    },
  });
}

export function useRules() {
  return useQuery({ queryKey: keys.rules, queryFn: async () => data(await api.GET('/rules')) });
}

function useRuleMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.rules });
      invalidateClassified(qc);
    },
  });
}

export const useCreateRule = () => useRuleMutation(async (body: RuleInput) => data(await api.POST('/rules', { body })));

export const useUpdateRule = () =>
  useRuleMutation(async ({ id, ...body }: RuleInput & { id: string }) =>
    data(await api.PUT('/rules/{id}', { params: { path: { id } }, body })),
  );

export const useDeleteRule = () =>
  useRuleMutation(async (id: string) => {
    await api.DELETE('/rules/{id}', { params: { path: { id } } });
  });

export const useReorderRules = () =>
  useRuleMutation(async (ids: string[]) => data(await api.PUT('/rules/order', { body: { ids } })));

export const useApplyRules = () =>
  useRuleMutation(async () => {
    await api.POST('/rules/apply');
  });

/** A match has something to match (the server refuses a rule that would match everything). */
export function matchesSomething(m: RuleMatch): boolean {
  return !!m.search?.trim() || (m.filters ?? []).some((f) => !!f.cond?.trim() || !!f.excluded?.length);
}

/** POST /rules/preview, for the rule editor: count, manual rows and the newest rows. */
export function useRulePreview(match: RuleMatch) {
  const matchKey = JSON.stringify(match);
  return useQuery({
    queryKey: keys.rulePreview(matchKey),
    queryFn: async () => data(await api.POST('/rules/preview', { body: { match } })),
    enabled: matchesSomething(match),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  });
}

export type BulkAction = { category: { id: string } | { reset: true } } | { transfer: 'yes' | 'no' | 'auto' };

/** POST /transactions/bulk with a category or transfer action (the quick bar, PLAN task 15.5). */
export function useBulkClassify() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ ids, ...action }: BulkAction & { ids: string[] }) =>
      data(await api.POST('/transactions/bulk', { body: { ids, ...action } })),
    onSuccess: () => {
      invalidateClassified(qc);
    },
  });
}
