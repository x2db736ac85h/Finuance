// The preview window's working copy (PLAN §7.10): the server's last preview plus the user's
// changes not saved yet. Every change waits `delay` ms for more (typing a skip count, clicking
// through roles) and is then sent as one PATCH; the screen always renders the server's answer, with
// pending changes shown on top. A 422 drops the pending changes and keeps the problem to show.

import type { Problem } from '../api/client';
import { ApiError } from '../api/client';
import type { ImportChanges, ImportOptions, ImportPreview, Mapping } from '../api/hooks/imports';

export type EditorState = {
  preview: ImportPreview | null;
  /** Changes not answered by the server yet. */
  draft: ImportChanges | null;
  saving: boolean;
  problem: Problem | null;
};

export type Effective = { accountId: string | undefined; mapping: Mapping; options: ImportOptions };

/** The settings the controls show: the preview's, with the pending changes laid over them. */
export function effective(s: EditorState): Effective | null {
  if (!s.preview) return null;
  const imp = s.preview.import;
  return {
    accountId: s.draft?.accountId ?? imp.accountId,
    mapping: s.draft?.mapping ?? imp.mapping,
    options: { ...imp.options, ...s.draft?.options },
  };
}

type Patch = (changes: ImportChanges) => Promise<ImportPreview>;

export class PreviewEditor {
  private state: EditorState = { preview: null, draft: null, saving: false, problem: null };
  private readonly listeners = new Set<() => void>();
  private timer: ReturnType<typeof setTimeout> | undefined;
  private running: Promise<void> | null = null;
  private readonly patch: Patch;
  private readonly delay: number;
  private readonly onSaved: ((p: ImportPreview) => void) | undefined;

  constructor(patch: Patch, opts: { delay?: number; onSaved?: (p: ImportPreview) => void } = {}) {
    this.patch = patch;
    this.delay = opts.delay ?? 300;
    this.onSaved = opts.onSaved;
  }

  getState = (): EditorState => this.state;

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  };

  /** Starts from a preview the caller already has (the upload's answer). */
  set(preview: ImportPreview): void {
    this.update({ preview, problem: null });
  }

  /** Loads the preview by re-reading the import with its stored settings (PATCH {}). */
  async load(): Promise<void> {
    this.update({ saving: true });
    try {
      this.update({ preview: await this.patch({}), saving: false, problem: null });
    } catch (e) {
      this.update({ saving: false, problem: problemOf(e) });
    }
  }

  /**
   * Queues a change. Options merge into the full options document (the server replaces it as a
   * whole); a mapping replaces the mapping. Changing options without a mapping lets the server keep
   * or re-detect the mapping, so a pending mapping is only sent along when the user set it.
   */
  change(ch: ImportChanges): void {
    const cur = effective(this.state);
    if (!cur) return;
    const draft: ImportChanges = { ...this.state.draft };
    if (ch.accountId !== undefined) draft.accountId = ch.accountId;
    if (ch.mapping) draft.mapping = ch.mapping;
    if (ch.options) draft.options = { ...cur.options, ...ch.options };
    this.update({ draft, problem: null });
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), this.delay);
  }

  /** Sends pending changes now (before committing) and waits for the answer. */
  async flush(): Promise<void> {
    clearTimeout(this.timer);
    while (this.running) await this.running;
    const draft = this.state.draft;
    if (!draft) return;
    this.running = this.send(draft).finally(() => {
      this.running = null;
    });
    await this.running;
  }

  /** Drops a pending debounce (the window closed); listeners stay with their subscriptions. */
  dispose(): void {
    clearTimeout(this.timer);
  }

  private async send(draft: ImportChanges): Promise<void> {
    this.update({ saving: true });
    try {
      const preview = await this.patch(draft);
      // Changes made while the request was out stay pending and go in the next one.
      const rest = this.state.draft === draft ? null : this.state.draft;
      this.update({ preview, draft: rest, saving: false, problem: null });
      this.onSaved?.(preview);
      if (rest) {
        clearTimeout(this.timer);
        this.timer = setTimeout(() => void this.flush(), this.delay);
      }
    } catch (e) {
      this.update({
        draft: this.state.draft === draft ? null : this.state.draft,
        saving: false,
        problem: problemOf(e),
      });
    }
  }

  private update(p: Partial<EditorState>): void {
    this.state = { ...this.state, ...p };
    for (const fn of this.listeners) fn();
  }
}

function problemOf(e: unknown): Problem {
  if (e instanceof ApiError) return e.problem;
  return {
    type: '/problems/internal',
    title: "Can't reach the Finuance server. Check your connection and try again.",
    status: 0,
    code: 'internal',
  };
}
