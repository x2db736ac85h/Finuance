import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { errorName, installClientEventHandlers, reportClientEvent, resetClientEvents, screenOf } from './clientEvents';

describe('screenOf', () => {
  it('keeps the app’s own first segment and hides every value', () => {
    expect(screenOf('/')).toBe('/');
    expect(screenOf('/transactions/all')).toBe('/transactions/:id');
    expect(screenOf('/imports/01a1fa08-83f5-7af9-a0f2-46f412659b52')).toBe('/imports/:id');
    expect(screenOf('/settings/security')).toBe('/settings/:id');
    expect(screenOf('/shared')).toBe('/shared');
  });
  it('sends nothing of a path the app does not define', () => {
    expect(screenOf('/my-secret-search-words/more')).toBe('/*');
  });
});

describe('errorName', () => {
  it('is the class name of an error, and nothing for anything else', () => {
    expect(errorName(new TypeError('the user typed this'))).toBe('TypeError');
    expect(errorName('a string that was thrown')).toBeUndefined();
    const odd = new Error('x');
    odd.name = 'a name with spaces: and text';
    expect(errorName(odd)).toBeUndefined();
  });
});

describe('reportClientEvent', () => {
  type Call = { url: string; init: RequestInit };
  const calls: Call[] = [];
  let fail = false;
  const fetchMock = vi.fn((url: string, init: RequestInit) => {
    calls.push({ url, init });
    return fail ? Promise.reject(new Error('offline')) : Promise.resolve(new Response(null, { status: 204 }));
  });
  const bodyOf = (c: Call) => JSON.parse(c.init.body as string) as Record<string, string>;
  beforeEach(() => {
    resetClientEvents();
    fetchMock.mockClear();
    calls.length = 0;
    fail = false;
    vi.stubGlobal('fetch', fetchMock);
    window.history.replaceState(null, '', '/imports/01a1fa08?q=groceries');
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    window.history.replaceState(null, '', '/');
  });

  it('posts the kind, the class name and the route pattern, and nothing else', () => {
    reportClientEvent('error', new RangeError('balance of 1 234,56 is out of range'));
    expect(calls).toHaveLength(1);
    const call = calls[0];
    if (!call) throw new Error('no request was made');
    expect(call.url).toMatch(/\/api\/v1\/client-events$/);
    expect(call.init.method).toBe('POST');
    expect((call.init.headers as Record<string, string>)['X-Finuance-Client']).toBe('web');
    expect(bodyOf(call)).toEqual({ kind: 'error', name: 'RangeError', route: '/imports/:id' });
    expect(call.init.body as string).not.toMatch(/groceries|1 234|01a1fa08/);
  });

  it('stops after a few reports per page', () => {
    for (let i = 0; i < 20; i++) reportClientEvent('error', new Error('again'));
    expect(fetchMock).toHaveBeenCalledTimes(5);
  });

  it('never throws, also when the request fails', () => {
    fail = true;
    expect(() => {
      reportClientEvent('render', new Error('x'));
    }).not.toThrow();
  });

  it('reports uncaught errors and unhandled rejections from the window', () => {
    installClientEventHandlers();
    window.dispatchEvent(new ErrorEvent('error', { error: new TypeError('boom') }));
    window.dispatchEvent(new ErrorEvent('error', {})); // a resource that failed to load: not reported
    const rejection = new Event('unhandledrejection') as Event & { reason?: unknown };
    rejection.reason = new SyntaxError('bad');
    window.dispatchEvent(rejection);
    expect(calls.map((c) => bodyOf(c).kind)).toEqual(['error', 'unhandled_rejection']);
  });
});
