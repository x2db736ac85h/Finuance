// Shared groups (Phase 19, ADR 0028): the timeline, a group's detail and report, and every change.
// Each change refetches everything under the `shared` key (and the personal groups, which carry
// the link).

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type SharedGroup = Schemas['SharedGroup'];
export type SharedGroupInput = Schemas['SharedGroupInput'];
export type SharedGroupDetail = Schemas['SharedGroupDetail'];
export type SharedMember = Schemas['SharedMember'];
export type SharedExpense = Schemas['SharedExpense'];
export type SharedExpenseInput = Schemas['SharedExpenseInput'];
export type SharedSplit = Schemas['SharedSplit'];
export type SharedSettlementInput = Schemas['SharedSettlementInput'];
export type SharedGroupReport = Schemas['SharedGroupReport'];
export type SharedKind = Schemas['SharedGroupKind'];
export type GroupTimeline = Schemas['GroupTimeline'];
export type TimelineGroup = Schemas['TimelineGroup'];
export type TimelineHint = Schemas['TimelineHint'];

export function useSharedGroups(enabled = true) {
  return useQuery({ queryKey: keys.shared, queryFn: async () => data(await api.GET('/shared-groups')), enabled });
}

export function useTimeline() {
  return useQuery({ queryKey: keys.timeline, queryFn: async () => data(await api.GET('/groups/timeline')) });
}

export function useSharedGroup(id: string) {
  return useQuery({
    queryKey: keys.sharedGroup(id),
    queryFn: async () => data(await api.GET('/shared-groups/{id}', { params: { path: { id } } })),
  });
}

export function useSharedReport(id: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.sharedReport(id),
    queryFn: async () => data(await api.GET('/shared-groups/{id}/report', { params: { path: { id } } })),
    enabled,
  });
}

function useSharedMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      for (const k of [keys.shared, keys.groups, keys.notifications, keys.activity]) {
        void qc.invalidateQueries({ queryKey: k });
      }
    },
  });
}

const path = (id: string) => ({ params: { path: { id } } });

export const useCreateShared = () =>
  useSharedMutation(async (body: SharedGroupInput) => data(await api.POST('/shared-groups', { body })));

export const useUpdateShared = () =>
  useSharedMutation(async ({ id, body }: { id: string; body: SharedGroupInput }) =>
    data(await api.PATCH('/shared-groups/{id}', { ...path(id), body })),
  );

export const useDeleteShared = () =>
  useSharedMutation(async (id: string) => {
    data(await api.DELETE('/shared-groups/{id}', path(id)));
  });

export const useInvite = () =>
  useSharedMutation(async ({ id, friendId }: { id: string; friendId: string }) =>
    data(await api.POST('/shared-groups/{id}/invites', { ...path(id), body: { friendId } })),
  );

export const useAccept = () =>
  useSharedMutation(async (id: string) => data(await api.POST('/shared-groups/{id}/accept', path(id))));

export const useDecline = () =>
  useSharedMutation(async (id: string) => {
    data(await api.POST('/shared-groups/{id}/decline', path(id)));
  });

export const useLeave = () =>
  useSharedMutation(async (id: string) => {
    data(await api.POST('/shared-groups/{id}/leave', path(id)));
  });

export const useRemoveMember = () =>
  useSharedMutation(async ({ id, memberId }: { id: string; memberId: string }) => {
    data(await api.DELETE('/shared-groups/{id}/members/{memberId}', { params: { path: { id, memberId } } }));
  });

export const useAddExpense = () =>
  useSharedMutation(async ({ id, body }: { id: string; body: SharedExpenseInput }) =>
    data(await api.POST('/shared-groups/{id}/expenses', { ...path(id), body })),
  );

export const useUpdateExpense = () =>
  useSharedMutation(async ({ id, expenseId, body }: { id: string; expenseId: string; body: SharedExpenseInput }) => {
    data(await api.PATCH('/shared-groups/{id}/expenses/{expenseId}', { params: { path: { id, expenseId } }, body }));
  });

export const useDeleteExpense = () =>
  useSharedMutation(async ({ id, expenseId }: { id: string; expenseId: string }) => {
    data(await api.DELETE('/shared-groups/{id}/expenses/{expenseId}', { params: { path: { id, expenseId } } }));
  });

export const useSettle = () =>
  useSharedMutation(async ({ id, body }: { id: string; body: SharedSettlementInput }) =>
    data(await api.POST('/shared-groups/{id}/settlements', { ...path(id), body })),
  );

export const useDeleteSettlement = () =>
  useSharedMutation(async ({ id, settlementId }: { id: string; settlementId: string }) => {
    data(
      await api.DELETE('/shared-groups/{id}/settlements/{settlementId}', { params: { path: { id, settlementId } } }),
    );
  });

/** GET /shared-groups/{id}/export: the CSV as a blob with the server's file name. */
export async function exportShared(id: string): Promise<{ blob: Blob; filename: string }> {
  const r = await api.GET('/shared-groups/{id}/export', { ...path(id), parseAs: 'blob' });
  const blob = data(r);
  const cd = r.response.headers.get('Content-Disposition') ?? '';
  const m = /filename="?([^";]+)"?/i.exec(cd);
  return { blob, filename: m?.[1] ?? 'shared-group.csv' };
}
