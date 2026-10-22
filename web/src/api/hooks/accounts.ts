import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Account = Schemas['Account'];

export function useAccounts() {
  return useQuery({ queryKey: keys.accounts, queryFn: async () => data(await api.GET('/accounts')) });
}

/** Data changes that affect every view (import, demo data, account delete). */
export function invalidateData(qc: ReturnType<typeof useQueryClient>) {
  for (const k of [keys.accounts, keys.imports(), keys.categories, keys.tx, keys.insight, keys.overview]) {
    void qc.invalidateQueries({ queryKey: k });
  }
}

/** POST /demo-data: the demo account with the committed demo year. */
export function useLoadDemo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => data(await api.POST('/demo-data')),
    onSuccess: () => {
      invalidateData(qc);
    },
  });
}

/** POST /accounts with a name (the import preview's "Create account"). */
export function useCreateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => data(await api.POST('/accounts', { body: { name } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.accounts }),
  });
}
