// Transaction groups (PLAN §4.7, task 10.12): the list with stats, create / rename / delete, and
// bulk labelling; the group window's report and budget (task 15.9). Every change refetches the
// groups, the grid windows and the open details.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Group = Schemas['Group'];
export type GroupInput = Schemas['GroupInput'];
export type GroupReport = Schemas['GroupReport'];

export function useGroups() {
  return useQuery({ queryKey: keys.groups, queryFn: async () => data(await api.GET('/groups')) });
}

/** GET /groups/{id}/report: the group window (totals, span, per day, categories, top payees). */
export function useGroupReport(id: string) {
  return useQuery({
    queryKey: keys.groupReport(id),
    queryFn: async () => data(await api.GET('/groups/{id}/report', { params: { path: { id } } })),
  });
}

/** After any group change: the list, the rows (group column and filter) and dependent views. */
export function invalidateGroups(qc: ReturnType<typeof useQueryClient>) {
  // a linked group's transactions are copied into its shared group (ADR 0028)
  for (const k of [
    keys.groups,
    ['groupReport'],
    keys.tx,
    ['distinct'],
    ['transaction'],
    keys.insight,
    keys.overview,
    keys.shared,
  ]) {
    void qc.invalidateQueries({ queryKey: k });
  }
}

export function useCreateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: GroupInput) => data(await api.POST('/groups', { body })),
    onSuccess: () => {
      invalidateGroups(qc);
    },
  });
}

export function useUpdateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: GroupInput & { id: string }) =>
      data(await api.PATCH('/groups/{id}', { params: { path: { id } }, body })),
    onSuccess: () => {
      invalidateGroups(qc);
    },
  });
}

export function useDeleteGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE('/groups/{id}', { params: { path: { id } } });
    },
    onSuccess: () => {
      invalidateGroups(qc);
    },
  });
}

/** POST /transactions/bulk: put the transactions in a group, or take them out (null). */
export function useAssignGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ ids, groupId }: { ids: string[]; groupId: string | null }) =>
      data(await api.POST('/transactions/bulk', { body: { ids, set: { groupId } } })),
    onSuccess: () => {
      invalidateGroups(qc);
    },
  });
}
