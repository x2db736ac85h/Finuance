import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { renderApp } from '../test/render';
import { API, resetDb, server } from '../test/server';

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});
beforeEach(() => {
  resetDb();
});

describe('access tokens (ADR 0036)', () => {
  it('makes a token shown once, lists it by its prefix and revokes it', async () => {
    let tokens: Schemas['AccessToken'][] = [];
    const made: Schemas['AccessTokenRequest'][] = [];
    const revoked: string[] = [];
    server.use(
      http.get(`${API}/me/tokens`, () => HttpResponse.json(tokens)),
      http.post(`${API}/me/tokens`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AccessTokenRequest'];
        made.push(body);
        const token: Schemas['AccessToken'] = {
          id: '0b000000-0000-4000-8000-000000000001',
          name: body.name,
          prefix: 'fin_AbCd1234',
          scope: body.scope,
          createdAt: '2026-11-06T10:00:00Z',
          expiresAt: '2099-01-01T10:00:00Z',
        };
        tokens = [token];
        return HttpResponse.json({ token, secret: 'fin_AbCd1234restofthesecret' }, { status: 201 });
      }),
      http.delete(`${API}/me/tokens/:id`, ({ params }) => {
        revoked.push(String(params['id']));
        tokens = tokens.map((t) => ({ ...t, revokedAt: '2026-11-06T11:00:00Z' }));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp('/settings/tokens');
    const form = await screen.findByRole('form', { name: 'New access token' });
    expect(await screen.findByText('None yet.')).toBeTruthy();
    fireEvent.change(within(form).getByLabelText('Name'), { target: { value: 'laptop CLI' } });
    fireEvent.change(within(form).getByLabelText('Allows'), { target: { value: 'write' } });
    fireEvent.change(within(form).getByLabelText('Valid for'), { target: { value: '30' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Make a token' }));
    expect((await screen.findByLabelText('The new token')).textContent).toBe('fin_AbCd1234restofthesecret');
    expect(made).toEqual([{ name: 'laptop CLI', scope: 'write', expiresInDays: 30 }]);
    expect(await screen.findByText('fin_AbCd1234…')).toBeTruthy();
    expect(screen.getByText('read and write')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await waitFor(() => {
      expect(revoked).toEqual(['0b000000-0000-4000-8000-000000000001']);
    });
    expect(await screen.findByText(/revoked/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull();
  });
});
