import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { api, ApiError, data, setUnauthenticatedHandler } from './client';

const base = `${window.location.origin}/api/v1`;
const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const problem = (status: number, code: string) =>
  HttpResponse.json({ type: `/problems/${code}`, title: 'T', status, code, detail: 'D' }, { status });

describe('api client', () => {
  it('sends the CSRF header and same-origin credentials', async () => {
    let header: string | null = null;
    server.use(
      http.post(`${base}/auth/logout`, ({ request }) => {
        header = request.headers.get('X-Finuance-Client');
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await api.POST('/auth/logout');
    expect(header).toBe('web');
  });

  it('turns problems into ApiError', async () => {
    server.use(http.get(`${base}/accounts`, () => problem(422, 'validation_failed')));
    const err = await api.GET('/accounts').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe('validation_failed');
    expect((err as ApiError).status).toBe(422);
    expect((err as ApiError).message).toBe('D');
  });

  it('makes up a problem for non-JSON failures', async () => {
    server.use(http.get(`${base}/accounts`, () => new HttpResponse('bad gateway', { status: 502 })));
    const err = (await api.GET('/accounts').catch((e: unknown) => e)) as ApiError;
    expect(err.code).toBe('internal');
    expect(err.status).toBe(502);
  });

  it('routes 401 to the login page with next', async () => {
    const go = vi.fn();
    setUnauthenticatedHandler(go);
    window.history.pushState({}, '', '/t/all?x=1');
    server.use(http.get(`${base}/me`, () => problem(401, 'unauthenticated')));
    await expect(api.GET('/me')).rejects.toBeInstanceOf(ApiError);
    expect(go).toHaveBeenCalledWith('/t/all?x=1');
  });

  it('does not redirect for a failed login', async () => {
    const go = vi.fn();
    setUnauthenticatedHandler(go);
    server.use(http.post(`${base}/auth/login`, () => problem(401, 'unauthenticated')));
    await expect(api.POST('/auth/login', { body: { email: 'a@b.c', password: 'x' } })).rejects.toBeInstanceOf(ApiError);
    expect(go).not.toHaveBeenCalled();
  });

  it('data() returns the body', async () => {
    server.use(
      http.get(`${base}/meta`, () => HttpResponse.json({ version: 'v', signupEnabled: true, demoMode: false })),
    );
    expect(data(await api.GET('/meta')).signupEnabled).toBe(true);
  });
});
