// Personal access tokens (ADR 0036): for the fin CLI and scripts. The secret comes back once,
// from the create call; the list never holds it.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';

export type AccessToken = Schemas['AccessToken'];

const tokensKey = ['tokens'] as const;

export function useAccessTokens() {
  return useQuery({ queryKey: tokensKey, queryFn: async () => data(await api.GET('/me/tokens')) });
}

export function useCreateAccessToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas['AccessTokenRequest']) => data(await api.POST('/me/tokens', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: tokensKey }),
  });
}

export function useRevokeAccessToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      data(await api.DELETE('/me/tokens/{id}', { params: { path: { id } } }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: tokensKey }),
  });
}
