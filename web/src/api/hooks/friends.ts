// Friends (Phase 19, ADR 0027) and in-app notifications (19.8).

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Friend = Schemas['Friend'];
export type FriendCode = Schemas['FriendCode'];
export type Notification = Schemas['Notification'];

export function useFriends(enabled = true) {
  return useQuery({ queryKey: keys.friends, queryFn: async () => data(await api.GET('/friends')), enabled });
}

/** POST /friends/code: a new code (the previous one stops working). Shown once, kept in memory. */
export function useNewFriendCode() {
  return useMutation({ mutationFn: async () => data(await api.POST('/friends/code')) });
}

export function useAddFriend() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (code: string) => data(await api.POST('/friends', { body: { code } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.friends });
      void qc.invalidateQueries({ queryKey: keys.activity });
    },
  });
}

export function useRemoveFriend() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      data(await api.DELETE('/friends/{id}', { params: { path: { id } } }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.friends });
    },
  });
}

/** GET /notifications: checked every minute and when the window gets focus again. */
export function useNotifications() {
  return useQuery({
    queryKey: keys.notifications,
    queryFn: async () => data(await api.GET('/notifications', { params: { query: { limit: 30 } } })),
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}

/** POST /notifications/read: every unread one. */
export function useMarkNotificationsRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      data(await api.POST('/notifications/read', { body: {} }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.notifications }),
  });
}
