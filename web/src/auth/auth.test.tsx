import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { renderApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';
import { safeNext } from './next';

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
beforeEach(() => {
  resetDb();
  db.signedIn = false;
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

describe('login', () => {
  it('shows the server message for wrong credentials', async () => {
    server.use(http.post(`${API}/auth/login`, () => problem(401, 'unauthenticated', 'Wrong email or password.')));
    const user = userEvent.setup();
    const { router } = renderApp('/login');
    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'a@b.c');
    await user.type(screen.getByLabelText('Password'), 'nope');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect((await screen.findByText('Wrong email or password.')).getAttribute('role')).toBe('alert');
    expect(router.state.location.pathname).toBe('/login');
  });

  it('asks for both fields before sending', async () => {
    const user = userEvent.setup();
    renderApp('/login');
    await user.click(await screen.findByRole('button', { name: 'Sign in' }));
    expect(screen.getByText('Enter your email and password.')).toBeTruthy();
  });

  it('links to sign-up only when it is enabled', async () => {
    renderApp('/login');
    expect(await screen.findByRole('link', { name: 'Create an account' })).toBeTruthy();
  });
});

describe('signup', () => {
  it('checks the password length, then creates the account and opens the app', async () => {
    let body: unknown;
    server.use(
      http.post(`${API}/auth/signup`, async ({ request }) => {
        body = await request.json();
        db.signedIn = true;
        return HttpResponse.json(db.user, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/signup');
    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'new@example.com');
    await user.type(screen.getByLabelText('Password'), 'short');
    await user.click(screen.getByRole('button', { name: 'Create account' }));
    expect(screen.getByText('Use a password of at least 12 characters.')).toBeTruthy();
    await user.type(screen.getByLabelText('Password'), ' but longer now');
    await user.click(screen.getByRole('button', { name: 'Create account' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/all');
    });
    expect(body).toEqual({ email: 'new@example.com', password: 'short but longer now' });
    expect(await screen.findByRole('heading', { name: 'Drop CSV files here' })).toBeTruthy();
  });

  it('explains when sign-up is turned off', async () => {
    db.signupEnabled = false;
    renderApp('/signup');
    expect(await screen.findByRole('heading', { name: 'Sign-up is turned off' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Create account' })).toBeNull();
  });
});

describe('safeNext', () => {
  it('only follows same-origin app paths', () => {
    expect(safeNext('/settings/profile')).toBe('/settings/profile');
    expect(safeNext('/t/all?x=1')).toBe('/t/all?x=1');
    expect(safeNext(null)).toBe('/');
    expect(safeNext('https://evil.example')).toBe('/');
    expect(safeNext('//evil.example')).toBe('/');
    expect(safeNext('/\\evil.example')).toBe('/');
    expect(safeNext('/login?next=/x')).toBe('/');
  });
});
