// Two-step sign-in (ADR 0040): a code from an authenticator app after the password. The secret
// and the recovery codes each come back once, from the call that makes them; nothing here keeps them.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type SecondFactor = Schemas['SecondFactor'];

const key = ['second-factor'] as const;

export function useSecondFactor() {
  return useQuery({ queryKey: key, queryFn: async () => data(await api.GET('/me/second-factor')) });
}

/** After a change the account's own record (`secondFactor`, `secondFactorRequired`) changes too. */
function useRefresh() {
  const qc = useQueryClient();
  return () => Promise.all([qc.invalidateQueries({ queryKey: key }), qc.invalidateQueries({ queryKey: keys.me })]);
}

export function useStartSecondFactor() {
  return useMutation({ mutationFn: async () => data(await api.POST('/me/second-factor')) });
}

export function useConfirmSecondFactor() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (code: string) => data(await api.POST('/me/second-factor/confirm', { body: { code } })),
    onSuccess: refresh,
  });
}

export function useNewRecoveryCodes() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (code: string) => data(await api.POST('/me/second-factor/recovery-codes', { body: { code } })),
    onSuccess: refresh,
  });
}

export function useDisableSecondFactor() {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (body: Schemas['DisableSecondFactorRequest']) => {
      data(await api.POST('/me/second-factor/disable', { body }));
    },
    onSuccess: refresh,
  });
}
