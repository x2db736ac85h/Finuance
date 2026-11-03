import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { renderApp, warmUpApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';
import { browserName } from './Sessions';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const FF = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:131.0) Gecko/20100101 Firefox/131.0';
const CHROME = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36';

describe('settings', () => {
  it('lists sessions and signs out another browser', async () => {
    db.sessions = [
      {
        id: 'aaaaaaaaaaaaaaaa',
        createdAt: '2026-10-20T08:00:00Z',
        lastSeenAt: '2026-10-21T08:00:00Z',
        expiresAt: '2026-11-20T08:00:00Z',
        userAgent: FF,
        current: true,
      },
      {
        id: 'bbbbbbbbbbbbbbbb',
        createdAt: '2026-10-19T08:00:00Z',
        lastSeenAt: '2026-10-19T09:00:00Z',
        expiresAt: '2026-11-19T08:00:00Z',
        userAgent: CHROME,
        ip: '192.0.2.4',
        current: false,
      },
    ];
    let revoked = '';
    server.use(
      http.delete(`${API}/me/sessions/:id`, ({ params }) => {
        revoked = String(params['id']);
        db.sessions = db.sessions.filter((s) => s.id !== revoked);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/sessions');
    const list = await screen.findByRole('list');
    await within(list).findByText('Chrome on Linux');
    expect(within(list).getByText('this browser')).toBeTruthy();
    expect(within(list).getAllByRole('button', { name: 'Sign out' })).toHaveLength(1);
    await user.click(within(list).getByRole('button', { name: 'Sign out' }));
    await waitFor(() => {
      expect(within(list).queryByText('Chrome on Linux')).toBeNull();
    });
    expect(revoked).toBe('bbbbbbbbbbbbbbbb');
  });

  it('checks that the new passwords match before sending', async () => {
    const user = userEvent.setup();
    renderApp('/settings/profile');
    await user.type(await screen.findByLabelText('Current password'), 'old password 123');
    await user.type(screen.getByLabelText('New password'), 'a new password 1');
    await user.type(screen.getByLabelText('Repeat the new password'), 'a new password 2');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(screen.getByText("The new passwords don't match. Type the same password twice.")).toBeTruthy();
  });

  it('shows the server message for a wrong current password', async () => {
    server.use(
      http.post(`${API}/me/password`, () => problem(422, 'validation_failed', 'The current password is wrong.')),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/settings/profile');
    await user.type(await screen.findByLabelText('Current password'), 'wrong password 1');
    await user.type(screen.getByLabelText('New password'), 'a new password 1');
    await user.type(screen.getByLabelText('Repeat the new password'), 'a new password 1');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(await screen.findByText('The current password is wrong.')).toBeTruthy();
    expect(router.state.location.pathname).toBe('/settings/profile');
  });

  it('saves the display name', async () => {
    server.use(
      http.patch(`${API}/me`, async ({ request }) => {
        const { displayName } = (await request.json()) as { displayName: string };
        db.user = { ...db.user, displayName };
        return HttpResponse.json(db.user);
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/profile');
    const name = await screen.findByLabelText('Name');
    await user.clear(name);
    await user.type(name, 'Alex K.');
    await user.click(screen.getByRole('button', { name: 'Save name' }));
    expect(await screen.findByText('Saved your name')).toBeTruthy();
    expect(db.user.displayName).toBe('Alex K.');
  });

  it('deletes the account after the password and returns to sign-in', async () => {
    let password = '';
    server.use(
      http.delete(`${API}/me`, async ({ request }) => {
        password = ((await request.json()) as { password: string }).password;
        db.signedIn = false;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/settings/data');
    expect((await screen.findByRole('link', { name: 'Download export' })).getAttribute('href')).toBe(
      '/api/v1/me/export',
    );
    await user.click(screen.getByRole('button', { name: 'Delete account…' }));
    const dlg = screen.getByRole('dialog', { name: 'Delete account' });
    await user.type(within(dlg).getByLabelText('Password'), 'my password 123');
    await user.click(within(dlg).getByRole('button', { name: 'Delete account' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login');
    });
    expect(password).toBe('my password 123');
  });

  it('sends unknown sections to appearance', async () => {
    const { router } = renderApp('/settings/nope');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/appearance');
    });
    expect(await screen.findByRole('button', { name: 'System' })).toBeTruthy();
  });
});

describe('browserName', () => {
  it('names common browsers', () => {
    expect(browserName(FF)).toBe('Firefox on macOS');
    expect(browserName(CHROME)).toBe('Chrome on Linux');
    expect(browserName('')).toBe('Unknown browser');
    expect(browserName('curl/8.0')).toBe('curl/8.0');
  });
});
