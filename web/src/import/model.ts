// The import window's data: one PreviewEditor per open import. A previewed import is edited on
// the server (debounced PATCH, re-staged rows). A committed or failed import can't be re-staged, so
// its editor works locally on the stored settings and rows, and "Reprocess" sends the result.

import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useSyncExternalStore } from 'react';

import {
  patchImport,
  useImport,
  useImportRows,
  type ImportChanges,
  type ImportDetail,
  type ImportPreview,
  type StagedRow,
} from '../api/hooks/imports';
import { keys } from '../api/keys';
import { PreviewEditor } from './editor';
import { uploadedPreview } from './queue';

export type Mode = 'preview' | 'stored';

/** A preview built from a stored import (no live re-staging). */
export function storedPreview(detail: ImportDetail, rows: StagedRow[]): ImportPreview {
  const s = detail.stats;
  const num = (k: string) => (typeof s[k] === 'number' ? s[k] : 0);
  const str = (k: string) => (typeof s[k] === 'string' ? s[k] : undefined);
  const dateFrom = str('dateFrom');
  const dateTo = str('dateTo');
  return {
    import: detail,
    stats: {
      rows: num('rows'),
      errors: num('errors'),
      duplicatesEstimate: num('duplicates'),
      newEstimate: num('new'),
      headerAt: num('headerAt'),
      dropped: num('dropped'),
      ...(dateFrom ? { dateFrom } : {}),
      ...(dateTo ? { dateTo } : {}),
    },
    suggestedAccount: {},
    sampleRows: rows.slice(0, 50),
    tailRows: [],
    missing: [],
    warnings: [],
    detected: { ignore: [] },
  };
}

/** Applies changes to a stored preview locally (what the controls show until Reprocess). */
function localPatch(get: () => ImportPreview | null) {
  return (ch: ImportChanges): Promise<ImportPreview> => {
    const cur = get();
    if (!cur) return Promise.reject(new Error('not loaded'));
    return Promise.resolve({
      ...cur,
      import: {
        ...cur.import,
        ...(ch.accountId ? { accountId: ch.accountId } : {}),
        ...(ch.mapping ? { mapping: ch.mapping } : {}),
        ...(ch.options ? { options: ch.options } : {}),
      },
    });
  };
}

export function useImportModel(id: string) {
  const qc = useQueryClient();
  const detail = useImport(id);
  const status = detail.data?.status;
  const mode: Mode | null = !status ? null : status === 'previewed' ? 'preview' : 'stored';
  const rows = useImportRows(id, false, mode === 'stored');

  const editor = useMemo(() => {
    if (!mode) return null;
    if (mode === 'preview') {
      return new PreviewEditor((ch) => patchImport(id, ch), {
        onSaved: (p) => {
          qc.setQueryData(keys.import(id), p.import);
        },
      });
    }
    let ed: PreviewEditor | null = null;
    ed = new PreviewEditor(
      localPatch(() => ed?.getState().preview ?? null),
      { delay: 0 },
    );
    return ed;
  }, [id, mode, qc]);

  useEffect(() => {
    if (!editor || mode !== 'preview') return;
    const fromUpload = uploadedPreview(id);
    if (fromUpload) editor.set(fromUpload);
    else void editor.load();
    return () => {
      editor.dispose();
    };
  }, [editor, id, mode]);

  const firstRows = rows.data?.pages[0]?.rows;
  useEffect(() => {
    if (editor && mode === 'stored' && detail.data && firstRows && !editor.getState().preview) {
      editor.set(storedPreview(detail.data, firstRows));
    }
  }, [editor, mode, detail.data, firstRows]);

  const state = useSyncExternalStore(editor?.subscribe ?? noopSubscribe, editor?.getState ?? emptyState);
  return { detail, mode, editor, state };
}

const noopSubscribe = () => () => undefined;
const EMPTY = { preview: null, draft: null, saving: false, problem: null };
const emptyState = () => EMPTY;
