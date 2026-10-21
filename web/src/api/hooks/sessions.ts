import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Session = Schemas['Session'];

export function useSessions() {
  return useQuery({ queryKey: keys.sessions, queryFn: async () => data(await api.GET('/me/sessions')) });
}

export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE('/me/sessions/{id}', { params: { path: { id } } });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sessions }),
  });
}
