// Wires the stores to the preferences sync: collect/apply over uiStore + viewStore, one sync per
// signed-in session (started by RequireAuth once GET /preferences has loaded).

import { create } from 'zustand';

import { putPrefs, type Preferences } from '../api/hooks/prefs';
import { useChat } from '../assistant/chatStore';
import { useAnswerNotes } from '../assistant/answerNotes';
import { useCashDialogs } from '../cash/store';
import { useDupDialog } from '../duplicates/store';
import { resetQueue } from '../import/queue';
import { useSelection } from './selectionStore';
import type { ParsedPrefs, PrefsDoc } from './prefs';
import { PrefsSync } from './prefsSync';
import { collectUi, NARROW_PX, uiDefaults, useUi } from './uiStore';
import { collectViews, useViews } from './viewStore';

export function collectPrefs(): PrefsDoc {
  return { v: 1, ...collectUi(useUi.getState()), views: collectViews(useViews.getState().views) };
}

export function applyPrefs(p: ParsedPrefs): void {
  useUi.getState().hydrate(p, window.innerWidth < NARROW_PX);
  useViews.getState().hydrate(p);
}

/** Back to the defaults (sign-out, another user signing in). */
export function resetStores(): void {
  useUi.setState(uiDefaults);
  useViews.getState().reset();
}

/**
 * Forgets everything the app holds in memory about the person who was signed in: the chat on
 * screen (questions, answers, the rows a tool returned), the selection, queued uploads and open
 * dialogs, besides the settings. For sign-out and for another account signing in in the same tab,
 * so the next person never sees the previous one's data (WEB-1).
 */
export function resetSession(): void {
  useChat.getState().ctrl?.abort(); // an answer still streaming belongs to the session that ends
  useChat.setState(useChat.getInitialState(), true);
  useAnswerNotes.getState().reset();
  useSelection.setState(useSelection.getInitialState(), true);
  useCashDialogs.setState(useCashDialogs.getInitialState(), true);
  useDupDialog.setState(useDupDialog.getInitialState(), true);
  resetQueue();
  resetStores();
}

let current: PrefsSync | null = null;
/** Whose data the in-memory stores hold (null: nobody's). */
let sessionUser: string | null = null;

/** The user whose preferences are loaded into the stores (null before and after a session). */
export const usePrefsReady = create<{ userId: string | null }>()(() => ({ userId: null }));

/**
 * Hydrates the stores from doc and saves every later change. Returns the function that stops it.
 * onError reports failed saves (not conflicts, which are merged).
 */
export function startPrefsSync(
  userId: string,
  doc: Preferences,
  onError?: (e: unknown) => void,
  defaults?: Record<string, unknown>,
): () => void {
  current?.stop();
  // the same person again (their preferences were reloaded): only the settings start over
  if (sessionUser !== userId) resetSession();
  else resetStores();
  sessionUser = userId;
  const sync = new PrefsSync({
    put: putPrefs,
    collect: collectPrefs,
    apply: applyPrefs,
    ...(onError ? { onError } : {}),
  });
  // Instance defaults (an administrator's, Phase 19) under the saved document: only keys the user
  // never saved take them; the first save then keeps them like any other setting.
  sync.start(defaults ? { ...doc, prefs: { ...defaults, ...doc.prefs } } : doc);
  current = sync;
  usePrefsReady.setState({ userId });
  const unsubUi = useUi.subscribe(() => {
    sync.schedule();
  });
  const unsubViews = useViews.subscribe(() => {
    sync.schedule();
  });
  // A tab being hidden may be closed next: save without waiting for the debounce.
  const onHide = () => {
    if (document.visibilityState === 'hidden') void sync.flush();
  };
  document.addEventListener('visibilitychange', onHide);
  return () => {
    unsubUi();
    unsubViews();
    document.removeEventListener('visibilitychange', onHide);
    sync.stop();
    if (current === sync) {
      current = null;
      usePrefsReady.setState({ userId: null });
    }
  };
}

/** Saves pending changes now (before signing out). */
export async function flushPrefs(): Promise<void> {
  await current?.flush();
}
