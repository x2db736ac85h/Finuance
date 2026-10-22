// Transaction groups (PLAN §4.7, task 10.12): the list with stats, create / rename / delete, and
// bulk labelling. Every change refetches the groups, the grid windows and the open details.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Group = Schemas['Group'];
export type GroupInput = Schemas['GroupInput'];

export function useGroups() {
  return useQuery({ queryKey: keys.groups, queryFn: async () => data(await api.GET('/groups')) });
}

/** After any group change: the list, the rows (group column and filter) and dependent views. */
export function invalidateGroups(qc: ReturnType<typeof useQueryClient>) {
  for (const k of [keys.groups, keys.tx, ['distinct'], ['transaction'], keys.insight, keys.overview]) {
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
