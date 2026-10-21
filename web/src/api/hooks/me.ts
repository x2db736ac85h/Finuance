import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type User = Schemas['User'];
export type Meta = Schemas['Meta'];

/** Public instance information (signup enabled, demo mode). */
export function useMeta() {
  return useQuery({ queryKey: keys.meta, queryFn: async () => data(await api.GET('/meta')), staleTime: Infinity });
}

/** The signed-in user; a 401 sends the browser to /login (client middleware). */
export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: async () => data(await api.GET('/me')),
    retry: false,
    staleTime: 60_000,
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas['LoginRequest']) => data(await api.POST('/auth/login', { body })),
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(keys.me, user);
    },
  });
}

export function useSignup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas['SignupRequest']) => data(await api.POST('/auth/signup', { body })),
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(keys.me, user);
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.POST('/auth/logout');
    },
    onSettled: () => {
      qc.clear();
    },
  });
}

export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (displayName: string) => data(await api.PATCH('/me', { body: { displayName } })),
    onSuccess: (user) => {
      qc.setQueryData(keys.me, user);
    },
  });
}

export function useChangePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas['ChangePasswordRequest']) => {
      await api.POST('/me/password', { body });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sessions }),
  });
}

export function useDeleteMe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (password: string) => {
      await api.DELETE('/me', { body: { password } });
    },
    onSuccess: () => {
      qc.clear();
    },
  });
}
