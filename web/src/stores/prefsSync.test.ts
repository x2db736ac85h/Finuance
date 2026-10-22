import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client';
import type { Preferences } from '../api/hooks/prefs';
import { applyPrefs, collectPrefs, resetStores } from './persist';
import { mergeLocal, PrefsSync } from './prefsSync';
import { useUi } from './uiStore';
import { useViews } from './viewStore';

type Put = (version: number, prefs: Record<string, unknown>) => Promise<Preferences>;

function stale(current: Preferences) {
  return new ApiError({
    type: '/problems/stale_version',
    title: 'Stale version',
    status: 409,
    code: 'stale_version',
    current,
  } as ConstructorParameters<typeof ApiError>[0]);
}

/** A fake server: stores what was put, answers 409 when the version is behind. */
function server(initial: Preferences) {
  const state = { ...initial };
  const calls: { version: number; prefs: Record<string, unknown> }[] = [];
  const put = vi.fn<Put>((version, prefs) => {
    calls.push({ version, prefs });
    if (version !== state.version) return Promise.reject(stale({ ...state }));
    state.version += 1;
    state.prefs = prefs;
    return Promise.resolve({ ...state });
  });
  return { state, calls, put };
}

function startSync(put: Put, doc: Preferences, onError?: (e: unknown) => void) {
  const sync = new PrefsSync({
    put,
    collect: collectPrefs,
    apply: applyPrefs,
    delay: 500,
    ...(onError ? { onError } : {}),
  });
  sync.start(doc);
  const schedule = () => {
    sync.schedule();
  };
  const unsub = [useUi.subscribe(schedule), useViews.subscribe(schedule)];
  cleanups.push(() => {
    unsub.forEach((u) => {
      u();
    });
    sync.stop();
  });
  return { sync };
}
const cleanups: (() => void)[] = [];

beforeEach(() => {
  vi.useFakeTimers();
  resetStores();
});
afterEach(() => {
  cleanups.splice(0).forEach((c) => {
    c();
  });
  vi.useRealTimers();
});

describe('PrefsSync', () => {
  it('hydrates the stores from the server document without saving', async () => {
    const srv = server({ version: 3, prefs: { theme: 'light', ui: 1.1, views: { all: { period: 'ytd' } } } });
    startSync(srv.put, srv.state);
    expect(useUi.getState().themePref).toBe('light');
    expect(useUi.getState().uiScalePref).toBe(1.1);
    expect(useViews.getState().views['all']?.period).toBe('ytd');
    await vi.advanceTimersByTimeAsync(2000);
    expect(srv.put).not.toHaveBeenCalled();
  });

  it('debounces: one PUT 500 ms after the last change, with the version read (0 first)', async () => {
    const srv = server({ version: 0, prefs: {} });
    startSync(srv.put, srv.state);
    useUi.getState().setTheme('light');
    await vi.advanceTimersByTimeAsync(300);
    useUi.getState().setUiScale(1.2);
    await vi.advanceTimersByTimeAsync(499);
    expect(srv.put).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(srv.calls).toHaveLength(1);
    expect(srv.calls[0]?.version).toBe(0);
    expect(srv.calls[0]?.prefs).toMatchObject({ v: 1, theme: 'light', ui: 1.2 });
    // the next save carries the new version
    useUi.getState().setTheme('dark');
    await vi.advanceTimersByTimeAsync(500);
    expect(srv.calls[1]?.version).toBe(1);
    expect(srv.state.version).toBe(2);
  });

  it('does not save session-only changes (search, filters)', async () => {
    const srv = server({ version: 0, prefs: {} });
    startSync(srv.put, srv.state);
    useViews.getState().patch('all', {});
    await vi.advanceTimersByTimeAsync(500);
    srv.put.mockClear();
    useViews.getState().patch('all', { search: 'kino', filters: { payee: { cond: 'x', excluded: [] } } });
    await vi.advanceTimersByTimeAsync(1000);
    expect(srv.put).not.toHaveBeenCalled();
  });

  it('keeps keys it does not know when saving', async () => {
    const srv = server({ version: 1, prefs: { theme: 'dark', futureKey: { x: 1 } } });
    startSync(srv.put, srv.state);
    useUi.getState().setTheme('light');
    await vi.advanceTimersByTimeAsync(500);
    expect(srv.state.prefs).toMatchObject({ theme: 'light', futureKey: { x: 1 } });
  });

  it('on 409 the server wins, then the local change is re-applied and saved', async () => {
    const srv = server({ version: 1, prefs: { theme: 'dark', ui: 'auto' } });
    startSync(srv.put, { version: 1, prefs: { theme: 'dark', ui: 'auto' } });
    // another browser saves a new size meanwhile
    srv.state.version = 2;
    srv.state.prefs = { theme: 'dark', ui: 1.3, sbOpen: true };
    useUi.getState().setTheme('light');
    await vi.advanceTimersByTimeAsync(500);
    expect(srv.calls.map((c) => c.version)).toEqual([1, 2]);
    expect(srv.state.version).toBe(3);
    expect(srv.state.prefs).toMatchObject({ theme: 'light', ui: 1.3, sbOpen: true });
    // the stores show the merged document
    expect(useUi.getState().uiScalePref).toBe(1.3);
    expect(useUi.getState().sidebarOpen).toBe(true);
    expect(useUi.getState().themePref).toBe('light');
    // and nothing else is pending
    await vi.advanceTimersByTimeAsync(2000);
    expect(srv.calls).toHaveLength(2);
  });

  it('retries once; a second conflict takes the server document as is', async () => {
    let version = 5;
    const put = vi.fn<Put>(() => {
      version += 1;
      return Promise.reject(stale({ version, prefs: { theme: 'dark', ui: 0.9 } }));
    });
    startSync(put, { version: 1, prefs: {} });
    useUi.getState().setTheme('light');
    await vi.advanceTimersByTimeAsync(500);
    expect(put).toHaveBeenCalledTimes(2);
    expect(useUi.getState().themePref).toBe('dark');
    expect(useUi.getState().uiScalePref).toBe(0.9);
    await vi.advanceTimersByTimeAsync(2000);
    expect(put).toHaveBeenCalledTimes(2);
  });

  it('reports other errors and tries again on the next change', async () => {
    const onError = vi.fn();
    const put = vi
      .fn<Put>((v, prefs) => Promise.resolve({ version: v + 1, prefs }))
      .mockRejectedValueOnce(
        new ApiError({ type: '/problems/internal', title: 'Oops', status: 500, code: 'internal' }),
      );
    startSync(put, { version: 0, prefs: {} }, onError);
    useUi.getState().setTheme('light');
    await vi.advanceTimersByTimeAsync(500);
    expect(onError).toHaveBeenCalledTimes(1);
    useUi.getState().setUiScale(1.1);
    await vi.advanceTimersByTimeAsync(500);
    expect(put).toHaveBeenCalledTimes(2);
    expect(put.mock.calls[1]?.[1]).toMatchObject({ theme: 'light', ui: 1.1 });
  });

  it('flush saves at once; stop drops a pending save', async () => {
    const srv = server({ version: 0, prefs: {} });
    const { sync } = startSync(srv.put, srv.state);
    useUi.getState().setTheme('light');
    await sync.flush();
    expect(srv.calls).toHaveLength(1);
    useUi.getState().setTheme('dark');
    sync.stop();
    await vi.advanceTimersByTimeAsync(1000);
    expect(srv.calls).toHaveLength(1);
  });
});

describe('mergeLocal', () => {
  it('overlays only the keys and views that changed locally', () => {
    resetStores();
    const base = collectPrefs();
    useUi.getState().setTheme('light');
    useViews.getState().patch('a1', { period: 'l30' });
    const local = collectPrefs();
    const merged = mergeLocal({ theme: 'dark', ui: 1.3, views: { all: { period: 'ytd' } } }, base, local);
    expect(merged).toMatchObject({ theme: 'light', ui: 1.3 });
    expect(merged['views']).toEqual({ all: { period: 'ytd' }, a1: local.views['a1'] });
  });
});
