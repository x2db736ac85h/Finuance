import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { renderApp, warmUpApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';
import { safeNext } from './next';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
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

  it('asks for a code when the account uses two-step sign-in, and signs in with it', async () => {
    const steps: unknown[] = [];
    server.use(
      http.post(`${API}/auth/login`, () =>
        HttpResponse.json({ challenge: 'challenge-token', expiresAt: '2099-01-01T00:00:00Z' }, { status: 202 }),
      ),
      http.post(`${API}/auth/login/second-step`, async ({ request }) => {
        const body = (await request.json()) as { challenge: string; code: string };
        steps.push(body);
        if (body.code !== '123456') return problem(401, 'unauthenticated', 'The code is not valid.');
        db.signedIn = true;
        db.user = { ...db.user, secondFactor: true };
        return HttpResponse.json(db.user);
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/login?next=/settings/security');
    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'a@b.c');
    await user.type(screen.getByLabelText('Password'), 'the right password');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    // the password alone signs nobody in: the page asks for the code and stays on /login
    const code = await screen.findByRole('textbox', { name: 'Code' });
    expect(screen.getByRole('heading', { name: 'Enter your code' })).toBeTruthy();
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(code.getAttribute('autocomplete')).toBe('one-time-code');
    expect(router.state.location.pathname).toBe('/login');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByText('Enter the code from your authenticator app.')).toBeTruthy();
    await user.type(code, '000000');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect((await screen.findByText('The code is not valid.')).getAttribute('role')).toBe('alert');
    await waitFor(() => {
      expect(code).toHaveProperty('value', '');
    });
    await user.type(code, '123456');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/security');
    });
    expect(steps).toEqual([
      { challenge: 'challenge-token', code: '000000' },
      { challenge: 'challenge-token', code: '123456' },
    ]);
  });

  it('lets someone start again with the password from the code step', async () => {
    server.use(
      http.post(`${API}/auth/login`, () =>
        HttpResponse.json({ challenge: 'c', expiresAt: '2099-01-01T00:00:00Z' }, { status: 202 }),
      ),
    );
    const user = userEvent.setup();
    renderApp('/login');
    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'a@b.c');
    await user.type(screen.getByLabelText('Password'), 'pw pw pw pw');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    await user.click(await screen.findByRole('button', { name: 'Start again with your password' }));
    expect(screen.getByRole('heading', { name: 'Sign in' })).toBeTruthy();
    expect(screen.getByLabelText<HTMLInputElement>('Password').value).toBe('');
  });

  it('links to sign-up only when it is enabled', async () => {
    renderApp('/login');
    expect(await screen.findByRole('link', { name: 'Create an account' })).toBeTruthy();
  });
});

describe('demo', () => {
  it('starts a demo visitor from the sign-in page and shows the demo banner', async () => {
    db.demoMode = true;
    db.signupEnabled = false;
    server.use(
      http.post(`${API}/auth/demo`, () => {
        db.signedIn = true;
        db.user = { ...db.user, email: 'demo-1@demo.invalid', isDemo: true };
        return HttpResponse.json(db.user);
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/login');
    expect(screen.queryByRole('link', { name: 'Create an account' })).toBeNull();
    await user.click(await screen.findByRole('button', { name: 'Try the demo' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/all');
    });
    expect((await screen.findByText('Demo — your data is deleted after 24 hours.')).getAttribute('role')).toBe(
      'status',
    );
  });

  it('offers no demo on a normal instance, and no banner for a normal user', async () => {
    renderApp('/login');
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Try the demo' })).toBeNull();
  });

  it('shows why the demo could not start', async () => {
    db.demoMode = true;
    server.use(
      http.post(`${API}/auth/demo`, () =>
        problem(429, 'rate_limited', 'Too many demos from this address; try again in an hour.'),
      ),
    );
    const user = userEvent.setup();
    renderApp('/login');
    await user.click(await screen.findByRole('button', { name: 'Try the demo' }));
    expect(await screen.findByText('Too many demos from this address; try again in an hour.')).toBeTruthy();
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
