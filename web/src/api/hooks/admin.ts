// The admin panel's API (Phase 19, ADR 0025). Every route answers 404 for anyone but an
// administrator; the panel is only linked for them.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type AdminUser = Schemas['AdminUser'];
export type InstanceSettings = Schemas['InstanceSettingsDoc'];
export type AdminTheme = Schemas['Theme'];
export type LimitOverrides = Schemas['LimitOverrides'];
export type DeveloperInfo = Schemas['DeveloperInfo'];

export function useAdminUsers(search: string, cursor: string) {
  return useQuery({
    queryKey: [...keys.admin, 'users', search, cursor],
    queryFn: async () =>
      data(
        await api.GET('/admin/users', {
          params: { query: { ...(search ? { search } : {}), ...(cursor ? { cursor } : {}) } },
        }),
      ),
  });
}

/**
 * The Developer section (FINUANCE_DEVELOPER_MODE): 404 while the mode is off, which hides the
 * section. Everything it shows comes from here, never from the bundle.
 */
export function useDeveloperInfo(enabled = true) {
  return useQuery({
    queryKey: [...keys.admin, 'developer'],
    queryFn: async () => data(await api.GET('/admin/developer')),
    retry: false,
    enabled,
  });
}

export function useAdminUser(id: string | null) {
  return useQuery({
    queryKey: [...keys.admin, 'user', id],
    queryFn: async () => data(await api.GET('/admin/users/{id}', { params: { path: { id: id ?? '' } } })),
    enabled: id !== null,
  });
}

function useAdminMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.admin });
    },
  });
}

export function useCreateUser() {
  return useAdminMutation(async (body: Schemas['AdminCreateUserRequest']) =>
    data(await api.POST('/admin/users', { body })),
  );
}

export function useUpdateUser() {
  return useAdminMutation(async ({ id, body }: { id: string; body: Schemas['AdminUpdateUserRequest'] }) =>
    data(await api.PATCH('/admin/users/{id}', { params: { path: { id } }, body })),
  );
}

export function useDeleteUser() {
  return useAdminMutation(async ({ id, confirmEmail }: { id: string; confirmEmail: string }) => {
    await api.DELETE('/admin/users/{id}', { params: { path: { id } }, body: { confirmEmail } });
  });
}

export function useResetPassword() {
  return useAdminMutation(async (id: string) =>
    data(await api.POST('/admin/users/{id}/reset-password', { params: { path: { id } } })),
  );
}

export function useRevokeUserSessions() {
  return useAdminMutation(async (id: string) =>
    data(await api.POST('/admin/users/{id}/revoke-sessions', { params: { path: { id } } })),
  );
}

export function useInstanceSettings() {
  return useQuery({
    queryKey: [...keys.admin, 'settings'],
    queryFn: async () => data(await api.GET('/admin/settings')),
  });
}

export function usePutInstanceSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: InstanceSettings) => data(await api.PUT('/admin/settings', { body })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.admin });
      await qc.invalidateQueries({ queryKey: keys.meta });
    },
  });
}

export function useInstanceUsage(days: number) {
  return useQuery({
    queryKey: [...keys.admin, 'usage', days],
    queryFn: async () => data(await api.GET('/admin/usage', { params: { query: { days } } })),
  });
}

export function useAdminThemes() {
  return useQuery({ queryKey: [...keys.admin, 'themes'], queryFn: async () => data(await api.GET('/admin/themes')) });
}

function useThemeMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.admin });
      await qc.invalidateQueries({ queryKey: keys.meta });
    },
  });
}

export function useCreateTheme() {
  return useThemeMutation(async (body: Schemas['ThemeCreateRequest']) =>
    data(await api.POST('/admin/themes', { body })),
  );
}

export function useUpdateTheme() {
  return useThemeMutation(async ({ id, body }: { id: string; body: Schemas['ThemeUpdateRequest'] }) =>
    data(await api.PUT('/admin/themes/{id}', { params: { path: { id } }, body })),
  );
}

export function useSetThemeStatus() {
  return useThemeMutation(async ({ id, body }: { id: string; body: Schemas['ThemeStatusRequest'] }) =>
    data(await api.PUT('/admin/themes/{id}/status', { params: { path: { id } }, body })),
  );
}

export function useDeleteTheme() {
  return useThemeMutation(async (id: string) => {
    await api.DELETE('/admin/themes/{id}', { params: { path: { id } } });
  });
}
