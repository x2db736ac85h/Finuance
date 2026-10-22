import { useQuery } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type Preferences = Schemas['Preferences'];

/**
 * GET /preferences, read once per sign-in: after that the preferences sync owns the document
 * (stores → debounced PUT), so the query never refetches on its own.
 */
export function usePrefs(enabled: boolean) {
  return useQuery({
    queryKey: keys.prefs,
    queryFn: async () => data(await api.GET('/preferences')),
    enabled,
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

/** PUT /preferences; a version mismatch throws ApiError stale_version carrying `current`. */
export async function putPrefs(version: number, prefs: Preferences['prefs']): Promise<Preferences> {
  return data(await api.PUT('/preferences', { body: { version, prefs } }));
}
