// Goals (Phase 20, ADR 0031): savings targets and reductions with their progress. Every change
// refetches the list and the assistant's changes (an applied proposal is one).

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Goal = Schemas['Goal'];
export type GoalInput = Schemas['GoalInput'];
export type GoalUpdate = Schemas['GoalUpdate'];

export function useGoals() {
  return useQuery({ queryKey: keys.goals, queryFn: async () => data(await api.GET('/goals')).goals });
}

function invalidateGoals(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: keys.goals });
  void qc.invalidateQueries({ queryKey: keys.aiChanges });
}

export function useCreateGoal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: GoalInput) => data(await api.POST('/goals', { body })),
    onSuccess: () => {
      invalidateGoals(qc);
    },
  });
}

/** PATCH /goals/{id}: the whole goal with the version read (409 stale_version otherwise). */
export function useUpdateGoal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: GoalUpdate }) =>
      data(await api.PATCH('/goals/{id}', { params: { path: { id } }, body })),
    onSettled: () => {
      invalidateGoals(qc);
    },
  });
}

export function useDeleteGoal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      data(await api.DELETE('/goals/{id}', { params: { path: { id } } }));
    },
    onSettled: () => {
      invalidateGoals(qc);
    },
  });
}
