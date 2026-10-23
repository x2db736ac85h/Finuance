// Migration from the single-file app (PLAN §4.10, task 14.1): start a run with what the wizard
// read from the backup, poll it, list the past runs.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';
import { invalidateData } from './accounts';
import { invalidateGroups } from './groups';

export type LegacyImport = Schemas['LegacyImport'];
export type LegacyReport = Schemas['LegacyImportReport'];

const active = (r: LegacyImport | undefined) => r?.status === 'queued' || r?.status === 'running';

export function useLegacyImports() {
  return useQuery({ queryKey: keys.legacyImports, queryFn: async () => data(await api.GET('/legacy-imports')) });
}

/** One run, polled every 500 ms while it is queued or running; everything is refetched when it ends. */
export function useLegacyImport(id: string | null) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: keys.legacyImport(id ?? ''),
    enabled: id !== null,
    queryFn: async () => {
      const r = data(await api.GET('/legacy-imports/{id}', { params: { path: { id: id ?? '' } } }));
      if (!active(r)) {
        invalidateData(qc);
        invalidateGroups(qc);
        void qc.invalidateQueries({ queryKey: keys.legacyImports });
        void qc.invalidateQueries({ queryKey: keys.chats });
      }
      return r;
    },
    refetchInterval: (q) => (active(q.state.data) ? 500 : false),
  });
}

/** POST /legacy-imports: the manifest part, then one file part per legacy file in its order. */
export function useStartLegacyImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ manifest, files }: { manifest: object; files: Blob[] }) =>
      data(
        await api.POST('/legacy-imports', {
          // the generated type models the parts as strings
          body: { manifest: '' },
          bodySerializer: () => {
            const fd = new FormData();
            fd.append('manifest', JSON.stringify(manifest));
            files.forEach((f, i) => {
              fd.append('file', f, `file-${String(i + 1)}.csv`);
            });
            return fd;
          },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.legacyImports }),
  });
}
