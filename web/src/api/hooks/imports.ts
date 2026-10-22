// Imports (PLAN §6.2, §7.10): upload, preview edits, commit, history. The preview window keeps its
// own state (import/editor.ts); these are the requests and the cached lists.

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, ApiError, data, type Schemas } from '../client';
import { keys } from '../keys';
import { invalidateData } from './accounts';

export type ImportPreview = Schemas['ImportPreview'];
export type ImportDetail = Schemas['ImportDetail'];
export type ImportSummary = Schemas['ImportSummary'];
export type ImportChanges = Schemas['ImportChanges'];
export type ImportOptions = Schemas['ImportOptions'];
export type Mapping = Schemas['Mapping'];
export type ColRef = Schemas['ColRef'];
export type StagedRow = Schemas['StagedRow'];
export type ImportLines = Schemas['ImportLines'];

/** Import history, newest first; all accounts or one. Polls while an import is committing. */
export function useImports(accountId?: string) {
  return useQuery({
    queryKey: keys.imports(accountId),
    queryFn: async () => data(await api.GET('/imports', { params: { query: accountId ? { accountId } : {} } })),
    refetchInterval: (q) => (q.state.data?.some((i) => i.status === 'committing') ? 1000 : false),
  });
}

export type UploadResult = { preview: ImportPreview; autoCommitted: boolean };

/**
 * POST /imports (multipart). With autoCommit a known, error-free layout is committed right away
 * (202). A file already imported throws ApiError duplicate_file with existingImportId.
 */
export async function uploadImport(
  file: File,
  opts: { accountId?: string; autoCommit: boolean },
): Promise<UploadResult> {
  const r = await api.POST('/imports', {
    params: { query: { autoCommit: opts.autoCommit } },
    // the generated type models the binary part as a string
    body: { file: file as unknown as string, ...(opts.accountId ? { accountId: opts.accountId } : {}) },
    bodySerializer: () => {
      const fd = new FormData();
      fd.append('file', file, file.name);
      if (opts.accountId) fd.append('accountId', opts.accountId);
      return fd;
    },
  });
  return { preview: data(r), autoCommitted: r.response.status === 202 };
}

/** The id of the existing import when e is a duplicate_file problem. */
export function duplicateOf(e: unknown): string | null {
  if (!(e instanceof ApiError) || e.code !== 'duplicate_file') return null;
  const id = (e.problem as { existingImportId?: unknown }).existingImportId;
  return typeof id === 'string' ? id : null;
}

export async function getImport(id: string): Promise<ImportDetail> {
  return data(await api.GET('/imports/{id}', { params: { path: { id } } }));
}

/** PATCH /imports/{id}: re-stages with the changes and returns the new preview ({} re-reads it). */
export async function patchImport(id: string, changes: ImportChanges): Promise<ImportPreview> {
  return data(await api.PATCH('/imports/{id}', { params: { path: { id } }, body: changes }));
}

export async function commitImport(id: string, rememberMapping: boolean): Promise<ImportDetail> {
  return data(await api.POST('/imports/{id}/commit', { params: { path: { id } }, body: { rememberMapping } }));
}

export async function reprocessImport(id: string, changes: ImportChanges): Promise<ImportDetail> {
  return data(await api.POST('/imports/{id}/reprocess', { params: { path: { id } }, body: changes }));
}

export async function deleteImport(id: string): Promise<void> {
  await api.DELETE('/imports/{id}', { params: { path: { id } } });
}

/** GET /imports/{id}/file: the original upload. */
export const importFileUrl = (id: string) => `/api/v1/imports/${id}/file`;

export function useImport(id: string) {
  return useQuery({ queryKey: keys.import(id), queryFn: () => getImport(id) });
}

/** The first `limit` records of the file with their status, for the given options (key). */
export function useImportLines(id: string, optionsKey: string, limit = 18, enabled = true) {
  return useQuery({
    queryKey: [...keys.importLines(id, optionsKey), limit],
    queryFn: async () => data(await api.GET('/imports/{id}/lines', { params: { path: { id }, query: { limit } } })),
    enabled,
    placeholderData: (prev) => prev,
  });
}

const PAGE = 100;

/** Staged rows in pages (all, or only the ones with errors). */
export function useImportRows(id: string, onlyErrors: boolean, enabled = true) {
  return useInfiniteQuery({
    queryKey: keys.importRows(id, onlyErrors),
    queryFn: async ({ pageParam }) =>
      data(
        await api.GET('/imports/{id}/rows', {
          params: { path: { id }, query: { offset: pageParam, limit: PAGE, onlyErrors } },
        }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const seen = pages.reduce((n, p) => n + p.rows.length, 0);
      return seen < last.total ? seen : undefined;
    },
    enabled,
  });
}

/** Delete an import (cancel a preview, or remove a committed one) and refresh every view. */
export function useDeleteImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deleteImport,
    onSuccess: () => {
      invalidateData(qc);
    },
  });
}
