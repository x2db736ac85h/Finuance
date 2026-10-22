// The upload queue (PLAN tasks 9.1, 9.2, 9.7): every way of opening files ends here. Files go up
// one at a time. A file that needs review opens the preview window; the next one waits until that
// window commits or cancels (`settle`). With the fast path on, a known layout is committed by the
// server straight away and reported with Review and Undo. A file imported before gets a toast
// with a link to that import.

import { create } from 'zustand';

import { ApiError } from '../api/client';
import {
  deleteImport,
  duplicateOf,
  getImport,
  uploadImport,
  type ImportDetail,
  type ImportPreview,
  type UploadResult,
} from '../api/hooks/imports';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { plural } from '../lib/format';
import { toast } from '../ui/toastStore';

export type QueueStatus = 'waiting' | 'uploading' | 'preview' | 'committing' | 'done' | 'skipped' | 'failed';
export type QueueItem = { key: number; name: string; status: QueueStatus; importId?: string };

export const useImportQueue = create<{ items: QueueItem[] }>()(() => ({ items: [] }));

export type QueueDeps = {
  upload: (file: File, opts: { autoCommit: boolean }) => Promise<UploadResult>;
  getImport: (id: string) => Promise<ImportDetail>;
  deleteImport: (id: string) => Promise<void>;
  navigate: (to: string) => void;
  /** Data changed (an import was committed or removed). */
  refresh: () => void;
  autoCommit: () => boolean;
  /** Poll interval and limit while waiting for a commit (PLAN: every 500 ms, at most 2 min). */
  pollMs?: number;
  pollLimitMs?: number;
};

const defaults: Omit<QueueDeps, 'navigate' | 'refresh' | 'autoCommit'> = {
  upload: uploadImport,
  getImport,
  deleteImport,
};

let deps: QueueDeps | null = null;
const files = new Map<number, File>();
/** Upload answers, so the preview window starts without asking again. */
const uploaded = new Map<string, ImportPreview>();

/** The upload's preview of importId (kept until the window settles). */
export function uploadedPreview(importId: string): ImportPreview | undefined {
  return uploaded.get(importId);
}
let nextKey = 1;
let running = false;
/** Resolves when the preview window of the file being reviewed commits or cancels. */
let reviewDone: (() => void) | null = null;

/** Wires the queue to the app (AppShell) or to fakes (tests). */
export function configureImports(d: Partial<QueueDeps> & Pick<QueueDeps, 'navigate' | 'refresh' | 'autoCommit'>): void {
  deps = { ...defaults, ...d };
}

/** Queues files and starts uploading if nothing is running. Non-CSV files are refused. */
export function importFiles(list: FileList | readonly File[]): void {
  const accepted = [...list].filter((f) => /\.(csv|tsv|txt)$/i.test(f.name) || f.type.startsWith('text/'));
  const refused = list.length - accepted.length;
  if (refused) toast(`Skipped ${plural(refused, 'file')} that isn't CSV, TSV or TXT.`, { kind: 'error' });
  if (!accepted.length) return;
  const added = accepted.map((f) => {
    const key = nextKey++;
    files.set(key, f);
    return { key, name: f.name, status: 'waiting' as const };
  });
  useImportQueue.setState((s) => ({ items: [...s.items.filter((i) => !finished(i)), ...added] }));
  void run();
}

/** The preview window of importId is done (committed or cancelled): the next file goes up. */
export function settle(importId: string, status: 'done' | 'skipped'): void {
  uploaded.delete(importId);
  setStatus((i) => i.importId === importId, status);
  const done = reviewDone;
  reviewDone = null;
  done?.();
}

/** "2 of 3" for the file under review, or null when it isn't part of a multi-file batch. */
export function stepOf(items: QueueItem[], importId: string): string | null {
  if (items.length < 2) return null;
  const at = items.findIndex((i) => i.importId === importId);
  return at < 0 ? null : `${at + 1} of ${items.length}`;
}

const finished = (i: QueueItem) => i.status === 'done' || i.status === 'skipped' || i.status === 'failed';

function setStatus(match: (i: QueueItem) => boolean, status: QueueStatus, importId?: string): void {
  useImportQueue.setState((s) => ({
    items: s.items.map((i) => (match(i) ? { ...i, status, ...(importId ? { importId } : {}) } : i)),
  }));
}

async function run(): Promise<void> {
  if (running || !deps) return;
  running = true;
  try {
    for (;;) {
      const item = useImportQueue.getState().items.find((i) => i.status === 'waiting');
      if (!item) break;
      await uploadOne(deps, item);
    }
    // a finished batch leaves nothing behind for the next one
    if (useImportQueue.getState().items.every(finished)) useImportQueue.setState({ items: [] });
  } finally {
    running = false;
  }
}

async function uploadOne(d: QueueDeps, item: QueueItem): Promise<void> {
  const file = files.get(item.key);
  files.delete(item.key);
  if (!file) return;
  const is = (i: QueueItem) => i.key === item.key;
  setStatus(is, 'uploading');
  let res: UploadResult;
  try {
    res = await d.upload(file, { autoCommit: d.autoCommit() });
  } catch (e) {
    const existing = duplicateOf(e);
    if (existing) {
      setStatus(is, 'skipped', existing);
      await reportDuplicate(d, file.name, existing);
    } else {
      setStatus(is, 'failed');
      toast(`${file.name}: ${errorMessage(e)}`, { kind: 'error' });
    }
    return;
  }
  const id = res.preview.import.id;
  if (res.autoCommitted) {
    setStatus(is, 'committing', id);
    const imp = await waitForCommit(d, id);
    if (imp?.status === 'committed') {
      setStatus(is, 'done');
      d.refresh();
      toast(`${committedMessage(imp)} from ${file.name}`, {
        actions: [
          {
            label: 'Review',
            run: () => {
              d.navigate(`/imports/${id}`);
            },
          },
          { label: 'Undo', run: () => void undo(d, id, file.name) },
        ],
      });
    } else {
      setStatus(is, 'failed');
      toast(`${file.name}: ${imp?.error ?? 'the import did not finish'}`, {
        kind: 'error',
        actions: [
          {
            label: 'Open',
            run: () => {
              d.navigate(`/imports/${id}`);
            },
          },
        ],
      });
    }
    return;
  }
  setStatus(is, 'preview', id);
  uploaded.set(id, res.preview);
  const reviewed = new Promise<void>((resolve) => {
    reviewDone = resolve;
  });
  d.navigate(`/imports/${id}`);
  await reviewed;
}

async function reportDuplicate(d: QueueDeps, name: string, existing: string): Promise<void> {
  let when = '';
  try {
    when = ` on ${fmtDateTime((await d.getImport(existing)).createdAt)}`;
  } catch {
    // the link still works without the date
  }
  toast(`${name} was already imported${when}.`, {
    actions: [
      {
        label: 'Open',
        run: () => {
          d.navigate(`/imports/${existing}`);
        },
      },
    ],
  });
}

/** Polls the import until it is no longer committing (or the limit passes); null on errors. */
export async function waitForCommit(
  d: Pick<QueueDeps, 'getImport' | 'pollMs' | 'pollLimitMs'>,
  id: string,
): Promise<ImportDetail | null> {
  const every = d.pollMs ?? 500;
  const until = Date.now() + (d.pollLimitMs ?? 120_000);
  for (;;) {
    try {
      const imp = await d.getImport(id);
      if (imp.status !== 'committing') return imp;
    } catch (e) {
      if (e instanceof ApiError && e.status < 500) return null;
      // a network blip or a server error: keep waiting
    }
    if (Date.now() >= until) return null;
    await new Promise((r) => setTimeout(r, every));
  }
}

/** "Imported 312 transactions · 45 duplicates skipped" from a committed import's stats. */
export function committedMessage(imp: ImportDetail): string {
  const n = Number(imp.stats['new'] ?? 0);
  const dup = Number(imp.stats['duplicates'] ?? 0);
  return `Imported ${plural(n, 'transaction')}${dup ? ` · ${plural(dup, 'duplicate')} skipped` : ''}`;
}

async function undo(d: QueueDeps, id: string, name: string): Promise<void> {
  try {
    await d.deleteImport(id);
    d.refresh();
    toast(`Removed the import of ${name}`);
  } catch (e) {
    toast(errorMessage(e), { kind: 'error' });
  }
}

/** Test hook: forget queued files and state. */
export function resetQueue(): void {
  files.clear();
  uploaded.clear();
  running = false;
  reviewDone = null;
  useImportQueue.setState({ items: [] });
}
