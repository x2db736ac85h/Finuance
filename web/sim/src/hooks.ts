// Server state through TanStack Query: the state polled every two seconds, the catalogue once,
// the configuration with its generation. Every change replaces the whole configuration with the
// generation read; a 409 means someone else changed it first, and the page reloads it.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { type Client, ControlError, data, type Schemas } from './api';
import type { paths } from './api/schema';

export type PresetName = paths['/presets/{name}']['post']['parameters']['path']['name'];

export type Config = Schemas['Config'];
export type ConfigDoc = Schemas['ConfigDoc'];

export function useSimState(client: Client) {
  return useQuery({
    queryKey: ['state'],
    queryFn: async () => data(await client.GET('/state')),
    refetchInterval: 2000,
  });
}

export function useCatalogue(client: Client) {
  return useQuery({
    queryKey: ['catalogue'],
    queryFn: async () => data(await client.GET('/catalogue')),
    staleTime: Infinity,
  });
}

export function useConfig(client: Client) {
  return useQuery({ queryKey: ['config'], queryFn: async () => data(await client.GET('/config')) });
}

/** Replaces the configuration with change(current); the answer (held to the ceilings) is stored. */
export function useChangeConfig(client: Client) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (change: (c: Config) => Config) => {
      const doc = qc.getQueryData<ConfigDoc>(['config']);
      if (!doc) throw new ControlError(0);
      const r = await client.PUT('/config', { body: { generation: doc.generation, config: change(doc.config) } });
      if (r.response.status === 409) {
        qc.setQueryData(['config'], r.error);
        throw new ControlError(409, {
          status: 409,
          code: 'stale_generation',
          title: 'Someone else changed the settings first; they are reloaded.',
        });
      }
      return data(r);
    },
    onSuccess: (doc) => {
      qc.setQueryData(['config'], doc);
      void qc.invalidateQueries({ queryKey: ['state'] });
    },
  });
}

/** Asks every animal that plays to rebuild its history with the configured profile. */
export function useRebuildHistory(client: Client) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => data(await client.POST('/history/rebuild')),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['state'] });
    },
  });
}

/** A POST that answers the configuration: pause, resume, reset, a preset. */
export function useConfigAction(client: Client) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (what: { kind: 'pause' | 'resume' | 'reset' } | { kind: 'preset'; name: PresetName }) => {
      switch (what.kind) {
        case 'pause':
          return data(await client.POST('/pause'));
        case 'resume':
          return data(await client.POST('/resume'));
        case 'reset':
          return data(await client.POST('/config/reset'));
        case 'preset':
          return data(await client.POST('/presets/{name}', { params: { path: { name: what.name } } }));
      }
    },
    onSuccess: (doc) => {
      qc.setQueryData(['config'], doc);
      void qc.invalidateQueries({ queryKey: ['state'] });
    },
  });
}

export function useActNow(client: Client) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas['ActRequest']) => data(await client.POST('/act', { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['state'] }),
  });
}
