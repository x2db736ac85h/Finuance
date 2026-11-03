import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { BUILTIN_THEMES } from '../styles/themes';
import { renderApp, warmUpApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';
import { parseLimits } from './Users';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
  db.user.isAdmin = true;
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const usage = { accounts: 1, transactions: 917, imports: 1, storedBytes: 150_000, chats: 0 };
const adminUser = (over: Partial<Schemas['AdminUser']> = {}): Schemas['AdminUser'] => ({
  id: '00000000-0000-4000-8000-0000000000a1',
  email: 'sam@example.com',
  displayName: 'Sam',
  isDemo: false,
  isAdmin: false,
  disabled: false,
  mustChangePassword: false,
  createdAt: '2026-11-01T10:00:00Z',
  limits: {},
  usage,
  ...over,
});
const totals = { users: 2, admins: 1, disabled: 0, demo: 0, active7: 1, active30: 2 };

describe('administration', () => {
  it('is only for administrators', async () => {
    db.user.isAdmin = false;
    const { router } = renderApp('/admin/users');
    await waitFor(() => {
      expect(router.state.location.pathname).not.toMatch(/^\/admin/);
    });
  });

  it('lists accounts with counts, creates one with a one-time password', async () => {
    let created: Schemas['AdminCreateUserRequest'] | undefined;
    const sam = adminUser();
    server.use(
      http.get(`${API}/admin/users`, () => HttpResponse.json({ users: [sam], totals })),
      http.get(`${API}/admin/users/:id`, () => HttpResponse.json(sam)),
      http.post(`${API}/admin/users`, async ({ request }) => {
        created = (await request.json()) as Schemas['AdminCreateUserRequest'];
        return HttpResponse.json(
          {
            user: adminUser({
              id: '00000000-0000-4000-8000-0000000000a2',
              email: created.email,
              mustChangePassword: true,
            }),
            oneTimePassword: 'abcd-efgh-jkmn-pqrs',
          },
          { status: 201 },
        );
      }),
    );
    renderApp('/admin/users');
    const row = await screen.findByRole('button', { name: 'sam@example.com' });
    expect(screen.getByText('917')).toBeTruthy();
    expect(screen.getByText(/2 accounts · 1 administrators/)).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('E-mail'), 'new@example.com');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create account' }));
    expect(await within(dialog).findByText('abcd-efgh-jkmn-pqrs')).toBeTruthy();
    expect(created).toEqual({ email: 'new@example.com', displayName: '', admin: false });
    expect(row).toBeTruthy();
  });

  it('acts on an account: limits, disable, delete with the e-mail', async () => {
    const patches: Schemas['AdminUpdateUserRequest'][] = [];
    let deleted: string | undefined;
    let sam = adminUser();
    server.use(
      http.get(`${API}/admin/users`, () => HttpResponse.json({ users: [sam], totals })),
      http.get(`${API}/admin/users/:id`, () => HttpResponse.json(sam)),
      http.patch(`${API}/admin/users/:id`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminUpdateUserRequest'];
        patches.push(body);
        sam = {
          ...sam,
          ...(body.disabled !== undefined ? { disabled: body.disabled } : {}),
          ...(body.limits ? { limits: body.limits } : {}),
        };
        return HttpResponse.json(sam);
      }),
      http.delete(`${API}/admin/users/:id`, async ({ request }) => {
        deleted = ((await request.json()) as { confirmEmail: string }).confirmEmail;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp('/admin/users');
    await userEvent.click(await screen.findByRole('button', { name: 'sam@example.com' }));
    const card = await screen.findByLabelText('Account sam@example.com');
    await userEvent.type(within(card).getByLabelText(/Accounts/), '2');
    await userEvent.type(within(card).getByLabelText(/Largest file/), '5');
    await userEvent.click(within(card).getByRole('button', { name: 'Save limits' }));
    await waitFor(() => {
      expect(patches).toContainEqual({ limits: { maxAccounts: 2, maxUploadBytes: 5 << 20 } });
    });
    await userEvent.click(within(card).getByRole('button', { name: 'Disable account' }));
    await waitFor(() => {
      expect(patches).toContainEqual({ disabled: true });
    });
    await userEvent.click(within(card).getByRole('button', { name: 'Delete account' }));
    const dialog = await screen.findByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Delete account' });
    expect(confirm.hasAttribute('disabled')).toBe(true);
    await userEvent.type(within(dialog).getByLabelText('E-mail address to confirm'), 'SAM@example.com');
    await userEvent.click(confirm);
    await waitFor(() => {
      expect(deleted).toBe('SAM@example.com');
    });
  });

  it('saves the instance defaults and switches', async () => {
    let put: Schemas['InstanceSettingsDoc'] | undefined;
    server.use(
      http.get(`${API}/admin/settings`, () =>
        HttpResponse.json({ version: 3, defaults: {}, features: {}, limits: {} }),
      ),
      http.put(`${API}/admin/settings`, async ({ request }) => {
        put = (await request.json()) as Schemas['InstanceSettingsDoc'];
        return HttpResponse.json({ ...put, version: 4 });
      }),
    );
    renderApp('/admin/instance');
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Daily spending' }));
    await userEvent.click(screen.getByRole('checkbox', { name: /Overview/ }));
    await userEvent.type(screen.getAllByLabelText(/Accounts/)[0] ?? document.body, '3');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(put).toEqual({
        version: 3,
        defaults: { cards: ['sum', 'cat', 'flow', 'pace', 'payee'] },
        features: { overview: false },
        limits: { maxAccounts: 3 },
      });
    });
  });

  it('edits a theme: approval waits for every pair to pass', async () => {
    const base = BUILTIN_THEMES[0];
    if (!base) throw new Error('no built-in theme');
    let theme: Schemas['Theme'] = {
      id: '00000000-0000-4000-8000-0000000000b1',
      slug: 'sea',
      name: 'Sea',
      basedOn: 'indigo',
      tokens: structuredClone(base.tokens),
      status: 'draft',
      version: 0,
      updatedAt: '2026-11-03T10:00:00Z',
    };
    let approved = false;
    server.use(
      http.get(`${API}/admin/themes`, () => HttpResponse.json([theme])),
      http.put(`${API}/admin/themes/:id`, async ({ request }) => {
        const body = (await request.json()) as Schemas['ThemeUpdateRequest'];
        theme = { ...theme, name: body.name, tokens: body.tokens, version: theme.version + 1 };
        return HttpResponse.json(theme);
      }),
      http.put(`${API}/admin/themes/:id/status`, () => {
        approved = true;
        theme = { ...theme, status: 'approved', version: theme.version + 1 };
        return HttpResponse.json(theme);
      }),
    );
    renderApp('/admin/themes');
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const muted = await screen.findByRole('textbox', { name: 'Secondary text, light' });
    await userEvent.clear(muted);
    await userEvent.type(muted, '#bbbbbb');
    expect(await screen.findByRole('table', { name: 'Colour pairs below 4.5:1' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Approve' }).hasAttribute('disabled')).toBe(true);
    await userEvent.clear(muted);
    await userEvent.type(muted, '#555555');
    expect(screen.getByText(/Every text colour reaches 4.5:1/)).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(theme.tokens.light['muted']).toBe('#555555');
    });
    const approve = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => {
      expect(approve.hasAttribute('disabled')).toBe(false);
    });
    await userEvent.click(approve);
    await waitFor(() => {
      expect(approved).toBe(true);
    });
  });

  it('shows a limit as a sentence', async () => {
    db.user.isAdmin = false;
    server.use(
      http.post(`${API}/accounts`, () =>
        problem(422, 'limit_reached', 'limit maxAccounts (1) reached', { limit: { name: 'maxAccounts', value: 1 } }),
      ),
    );
    const { errorMessage } = await import('../lib/errors');
    const { api } = await import('../api/client');
    const e = await api.POST('/accounts', { body: { name: 'Second', currency: 'PLN' } }).catch((x: unknown) => x);
    expect(errorMessage(e)).toBe('Your account allows 1 account. Ask an administrator if you need more.');
  });
});

describe('parseLimits', () => {
  it('reads megabytes and refuses bad numbers', () => {
    expect(parseLimits({ maxUploadBytes: '2,5', maxAccounts: ' 3 ', maxGroups: '' })).toEqual({
      maxUploadBytes: Math.round(2.5 * (1 << 20)),
      maxAccounts: 3,
    });
    expect(parseLimits({ maxAccounts: '-1' })).toMatch(/Accounts: enter a positive number/);
  });
});

describe('one-time passwords and switched-off features', () => {
  it('asks for a new password before anything else', async () => {
    db.user.isAdmin = false;
    db.user.mustChangePassword = true;
    let changed: Schemas['ChangePasswordRequest'] | undefined;
    server.use(
      http.post(`${API}/me/password`, async ({ request }) => {
        changed = (await request.json()) as Schemas['ChangePasswordRequest'];
        db.user.mustChangePassword = false;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp('/t/all');
    expect(await screen.findByRole('heading', { name: 'Choose a new password' })).toBeTruthy();
    await userEvent.type(screen.getByLabelText('One-time password'), 'abcd-efgh-jkmn-pqrs');
    await userEvent.type(screen.getByLabelText('New password'), 'my own password now');
    await userEvent.type(screen.getByLabelText('Repeat the new password'), 'my own password now');
    await userEvent.click(screen.getByRole('button', { name: 'Save and continue' }));
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: 'Choose a new password' })).toBeNull();
    });
    expect(changed).toEqual({ currentPassword: 'abcd-efgh-jkmn-pqrs', newPassword: 'my own password now' });
  });

  it('hides the overview and the assistant when they are off', async () => {
    db.user.isAdmin = false;
    db.features = { overview: false, assistant: false, friends: true, sharedGroups: true };
    const { router } = renderApp('/o/all');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/all');
    });
    expect(screen.queryByRole('button', { name: 'Personal finance overview' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Assistant/ })).toBeNull();
  });

  it('starts a new user from the instance defaults', async () => {
    db.user.isAdmin = false;
    db.defaults = { themeId: 'grove', noViz: true };
    renderApp('/t/all');
    await waitFor(() => {
      expect(document.documentElement.dataset['themeId']).toBe('grove');
    });
    const { useUi } = await import('../stores/uiStore');
    expect(useUi.getState().vizOpen).toBe(false);
  });
});
