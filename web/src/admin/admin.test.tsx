import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
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

const usage = {
  accounts: 1,
  transactions: 917,
  imports: 1,
  storedBytes: 150_000,
  chats: 0,
  assistantRuns: 12,
  assistantTokens: 34_500,
};
const adminUser = (over: Partial<Schemas['AdminUser']> = {}): Schemas['AdminUser'] => ({
  id: '00000000-0000-4000-8000-0000000000a1',
  email: 'sam@example.com',
  displayName: 'Sam',
  isDemo: false,
  isSynthetic: false,
  isAdmin: false,
  disabled: false,
  mustChangePassword: false,
  secondFactor: false,
  createdAt: '2026-11-01T10:00:00Z',
  limits: {},
  usage,
  ...over,
});
const totals = { users: 2, admins: 1, disabled: 0, demo: 0, synthetic: 0, active7: 1, active30: 2 };

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
      expect(router.state.location.pathname).toBe('/settings/general');
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

  it('marks the traffic simulator’s users in the list (ADR 0041)', async () => {
    const robot = adminUser({
      id: '00000000-0000-4000-8000-0000000000a3',
      email: 'otter@example.com',
      isSynthetic: true,
    });
    server.use(http.get(`${API}/admin/users`, () => HttpResponse.json({ users: [adminUser(), robot], totals })));
    renderApp('/admin/users');
    const row = (await screen.findByRole('button', { name: 'otter@example.com' })).closest('tr');
    expect(row && within(row).getByText('Synthetic')).toBeTruthy();
    const person = screen.getByRole('button', { name: 'sam@example.com' }).closest('tr');
    expect(person && within(person).queryByText('Synthetic')).toBeNull();
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

  // an account that uses two-step sign-in, with the calls the panel makes for it
  function roleServer() {
    const patches: Schemas['AdminUpdateUserRequest'][] = [];
    const removed: string[] = [];
    let sam = adminUser({ secondFactor: true });
    server.use(
      http.get(`${API}/admin/users`, () => HttpResponse.json({ users: [sam], totals })),
      http.get(`${API}/admin/users/:id`, () => HttpResponse.json(sam)),
      http.patch(`${API}/admin/users/:id`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminUpdateUserRequest'];
        patches.push(body);
        sam = { ...sam, ...(body.admin !== undefined ? { isAdmin: body.admin } : {}) };
        return HttpResponse.json(sam);
      }),
      http.delete(`${API}/admin/users/:id/second-factor`, ({ params }) => {
        removed.push(String(params['id']));
        sam = { ...sam, secondFactor: false };
        return new HttpResponse(null, { status: 204 });
      }),
    );
    return { patches, removed };
  }

  it("changes a role in one click and removes an account's two-step sign-in", async () => {
    const { patches, removed } = roleServer();
    renderApp('/admin/users');
    expect(await screen.findByText('active · two-step', {}, { timeout: 5000 })).toBeTruthy(); // who uses it
    await userEvent.click(screen.getByRole('button', { name: 'sam@example.com' }));
    const card = await screen.findByLabelText('Account sam@example.com');
    await userEvent.click(within(card).getByRole('button', { name: 'Make administrator' }));
    await waitFor(() => {
      expect(patches).toEqual([{ admin: true }]);
    });
    await userEvent.click(within(card).getByRole('button', { name: 'Remove two-step sign-in' }));
    await waitFor(() => {
      expect(removed).toEqual(['00000000-0000-4000-8000-0000000000a1']);
    });
  });

  it('asks for the password before a role change where the instance wants it confirmed', async () => {
    db.confirmPassword = true;
    const { patches } = roleServer();
    renderApp('/admin/users');
    await userEvent.click(await screen.findByRole('button', { name: 'sam@example.com' }, { timeout: 5000 }));
    const card = await screen.findByLabelText('Account sam@example.com');
    await userEvent.click(within(card).getByRole('button', { name: 'Make administrator' }));
    const form = await within(card).findByRole('form', { name: 'Confirm the role change' });
    expect(within(form).getByRole<HTMLButtonElement>('button', { name: 'Confirm' }).disabled).toBe(true);
    expect(patches).toHaveLength(0); // nothing is sent until the password is typed
    await userEvent.type(
      within(form).getByLabelText(/Your password, to make this account an administrator/),
      'my password',
    );
    await userEvent.click(within(form).getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(patches).toEqual([{ admin: true, password: 'my password' }]);
    });
    expect(within(card).queryByRole('form', { name: 'Confirm the role change' })).toBeNull();
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
    await userEvent.click(screen.getByRole('checkbox', { name: /Assistant digests/ }));
    await userEvent.type(screen.getAllByLabelText(/Accounts/)[0] ?? document.body, '3');
    await userEvent.type(screen.getByLabelText(/Assistant tokens/), '50000');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(put).toEqual({
        version: 3,
        defaults: { cards: ['sum', 'cat', 'flow', 'pace', 'payee'] },
        features: { overview: false, assistantDigests: false },
        limits: { maxAccounts: 3, assistantTokensPerDay: 50000 },
        security: { adminsNeedSecondFactor: false },
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

  it('saves the instructions and writes, publishes and deletes advice articles (ADR 0034)', async () => {
    let stored: Schemas['InstanceSettingsDoc'] = {
      version: 3,
      defaults: {},
      features: {},
      limits: {},
      assistant: { instructions: 'Be brief.' },
    };
    let articles: Schemas['AdviceArticle'][] = [];
    const puts: Schemas['InstanceSettingsDoc'][] = [];
    const sent: unknown[] = [];
    let starter = 0;
    const article = (over: Partial<Schemas['AdviceArticle']>): Schemas['AdviceArticle'] => ({
      id: '0a000000-0000-4000-8000-000000000001',
      slug: 'emergency-fund',
      language: 'en',
      title: 'Building an emergency fund',
      body: 'Keep **three months** aside.',
      bodyLength: 28,
      tags: ['saving'],
      status: 'draft',
      version: 1,
      createdAt: '2026-11-06T10:00:00Z',
      updatedAt: '2026-11-06T10:00:00Z',
      ...over,
    });
    server.use(
      http.get(`${API}/admin/settings`, () => HttpResponse.json(stored)),
      http.put(`${API}/admin/settings`, async ({ request }) => {
        const put = (await request.json()) as Schemas['InstanceSettingsDoc'];
        puts.push(put);
        stored = { ...put, version: stored.version + 1 };
        return HttpResponse.json(stored);
      }),
      http.get(`${API}/admin/advice`, () => HttpResponse.json(articles.map((a) => ({ ...a, body: '' })))),
      http.get(`${API}/admin/advice/:id`, ({ params }) =>
        HttpResponse.json(articles.find((a) => a.id === params['id'])),
      ),
      http.post(`${API}/admin/advice`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdviceInput'];
        sent.push(body);
        articles = [article({ ...body })];
        return HttpResponse.json(articles[0], { status: 201 });
      }),
      http.put(`${API}/admin/advice/:id`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdviceUpdate'];
        sent.push(body);
        articles = [article({ ...body, version: body.version + 1 })];
        return HttpResponse.json(articles[0]);
      }),
      http.delete(`${API}/admin/advice/:id`, () => {
        articles = [];
        return new HttpResponse(null, { status: 204 });
      }),
      http.post(`${API}/admin/advice/starter`, () => {
        starter++;
        return HttpResponse.json({ added: 8 });
      }),
    );
    renderApp('/admin/assistant');
    const box = await screen.findByRole('textbox', { name: 'Instructions for the assistant' });
    expect((box as HTMLTextAreaElement).value).toBe('Be brief.');
    fireEvent.change(box, { target: { value: 'Be brief. Answer in English.' } });
    // the instructions card's Save ("Who picks the model" has its own above it)
    await userEvent.click(within(box.closest('.set-card') as HTMLElement).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(puts.at(-1)?.assistant).toEqual({ instructions: 'Be brief. Answer in English.' });
    });
    expect(puts.at(-1)?.version).toBe(3);

    await userEvent.click(screen.getByRole('button', { name: 'Add the starter articles' }));
    await waitFor(() => {
      expect(starter).toBe(1);
    });

    await userEvent.click(screen.getByRole('button', { name: 'New article' }));
    const form = await screen.findByRole('form', { name: 'Article' });
    fireEvent.change(within(form).getByLabelText('Title'), { target: { value: 'Building an emergency fund' } });
    fireEvent.change(within(form).getByLabelText(/^Tags/), { target: { value: 'saving, goals' } });
    fireEvent.change(within(form).getByRole('textbox', { name: 'Text (markdown)' }), {
      target: { value: 'Keep **three months** aside.' },
    });
    await userEvent.click(within(form).getByRole('button', { name: 'Preview' }));
    expect(within(form).getByText('three months').tagName).toBe('B');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent[0]).toEqual({
        slug: 'building-an-emergency-fund',
        language: 'en',
        title: 'Building an emergency fund',
        body: 'Keep **three months** aside.',
        status: 'draft',
        tags: ['saving', 'goals'],
      });
    });
    expect(await screen.findByText('Draft')).toBeTruthy();

    // publish it
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const edit = await screen.findByRole('form', { name: 'Article' });
    await userEvent.selectOptions(within(edit).getByLabelText('Status'), 'published');
    await userEvent.click(within(edit).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent[1]).toMatchObject({ version: 1, status: 'published', slug: 'building-an-emergency-fund' });
    });
    expect(await screen.findByText('Published')).toBeTruthy();

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await userEvent.click(
      within(await screen.findByRole('form', { name: 'Article' })).getByRole('button', { name: 'Delete' }),
    );
    expect(await screen.findByText('No articles yet.')).toBeTruthy();
  });

  it('decides who picks the model, and switches a connection, its models and idle time; loads and unloads on Ollama', async () => {
    const ctl = { enabled: true, unloadAfterMinutes: 30, defaultModel: '', photoModel: '', version: 0 };
    const conns: Schemas['AdminConnection'][] = [
      {
        name: 'homelab',
        label: 'Homelab Ollama',
        api: 'openai',
        baseUrl: 'http://10.0.0.5:11434/v1',
        cloud: false,
        defaultModel: 'qwen3:4b',
        models: ['qwen3*'],
        contextTokens: 8192,
        source: 'file',
        hasKey: false,
        keyUnreadable: false,
        version: 0,
        controls: ctl,
      },
    ];
    let srv: Schemas['AdminModelServer'] = {
      server: 'ollama',
      reachable: true,
      latencyMs: 42,
      canManage: true,
      models: [
        {
          id: 'qwen3:4b',
          offered: true,
          tools: true,
          vision: false,
          loaded: true,
          vramBytes: 3_400_000_000,
          unloadsAt: '2026-11-08T13:00:00Z',
        },
        { id: 'qwen3-vl:4b', offered: true, tools: true, vision: true, loaded: false },
      ],
    };
    const settingsPuts: Schemas['InstanceSettingsDoc'][] = [];
    const controlPuts: Schemas['AdminConnectionControlsInput'][] = [];
    const actions: string[] = [];
    server.use(
      http.get(`${API}/admin/settings`, () =>
        HttpResponse.json({
          version: 3,
          defaults: {},
          features: {},
          limits: {},
          assistant: { instructions: 'Be brief.' },
        } satisfies Schemas['InstanceSettingsDoc']),
      ),
      http.put(`${API}/admin/settings`, async ({ request }) => {
        const body = (await request.json()) as Schemas['InstanceSettingsDoc'];
        settingsPuts.push(body);
        return HttpResponse.json({ ...body, version: 4 });
      }),
      http.get(`${API}/admin/advice`, () => HttpResponse.json([])),
      http.get(`${API}/admin/assistant/tuning`, () => HttpResponse.json([])),
      http.get(`${API}/admin/assistant/connections`, () => HttpResponse.json(conns)),
      http.get(`${API}/admin/assistant/connections/homelab/models`, () => HttpResponse.json(srv)),
      http.put(`${API}/admin/assistant/connections/homelab/controls`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminConnectionControlsInput'];
        controlPuts.push(body);
        if (conns[0]) conns[0] = { ...conns[0], controls: { ...body, version: body.version + 1 } };
        return HttpResponse.json(conns[0]);
      }),
      http.post(`${API}/admin/assistant/connections/homelab/unload`, async ({ request }) => {
        const { model } = (await request.json()) as Schemas['AdminModelAction'];
        actions.push(`unload ${model}`);
        srv = { ...srv, models: srv.models.map((m) => (m.id === model ? { ...m, loaded: false } : m)) };
        return HttpResponse.json(srv);
      }),
    );
    renderApp('/admin/assistant');

    // who picks: the administrator unless saved otherwise; saving keeps the instructions
    const who = await screen.findByLabelText('Who picks');
    expect((who as HTMLSelectElement).value).toBe('admin');
    fireEvent.change(who, { target: { value: 'people' } });
    fireEvent.change(screen.getByLabelText('Default connection'), { target: { value: 'homelab' } });
    const choiceCard = who.closest('.set-card') as HTMLElement;
    await userEvent.click(within(choiceCard).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(settingsPuts[0]?.assistant).toEqual({
        instructions: 'Be brief.',
        modelChoice: 'people',
        defaultConnection: 'homelab',
      });
    });

    // a connection's models and use
    await userEvent.click(screen.getByRole('button', { name: 'Models' }));
    const form = await screen.findByRole('form', { name: 'Models and use of Homelab Ollama' });
    expect(await within(form).findByText(/Connected to Ollama · 2 models · 42 ms/)).toBeTruthy();
    expect(within(form).getByText(/3.4 GB on the GPU|3.2 GiB on the GPU|on the GPU/)).toBeTruthy();
    await userEvent.click(within(form).getByRole('button', { name: 'Unload' }));
    await waitFor(() => {
      expect(actions).toEqual(['unload qwen3:4b']);
    });
    expect(await within(form).findAllByRole('button', { name: 'Load' })).toHaveLength(2);
    fireEvent.change(within(form).getByLabelText('Model for photos'), { target: { value: 'qwen3-vl:4b' } });
    fireEvent.change(within(form).getByLabelText('Unload a model after'), { target: { value: '10' } });
    await userEvent.click(within(form).getByRole('checkbox', { name: /^On/ }));
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(controlPuts[0]).toEqual({
        enabled: false,
        unloadAfterMinutes: 10,
        defaultModel: '',
        photoModel: 'qwen3-vl:4b',
        version: 0,
      });
    });
    expect(await screen.findByText('off')).toBeTruthy();
    // the save is confirmed (the form isn't remounted by the list coming back with a new version)
    // (it waits behind the "Unloaded" toast)
    expect(
      await screen.findByText('Saved Homelab Ollama: switched off, its models unloaded', {}, { timeout: 5000 }),
    ).toBeTruthy();
  });

  it('adds a connection for a provider with its key alone, and keeps models and the dialog apart (U12)', async () => {
    const ctl = { enabled: true, unloadAfterMinutes: null, defaultModel: '', photoModel: '', version: 0 };
    let conns: Schemas['AdminConnection'][] = [
      {
        name: 'anthropic',
        label: 'Claude (file)',
        api: 'anthropic',
        baseUrl: 'https://api.anthropic.com',
        cloud: true,
        defaultModel: '',
        models: [],
        contextTokens: 0,
        source: 'file',
        hasKey: true,
        keyUnreadable: false,
        version: 0,
        controls: ctl,
      },
    ];
    const posted: Schemas['AdminConnectionInput'][] = [];
    server.use(
      http.get(`${API}/admin/settings`, () =>
        HttpResponse.json({
          version: 1,
          defaults: {},
          features: {},
          limits: {},
        } satisfies Schemas['InstanceSettingsDoc']),
      ),
      http.get(`${API}/admin/advice`, () => HttpResponse.json([])),
      http.get(`${API}/admin/assistant/tuning`, () => HttpResponse.json([])),
      http.get(`${API}/admin/assistant/connections`, () => HttpResponse.json(conns)),
      http.get(`${API}/admin/assistant/connections/:name/models`, () =>
        HttpResponse.json({
          server: 'other',
          reachable: true,
          canManage: false,
          models: [{ id: 'claude-sonnet-5-5', offered: true, tools: true, vision: true }],
        } satisfies Schemas['AdminModelServer']),
      ),
      http.post(`${API}/admin/assistant/connections`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminConnectionInput'];
        posted.push(body);
        const { apiKey, ...rest } = body;
        conns = [
          ...conns,
          { ...rest, source: 'panel', hasKey: !!apiKey, keyUnreadable: false, version: 1, controls: ctl },
        ];
        return HttpResponse.json(conns.at(-1), { status: 201 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/admin/assistant');
    const list = await screen.findByRole('list', { name: 'Connections' });
    // the list names the provider, not the wire format
    expect(await within(list).findByText('Anthropic')).toBeTruthy();

    // Models first, then Add: the dialog takes over, the models panel goes
    await user.click(within(list).getByRole('button', { name: 'Models' }));
    expect(await screen.findByRole('form', { name: 'Models and use of Claude (file)' })).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Add a connection' }));
    const dlg = await screen.findByRole('dialog', { name: 'Add a connection' });
    expect(screen.queryByRole('form', { name: 'Models and use of Claude (file)' })).toBeNull();

    const providers = within(dlg).getByRole('radiogroup', { name: 'Provider' });
    expect(
      within(providers)
        .getAllByRole('radio')
        .map((b) => b.querySelector('b')?.textContent),
    ).toEqual(['Anthropic', 'OpenAI', 'Google', 'Local server', 'Other']);
    // Google comes with the cloud AI tasks: shown, not yet to be chosen
    const google = within(providers).getByRole('radio', { name: /^Google/ });
    expect(google.getAttribute('aria-disabled')).toBe('true');
    await user.click(google);
    expect(within(dlg).queryByRole('form', { name: 'Connection' })).toBeNull();

    // OpenAI, then back, then Anthropic: the form starts from the provider chosen last
    await user.click(within(providers).getByRole('radio', { name: /^OpenAI/ }));
    expect(
      within(await within(dlg).findByRole('form', { name: 'Connection' })).getByLabelText<HTMLInputElement>('Address')
        .value,
    ).toBe('https://api.openai.com/v1');
    await user.click(within(dlg).getByRole('button', { name: 'Provider' }));
    await user.click(
      within(within(dlg).getByRole('radiogroup', { name: 'Provider' })).getByRole('radio', { name: /^Anthropic/ }),
    );
    const form = await within(dlg).findByRole('form', { name: 'Connection' });
    // its key is all it asks for; without one there is nothing to save
    const save = within(form).getByRole<HTMLButtonElement>('button', { name: 'Save' });
    expect(save.disabled).toBe(true);
    // the name "anthropic" is taken by the file's connection
    expect(within(form).getByLabelText<HTMLInputElement>('Name').value).toBe('anthropic-2');
    fireEvent.change(within(form).getByLabelText(/^API key/), { target: { value: 'sk-ant-1' } });
    // a default model the offered patterns leave out is refused here, before saving
    fireEvent.change(within(form).getByLabelText(/^Models offered/), { target: { value: 'claude-haiku*' } });
    fireEvent.change(within(form).getByLabelText('Default model'), { target: { value: 'claude-sonnet-5-5' } });
    expect(within(form).getByRole('alert').textContent).toContain('fits none of the models offered');
    expect(save.disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText(/^Models offered/), { target: { value: 'claude-*' } });
    expect(within(form).queryByRole('alert')).toBeNull();
    await user.click(save);
    await waitFor(() => {
      expect(posted[0]).toEqual({
        name: 'anthropic-2',
        label: 'Claude',
        api: 'anthropic',
        baseUrl: 'https://api.anthropic.com',
        cloud: true,
        defaultModel: 'claude-sonnet-5-5',
        models: ['claude-*'],
        contextTokens: 0,
        apiKey: 'sk-ant-1',
      });
    });
    // on to its models
    const models = await screen.findByRole('form', { name: 'Models and use of Claude' });
    expect(within(models).getByRole('combobox', { name: 'Default model' })).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Add a connection' })).toBeNull();
  });

  it('adds and changes connections with a key that never comes back, and tunes a model (ADR 0035)', async () => {
    const conn = (over: Partial<Schemas['AdminConnection']>): Schemas['AdminConnection'] => ({
      name: 'lmstudio',
      label: 'LM Studio',
      api: 'openai',
      baseUrl: 'http://10.0.0.5:1234/v1',
      cloud: false,
      defaultModel: '',
      models: [],
      contextTokens: 8192,
      source: 'file',
      hasKey: false,
      keyUnreadable: false,
      version: 0,
      controls: { enabled: true, unloadAfterMinutes: 30, defaultModel: '', photoModel: '', version: 0 },
      ...over,
    });
    let conns = [conn({})];
    const posted: Schemas['AdminConnectionInput'][] = [];
    const updated: Schemas['AdminConnectionUpdate'][] = [];
    const tuned: Schemas['ModelTuning'][] = [];
    server.use(
      http.get(`${API}/admin/settings`, () =>
        HttpResponse.json({
          version: 1,
          defaults: {},
          features: {},
          limits: {},
        } satisfies Schemas['InstanceSettingsDoc']),
      ),
      http.get(`${API}/admin/advice`, () => HttpResponse.json([])),
      http.get(`${API}/admin/assistant/connections`, () => HttpResponse.json(conns)),
      http.post(`${API}/admin/assistant/connections`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminConnectionInput'];
        posted.push(body);
        const c = conn({ ...body, source: 'panel', hasKey: !!body.apiKey, version: 1 });
        conns = [...conns, c];
        return HttpResponse.json(c, { status: 201 });
      }),
      http.put(`${API}/admin/assistant/connections/:name`, async ({ request }) => {
        const body = (await request.json()) as Schemas['AdminConnectionUpdate'];
        updated.push(body);
        conns = conns.map((c) => (c.name === 'gpu' ? { ...c, label: body.label, version: 2 } : c));
        return HttpResponse.json(conns[1]);
      }),
      // a new connection goes on to its models: the server answers with one model
      http.get(`${API}/admin/assistant/connections/:name/models`, () =>
        HttpResponse.json({
          server: 'ollama',
          reachable: true,
          latencyMs: 12,
          canManage: false,
          models: [{ id: 'qwen3:4b', offered: true, tools: true, vision: false }],
        } satisfies Schemas['AdminModelServer']),
      ),
      http.get(`${API}/admin/assistant/tuning`, () => HttpResponse.json(tuned.at(-1) ? [tuned.at(-1)] : [])),
      http.put(`${API}/admin/assistant/tuning`, async ({ request }) => {
        const body = (await request.json()) as Schemas['ModelTuning'];
        tuned.push(body);
        return HttpResponse.json(body);
      }),
    );
    renderApp('/admin/assistant');
    expect(await screen.findByText('configuration file')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull(); // the file's are read only

    await userEvent.click(screen.getByRole('button', { name: 'Add a connection' }));
    // the provider first (Phase 26, U12); "Other" asks for everything
    const dlg = await screen.findByRole('dialog', { name: 'Add a connection' });
    await userEvent.click(
      within(within(dlg).getByRole('radiogroup', { name: 'Provider' })).getByRole('radio', { name: /^Other/ }),
    );
    const form = await screen.findByRole('form', { name: 'Connection' });
    // nothing for the browser to fill in: it put the account's e-mail into Models offered and a saved
    // password into the API key (found at the Phase 20 gate)
    expect(form.getAttribute('autocomplete')).toBe('off');
    expect(
      within(form)
        .getByLabelText(/^API key/)
        .getAttribute('autocomplete'),
    ).toBe('new-password');
    for (const box of within(form).getAllByRole('textbox')) expect(box.getAttribute('autocomplete')).toBe('off');
    fireEvent.change(within(form).getByLabelText('Name'), { target: { value: 'gpu' } });
    fireEvent.change(within(form).getByLabelText('Label'), { target: { value: 'GPU box' } });
    fireEvent.change(within(form).getByLabelText('Address'), { target: { value: 'http://10.0.0.9:11434/v1' } });
    fireEvent.change(within(form).getByLabelText(/^Models offered/), { target: { value: 'qwen3*, gemma3*' } });
    fireEvent.change(within(form).getByLabelText(/^API key/), { target: { value: 'sk-1' } });
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(posted[0]).toEqual({
        name: 'gpu',
        label: 'GPU box',
        api: 'openai',
        baseUrl: 'http://10.0.0.9:11434/v1',
        cloud: false,
        defaultModel: '',
        models: ['qwen3*', 'gemma3*'],
        contextTokens: 0,
        apiKey: 'sk-1',
      });
    });
    expect(await screen.findByText(/API key stored/)).toBeTruthy();
    // the dialog is gone and the new connection's models are open, with what the server said
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Add a connection' })).toBeNull();
    });
    const models = await screen.findByRole('form', { name: 'Models and use of GPU box' });
    expect(await within(models).findByText(/Connected to Ollama · 1 model/)).toBeTruthy();

    // a label change keeps the key, and sends none
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    // one thing open at a time: the dialog takes the models' place
    expect(await screen.findByRole('dialog', { name: 'Change GPU box' })).toBeTruthy();
    expect(screen.queryByRole('form', { name: 'Models and use of GPU box' })).toBeNull();
    const edit = await screen.findByRole('form', { name: 'Connection' });
    expect(within(edit).queryByLabelText(/New API key/)).toBeNull();
    fireEvent.change(within(edit).getByLabelText('Label'), { target: { value: 'GPU box (Ollama)' } });
    await userEvent.click(within(edit).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(updated[0]).toMatchObject({ version: 1, label: 'GPU box (Ollama)', keyAction: 'keep' });
    });
    expect(updated[0] && 'apiKey' in updated[0]).toBe(false);

    await userEvent.click(screen.getByRole('button', { name: 'Tune a model' }));
    const tune = await screen.findByRole('form', { name: 'Tuning' });
    fireEvent.change(within(tune).getByLabelText('Model'), { target: { value: 'qwen3:4b' } });
    fireEvent.change(within(tune).getByLabelText(/^Temperature/), { target: { value: '0,5' } });
    fireEvent.change(within(tune).getByLabelText('Thinking'), { target: { value: 'off' } });
    fireEvent.change(within(tune).getByLabelText(/^Note for this model/), { target: { value: 'Be brief.' } });
    await userEvent.click(within(tune).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(tuned[0]).toEqual({
        connection: 'lmstudio',
        model: 'qwen3:4b',
        temperature: 0.5,
        think: 'off',
        extraPrompt: 'Be brief.',
      });
    });
    expect(await screen.findByText(/temperature 0.5 · thinking off · a note/)).toBeTruthy();
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
    renderApp('/transactions/all');
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
    db.features = { overview: false, assistant: false, assistantDigests: false, friends: true, sharedGroups: true };
    const { router } = renderApp('/overview/all');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/transactions/all');
    });
    expect(screen.queryByRole('button', { name: 'Personal finance overview' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Assistant/ })).toBeNull();
  });

  it('starts a new user from the instance defaults', async () => {
    db.user.isAdmin = false;
    db.defaults = { themeId: 'grove', noViz: true };
    renderApp('/transactions/all');
    await waitFor(() => {
      expect(document.documentElement.dataset['themeId']).toBe('grove');
    });
    const { useUi } = await import('../stores/uiStore');
    expect(useUi.getState().vizOpen).toBe(false);
  });
});
