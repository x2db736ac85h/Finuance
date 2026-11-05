import { act, screen, waitFor, within } from '@testing-library/react';
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

  it('is linked from the settings for administrators only, and links back', async () => {
    const user = userEvent.setup();
    const { router } = renderApp('/settings/profile');
    const nav = await screen.findByRole('navigation', { name: 'Settings' });
    await user.click(within(nav).getByRole('link', { name: 'Administration' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/admin/users');
    });
    const adminNav = await screen.findByRole('navigation', { name: 'Administration' });
    await user.click(within(adminNav).getByRole('link', { name: 'Settings' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/appearance');
    });
  });

  it('has no settings link for other users', async () => {
    db.user.isAdmin = false;
    renderApp('/settings/profile');
    const nav = await screen.findByRole('navigation', { name: 'Settings' });
    expect(within(nav).getByRole('link', { name: 'Profile' })).toBeTruthy();
    expect(within(nav).queryByRole('link', { name: 'Administration' })).toBeNull();
  });

  it('shows the Developer section only when the instance has developer mode on', async () => {
    const { router } = renderApp('/admin/developer');
    // off (404): no section, and a direct visit lands on Users
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/admin/users');
    });
    const nav = await screen.findByRole('navigation', { name: 'Administration' });
    expect(within(nav).queryByRole('link', { name: 'Developer' })).toBeNull();
  });

  it('shows the build and the links with how to get in, and copies the command', async () => {
    const info: Schemas['DeveloperInfo'] = {
      build: {
        version: '0.3.0-rc.2',
        commit: '9ae8f60',
        date: '2026-11-05T10:00:00Z',
        goVersion: 'go1.27.3',
        environment: 'prod',
        instance: 'finuance-api-abc',
        schemaVersion: 14,
        expectedSchemaVersion: 14,
      },
      source: {
        url: 'https://github.com/example/finuance',
        commitUrl: 'https://github.com/example/finuance/commit/9ae8f60',
        releaseUrl: 'https://github.com/example/finuance/releases/tag/v0.3.0-rc.2',
      },
      links: [
        {
          group: 'Observability',
          name: 'Grafana',
          url: 'http://localhost:3000',
          note: 'User admin.\nThe second command prints the password.',
          commands: ['kubectl -n monitoring port-forward svc/grafana 3000:80', 'kubectl get secret grafana-admin'],
        },
        { group: 'CI/CD', name: 'Actions', url: 'https://github.com/example/finuance/actions' },
        { group: 'CI/CD', name: 'Odd', url: 'javascript:alert(1)' },
      ],
    };
    server.use(http.get(`${API}/admin/developer`, () => HttpResponse.json(info)));
    const user = userEvent.setup(); // with its own clipboard
    renderApp('/admin/developer');
    await screen.findByRole('heading', { name: 'Developer' });
    expect(screen.getByRole('link', { name: '0.3.0-rc.2' }).getAttribute('href')).toBe(info.source?.releaseUrl);
    expect(screen.getByRole('link', { name: '9ae8f60' }).getAttribute('href')).toBe(info.source?.commitUrl);
    expect(screen.getByText('finuance-api-abc')).toBeTruthy();
    const grafana = screen.getByRole('link', { name: 'Grafana' });
    expect(grafana.getAttribute('target')).toBe('_blank');
    expect(grafana.getAttribute('rel')).toBe('noopener noreferrer');
    // a link the server would never send still doesn't become one
    expect(screen.queryByRole('link', { name: 'Odd' })).toBeNull();
    expect(screen.getByText('Odd')).toBeTruthy();
    // Actions has nothing to explain: no ⓘ
    expect(screen.queryByRole('button', { name: 'How to get into Actions' })).toBeNull();

    const info1 = screen.getByRole('button', { name: 'How to get into Grafana' });
    await user.hover(info1);
    const pop = screen.getByRole('group', { name: 'How to get into Grafana' });
    expect(within(pop).getByText(/prints the password/)).toBeTruthy();
    await user.click(within(pop).getByRole('button', { name: 'Copy command 2 for Grafana' }));
    expect(await navigator.clipboard.readText()).toBe('kubectl get secret grafana-admin');
    await screen.findByText('Copied the command');
    // Escape closes it, keyboard focus opens it again
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('group', { name: 'How to get into Grafana' })).toBeNull();
    act(() => {
      info1.focus();
    });
    expect(screen.getByRole('group', { name: 'How to get into Grafana' })).toBeTruthy();
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('group', { name: 'How to get into Grafana' })).toBeNull();
  });

  it('never asks for the Developer section for anyone but an administrator', async () => {
    db.user.isAdmin = false;
    const asked: string[] = [];
    server.events.on('request:start', ({ request }) => {
      if (request.url.includes('/admin/')) asked.push(request.url);
    });
    const { router } = renderApp('/admin/developer');
    await waitFor(() => {
      expect(router.state.location.pathname).not.toMatch(/^\/admin/);
    });
    await screen.findByRole('navigation', { name: 'Accounts' }).catch(() => undefined);
    expect(asked).toEqual([]);
    server.events.removeAllListeners('request:start');
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
    let stored: Schemas['InstanceSettingsDoc'] = { version: 3, defaults: {}, features: {}, limits: {} };
    server.use(
      http.get(`${API}/admin/settings`, () => HttpResponse.json(stored)),
      http.put(`${API}/admin/settings`, async ({ request }) => {
        put = (await request.json()) as Schemas['InstanceSettingsDoc'];
        stored = { ...put, version: 4 };
        return HttpResponse.json(stored);
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
    // the saved version remounts the form; the confirmation still comes
    expect(await screen.findByText('Saved the instance settings')).toBeTruthy();
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
    expect(await screen.findByText('Saved Sea')).toBeTruthy();
    const approve = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => {
      expect(approve.hasAttribute('disabled')).toBe(false);
    });
    await userEvent.click(approve);
    await waitFor(() => {
      expect(approved).toBe(true);
    });
    // queued behind "Saved Sea" (one toast at a time)
    expect(await screen.findByText(/everyone can pick it now/, {}, { timeout: 4000 })).toBeTruthy();
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
