// Preferences sync (PLAN task 8.8, §7.4): the stores are the working copy, the server document is
// the saved one. Every store change schedules a save; after `delay` ms of quiet the persisted
// slices are collected and, if they differ from what the server has, sent with the version we
// read. On 409 stale_version (another browser saved meanwhile) the server's document wins, our
// own changes since the last save are re-applied on top of it, and the save is retried once.

import { ApiError } from '../api/client';
import type { Preferences } from '../api/hooks/prefs';
import { parsePrefs, type ParsedPrefs, type PrefsDoc } from './prefs';

type Raw = Record<string, unknown>;

export type SyncDeps = {
  put: (version: number, prefs: Raw) => Promise<Preferences>;
  /** The persisted slices of the stores, now. */
  collect: () => PrefsDoc;
  /** Writes a parsed document into the stores. */
  apply: (p: ParsedPrefs) => void;
  /** A save failed for another reason than a conflict; the next change tries again. */
  onError?: (e: unknown) => void;
  delay?: number;
};

const json = (x: unknown) => JSON.stringify(x);
const isRaw = (x: unknown): x is Raw => typeof x === 'object' && x !== null && !Array.isArray(x);

/** The server document with the keys (and views) that changed locally since base laid over it. */
export function mergeLocal(server: Raw, base: PrefsDoc, local: PrefsDoc): Raw {
  const out: Raw = { ...server };
  for (const k of Object.keys(local) as (keyof PrefsDoc)[]) {
    if (k === 'views' || json(local[k]) === json(base[k])) continue;
    out[k] = local[k];
  }
  const serverViews = server['views'];
  const views: Raw = isRaw(serverViews) ? { ...serverViews } : {};
  let viewsChanged = false;
  for (const [id, v] of Object.entries(local.views)) {
    if (json(v) === json(base.views[id])) continue;
    views[id] = v;
    viewsChanged = true;
  }
  if (viewsChanged) out['views'] = views;
  return out;
}

export class PrefsSync {
  private readonly deps: SyncDeps;
  private readonly delay: number;
  private version = 0;
  /** The server's document as stored, so keys this build doesn't know survive our saves. */
  private raw: Raw = {};
  /** What the server holds, in collected form; a save is needed when collect() differs. */
  private base: PrefsDoc | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private running: Promise<void> | null = null;
  private stopped = false;

  constructor(deps: SyncDeps) {
    this.deps = deps;
    this.delay = deps.delay ?? 500;
  }

  /** Hydrates the stores from the server's document and starts tracking changes from there. */
  start(doc: Preferences): void {
    this.adopt(doc);
  }

  /** A store changed: save after `delay` ms without further changes. */
  schedule(): void {
    if (this.stopped) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), this.delay);
  }

  /** Saves now if anything changed (also waits for a save already under way). */
  async flush(): Promise<void> {
    clearTimeout(this.timer);
    while (this.running) await this.running;
    if (this.stopped || !this.base || json(this.deps.collect()) === json(this.base)) return;
    this.running = this.save().finally(() => {
      this.running = null;
    });
    await this.running;
  }

  /** Stops saving (sign-out, unmount); a pending debounced save is dropped. */
  stop(): void {
    this.stopped = true;
    clearTimeout(this.timer);
  }

  private adopt(doc: Preferences): void {
    this.deps.apply(parsePrefs(doc.prefs));
    this.version = doc.version;
    this.raw = doc.prefs;
    this.base = this.deps.collect();
  }

  private async save(): Promise<void> {
    const base = this.base;
    if (!base) return;
    const local = this.deps.collect();
    try {
      this.saved(await this.deps.put(this.version, { ...this.raw, ...local }), local);
      return;
    } catch (e) {
      const current = staleCurrent(e);
      if (!current) {
        this.deps.onError?.(e);
        return;
      }
      // Server wins, then our changes since the last save go back on top.
      const merged = mergeLocal(current.prefs, base, local);
      this.deps.apply(parsePrefs(merged));
      this.version = current.version;
      this.raw = current.prefs;
      const retry = this.deps.collect();
      try {
        this.saved(await this.deps.put(this.version, { ...merged, ...retry }), retry);
      } catch (e2) {
        const again = staleCurrent(e2);
        if (again) this.adopt(again);
        else this.deps.onError?.(e2);
      }
    }
  }

  private saved(doc: Preferences, sent: PrefsDoc): void {
    this.version = doc.version;
    this.raw = doc.prefs;
    this.base = sent;
  }
}

function staleCurrent(e: unknown): Preferences | null {
  if (!(e instanceof ApiError) || e.code !== 'stale_version') return null;
  const current: unknown = (e.problem as { current?: unknown }).current;
  if (!isRaw(current)) return null;
  const { version, prefs } = current;
  return typeof version === 'number' && isRaw(prefs) ? { version, prefs } : null;
}
