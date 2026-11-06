// MSW API for component tests: a small in-memory Finuance with one signed-in user. Tests change
// `db` or add handlers with server.use(...).

import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

import type { Schemas } from '../api/client';

export const API = `${window.location.origin}/api/v1`;

export type Db = {
  signedIn: boolean;
  signupEnabled: boolean;
  demoMode: boolean;
  /** GET /meta: switches, view defaults and approved themes (Phase 19). */
  features: Schemas['Features'];
  defaults: Schemas['ViewDefaults'];
  themes: Schemas['PublicTheme'][];
  user: Schemas['User'];
  prefs: Schemas['Preferences'];
  accounts: Schemas['Account'][];
  sessions: Schemas['Session'][];
  imports: Schemas['ImportSummary'][];
  friends: Schemas['Friend'][];
  notifications: Schemas['Notification'][];
  notificationsRead: number;
  /** The assistant's changes (ADR 0030); the fake revert marks one undone. */
  aiChanges: Schemas['AIChange'][];
  /** Rows the fake query endpoint pages through (no filtering). */
  transactions: Schemas['TransactionRow'][];
  duplicatesSkipped: number;
  /** Groups; their stats are computed from `transactions`. */
  groups: { id: string; name: string; color: number; budgetMinor?: number }[];
  bulks: Schemas['BulkUpdate'][];
  /** Answers of GET /rules/suggestions. */
  ruleSuggestions: Schemas['RuleSuggestion'][];
  idRequests: Schemas['IdsRequest'][];
  queries: Schemas['TransactionQuery'][];
  puts: Schemas['Preferences'][];
  /** Answers of POST /insights/{name}; empty results when not set. */
  insights: Partial<Insights>;
  /** Bodies of every insight request, with the endpoint name. */
  insightRequests: { name: keyof Insights; body: Record<string, unknown> }[];
  /** The answer of POST /overview (no data until set) and the request bodies. */
  overview: Schemas['Overview'];
  overviewRequests: Schemas['OverviewRequest'][];
  budgetPuts: { categoryId: string; amountMinor: number | null }[];
  investments: Schemas['Investments'];
  investmentPuts: Schemas['Investments'][];
  goals: Schemas['Goal'][];
  mergeSuggestions: Schemas['MergeSuggestion'][];
  merges: { id: string; body: Schemas['MergeRequest'] }[];
  goalWrites: { method: 'POST' | 'PATCH'; id?: string; body: unknown; header: string | null }[];
  accountPatches: { id: string; body: Schemas['AccountInput'] }[];
};

export type Insights = {
  summary: Schemas['InsightSummary'];
  categories: Schemas['InsightCategories'];
  cashflow: Schemas['InsightCashflow'];
  pace: Schemas['InsightPace'];
  payees: Schemas['InsightPayees'];
  daily: Schemas['InsightDaily'];
};

const emptyInsights: Insights = {
  summary: { currency: 'PLN', count: 0, spentMinor: 0, incomeMinor: 0, netMinor: 0, days: 1, avgPerDayMinor: 0 },
  categories: { currency: 'PLN', totalMinor: 0, categories: [] },
  cashflow: { currency: 'PLN', months: [] },
  pace: { currency: 'PLN', current: [] },
  payees: { currency: 'PLN', total: 0, payees: [] },
  daily: { currency: 'PLN', days: [] },
};

export const freshDb = (): Db => ({
  signedIn: true,
  signupEnabled: true,
  demoMode: false,
  features: { overview: true, assistant: true, assistantDigests: true, friends: true, sharedGroups: true },
  friends: [],
  notifications: [],
  notificationsRead: 0,
  aiChanges: [],
  defaults: {},
  themes: [],
  user: {
    id: '00000000-0000-4000-8000-000000000001',
    email: 'alex@example.com',
    displayName: 'Alex',
    isDemo: false,
    createdAt: '2026-10-01T10:00:00Z',
    isAdmin: false,
    mustChangePassword: false,
    limits: {
      maxUploadBytes: 20 << 20,
      importsPerDay: 0,
      maxAccounts: 0,
      maxGroups: 0,
      maxSharedGroups: 0,
      assistantPerHour: 30,
      assistantTokensPerDay: 200_000,
    },
  },
  prefs: { version: 0, prefs: {} },
  accounts: [],
  sessions: [],
  imports: [],
  transactions: [],
  duplicatesSkipped: 0,
  groups: [],
  bulks: [],
  ruleSuggestions: [],
  idRequests: [],
  queries: [],
  puts: [],
  insights: {},
  insightRequests: [],
  overview: { currency: 'PLN' },
  overviewRequests: [],
  budgetPuts: [],
  investments: {
    version: 0,
    ppk: { employeeMinor: 0, employerMinor: 0, stateMinor: 0, valueMinor: 0 },
    swrBp: 400,
    realReturnBp: 500,
    positions: [],
  },
  investmentPuts: [],
  goals: [],
  goalWrites: [],
  mergeSuggestions: [],
  merges: [],
  accountPatches: [],
});

export let db = freshDb();
export function resetDb(): void {
  db = freshDb();
}

export const problem = (status: number, code: string, detail?: string, extra?: object) =>
  HttpResponse.json(
    { type: `/problems/${code}`, title: code, status, code, ...(detail ? { detail } : {}), ...extra },
    { status },
  );

const authed = <T>(fn: () => T) => (db.signedIn ? fn() : problem(401, 'unauthenticated', 'Sign in first.'));

export const account = (over: Partial<Schemas['Account']> = {}): Schemas['Account'] => ({
  id: '00000000-0000-4000-8000-0000000000a1',
  name: 'Konto',
  kind: 'bank',
  currency: 'PLN',
  openingBalance: { amountMinor: 0, currency: 'PLN' },
  txCount: 0,
  isDemo: false,
  archived: false,
  createdAt: '2026-10-01T10:00:00Z',
  ...over,
});

export const server = setupServer(
  http.get(`${API}/meta`, () =>
    HttpResponse.json({
      version: 'test',
      signupEnabled: db.signupEnabled,
      demoMode: db.demoMode,
      assistantEnabled: false,
      features: db.features,
      defaults: db.defaults,
      themes: db.themes,
    } satisfies Schemas['Meta']),
  ),
  http.get(`${API}/me`, () => authed(() => HttpResponse.json(db.user))),
  http.get(`${API}/preferences`, () => authed(() => HttpResponse.json(db.prefs))),
  http.put(`${API}/preferences`, async ({ request }) => {
    const body = (await request.json()) as Schemas['Preferences'];
    if (body.version !== db.prefs.version) return problem(409, 'stale_version', 'Stale.', { current: db.prefs });
    db.prefs = { version: db.prefs.version + 1, prefs: body.prefs };
    db.puts.push(db.prefs);
    return HttpResponse.json(db.prefs);
  }),
  http.get(`${API}/accounts`, () => authed(() => HttpResponse.json(db.accounts))),
  http.get(`${API}/me/sessions`, () => authed(() => HttpResponse.json(db.sessions))),
  http.get(`${API}/friends`, () => authed(() => HttpResponse.json(db.friends))),
  http.get(`${API}/notifications`, () =>
    authed(() =>
      HttpResponse.json({ items: db.notifications, unread: db.notifications.filter((n) => !n.readAt).length }),
    ),
  ),
  http.post(`${API}/notifications/read`, () =>
    authed(() => {
      db.notificationsRead++;
      db.notifications = db.notifications.map((n) => ({ ...n, readAt: n.readAt ?? '2026-11-03T12:00:00Z' }));
      return new HttpResponse(null, { status: 204 });
    }),
  ),
  http.get(`${API}/me/activity`, () => authed(() => HttpResponse.json({ events: [] }))),
  http.get(`${API}/ai-changes`, () => authed(() => HttpResponse.json({ changes: db.aiChanges }))),
  http.post(`${API}/ai-changes/:id/revert`, ({ params }) =>
    authed(() => {
      const c = db.aiChanges.find((x) => x.id === params['id']);
      if (!c) return problem(404, 'not_found', 'Not found.');
      if (c.revertedAt) return problem(409, 'conflict', 'Undone already.');
      c.revertedAt = '2026-11-06T12:00:00Z';
      return HttpResponse.json({ change: c, rows: c.items, deleted: 0, kept: 0 });
    }),
  ),
  // developer mode off, as on most instances (tests that need the section override this)
  http.get(`${API}/admin/developer`, () => authed(() => problem(404, 'not_found', 'Not found.'))),
  // Administration → Assistant starts empty (tests that need more override these)
  http.get(`${API}/admin/assistant/connections`, () => authed(() => HttpResponse.json([]))),
  http.get(`${API}/admin/assistant/tuning`, () => authed(() => HttpResponse.json([]))),
  http.get(`${API}/admin/advice`, () => authed(() => HttpResponse.json([]))),
  http.get(`${API}/imports`, () => authed(() => HttpResponse.json(db.imports))),
  http.post(`${API}/transactions/query`, async ({ request }) => {
    const q = (await request.json()) as Schemas['TransactionQuery'];
    db.queries.push(q);
    const offset = q.offset;
    const rows = db.transactions.slice(offset, offset + q.limit);
    if (!q.includeTotals) return HttpResponse.json({ rows, offset });
    const column = q.statColumn ?? 'amount';
    const xs = db.transactions.flatMap((r) => {
      const m = column === 'amount' ? r.amount : r.balanceAfter;
      return m ? [m.amountMinor] : [];
    });
    const sum = xs.reduce((n, x) => n + x, 0);
    const stats = xs.length
      ? { column, sum, avg: Math.round(sum / xs.length), min: Math.min(...xs), max: Math.max(...xs) }
      : { column };
    return HttpResponse.json({
      rows,
      offset,
      total: db.transactions.length,
      duplicatesSkipped: db.duplicatesSkipped,
      stats,
    });
  }),
  http.post(`${API}/insights/:name`, async ({ params, request }) => {
    const name = params['name'] as keyof Insights;
    db.insightRequests.push({ name, body: (await request.json()) as Record<string, unknown> });
    return HttpResponse.json(db.insights[name] ?? emptyInsights[name]);
  }),
  http.get(`${API}/rules/suggestions`, () => authed(() => HttpResponse.json(db.ruleSuggestions))),
  http.get(`${API}/groups`, () => authed(() => HttpResponse.json(db.groups.map(groupOf)))),
  http.post(`${API}/groups`, async ({ request }) => {
    const b = (await request.json()) as Schemas['GroupInput'];
    if (db.groups.some((g) => g.name.toLowerCase() === b.name.toLowerCase())) {
      return problem(409, 'conflict', 'A group with this name already exists.');
    }
    const g = {
      id: `00000000-0000-4000-8000-${String(db.groups.length + 1).padStart(12, '0')}`,
      name: b.name,
      color: b.color ?? 0,
      ...(b.budgetMinor ? { budgetMinor: b.budgetMinor } : {}),
    };
    db.groups.push(g);
    return HttpResponse.json(groupOf(g), { status: 201 });
  }),
  http.patch(`${API}/groups/:id`, async ({ params, request }) => {
    const b = (await request.json()) as Schemas['GroupInput'];
    const g = db.groups.find((x) => x.id === params['id']);
    if (!g) return problem(404, 'not_found');
    g.name = b.name;
    g.color = b.color ?? g.color;
    if (b.budgetMinor === 0) delete g.budgetMinor;
    else if (b.budgetMinor !== undefined) g.budgetMinor = b.budgetMinor;
    db.transactions = db.transactions.map((t) =>
      t.group?.id === g.id ? { ...t, group: { id: g.id, name: g.name } } : t,
    );
    return HttpResponse.json(groupOf(g));
  }),
  http.get(`${API}/groups/:id/report`, ({ params }) => {
    const g = db.groups.find((x) => x.id === params['id']);
    if (!g) return problem(404, 'not_found');
    return HttpResponse.json(groupReportOf(g));
  }),
  http.delete(`${API}/groups/:id`, ({ params }) => {
    db.groups = db.groups.filter((g) => g.id !== params['id']);
    db.transactions = db.transactions.map((t) => (t.group?.id === params['id'] ? ungrouped(t) : t));
    return new HttpResponse(null, { status: 204 });
  }),
  http.post(`${API}/transactions/bulk`, async ({ request }) => {
    const b = (await request.json()) as Schemas['BulkUpdate'];
    db.bulks.push(b);
    const g = b.set && db.groups.find((x) => x.id === b.set?.groupId);
    let updated = 0;
    db.transactions = db.transactions.map((t) => {
      if (!b.ids.includes(t.id)) return t;
      updated++;
      if (b.transfer) return { ...t, isTransfer: b.transfer === 'yes' };
      if (b.category) return { ...t, categorySource: b.category.id ? ('manual' as const) : ('bank' as const) };
      return g ? { ...t, group: { id: g.id, name: g.name } } : ungrouped(t);
    });
    return HttpResponse.json({ updated });
  }),
  http.post(`${API}/transactions/ids`, async ({ request }) => {
    const b = (await request.json()) as Schemas['IdsRequest'];
    db.idRequests.push(b);
    return HttpResponse.json(
      db.transactions.slice(b.offset, b.offset + b.limit).map((t) => ({
        id: t.id,
        amountMinor: t.amount.amountMinor,
        ...(t.group ? { groupId: t.group.id } : {}),
      })),
    );
  }),
  http.post(`${API}/overview`, async ({ request }) => {
    db.overviewRequests.push((await request.json()) as Schemas['OverviewRequest']);
    return HttpResponse.json(db.overview);
  }),
  http.put(`${API}/budgets/:categoryId`, async ({ params, request }) => {
    const body = (await request.json()) as Schemas['BudgetInput'];
    db.budgetPuts.push({ categoryId: String(params['categoryId']), amountMinor: body.amountMinor ?? null });
    return new HttpResponse(null, { status: 204 });
  }),
  http.get(`${API}/goals`, () => authed(() => HttpResponse.json({ goals: db.goals }))),
  http.get(`${API}/transactions/merge-suggestions`, () =>
    authed(() => HttpResponse.json({ suggestions: db.mergeSuggestions })),
  ),
  http.post(`${API}/transactions/:id/merge`, async ({ params, request }) => {
    const body = (await request.json()) as Schemas['MergeRequest'];
    db.merges.push({ id: String(params['id']), body });
    db.mergeSuggestions = db.mergeSuggestions.filter((m) => m.typedId !== params['id']);
    return new HttpResponse(null, { status: 204 });
  }),
  http.post(`${API}/goals`, async ({ request }) => {
    const body = (await request.json()) as Schemas['GoalInput'];
    db.goalWrites.push({ method: 'POST', body, header: request.headers.get('X-Assistant-Proposal') });
    const g: Schemas['Goal'] = {
      ...body,
      id: `goal-${String(db.goals.length + 1)}`,
      currency: body.currency ?? 'PLN',
      startsOn: body.startsOn ?? '2025-03-17',
      status: 'active',
      version: 1,
      ai: request.headers.has('X-Assistant-Proposal'),
      progress: { state: 'on_track', percent: 0, daysLeft: 100, savedMinor: 0, expectedMinor: 0, projectedMinor: 0 },
    };
    db.goals = [...db.goals, g];
    return HttpResponse.json(g, { status: 201 });
  }),
  http.patch(`${API}/goals/:id`, async ({ params, request }) => {
    const body = (await request.json()) as Schemas['GoalUpdate'];
    const id = String(params['id']);
    db.goalWrites.push({ method: 'PATCH', id, body, header: request.headers.get('X-Assistant-Proposal') });
    const cur = db.goals.find((g) => g.id === id);
    if (!cur) return problem(404, 'not_found', 'No such goal.');
    if (cur.version !== body.version) return problem(409, 'stale_version', 'The goal was changed meanwhile.');
    const g = { ...cur, ...body, version: cur.version + 1 };
    db.goals = db.goals.map((x) => (x.id === id ? g : x));
    return HttpResponse.json(g);
  }),
  http.delete(`${API}/goals/:id`, ({ params }) => {
    db.goals = db.goals.filter((g) => g.id !== params['id']);
    return new HttpResponse(null, { status: 204 });
  }),
  http.get(`${API}/investments`, () => HttpResponse.json(db.investments)),
  http.put(`${API}/investments`, async ({ request }) => {
    const body = (await request.json()) as Schemas['Investments'];
    db.investmentPuts.push(body);
    if (body.version !== db.investments.version)
      return problem(409, 'stale_version', 'Stale.', { current: db.investments });
    db.investments = { ...body, version: body.version + 1 };
    return HttpResponse.json(db.investments);
  }),
  http.patch(`${API}/accounts/:id`, async ({ params, request }) => {
    const body = (await request.json()) as Schemas['AccountInput'];
    const id = String(params['id']);
    db.accountPatches.push({ id, body });
    db.accounts = db.accounts.map((a) =>
      a.id === id ? { ...a, openingBalance: { amountMinor: body.openingBalanceMinor ?? 0, currency: a.currency } } : a,
    );
    const a = db.accounts.find((x) => x.id === id);
    return a ? HttpResponse.json(a) : problem(404, 'not_found');
  }),
  http.post(`${API}/auth/logout`, () => {
    db.signedIn = false;
    return new HttpResponse(null, { status: 204 });
  }),
);

function ungrouped(t: Schemas['TransactionRow']): Schemas['TransactionRow'] {
  const out = { ...t };
  delete out.group;
  return out;
}

function groupOf(g: Db['groups'][number]): Schemas['Group'] {
  const rows = db.transactions.filter((t) => t.group?.id === g.id);
  const spent = rows.reduce((n, t) => n + Math.max(0, -t.amount.amountMinor), 0);
  const income = rows.reduce((n, t) => n + Math.max(0, t.amount.amountMinor), 0);
  const days = rows.map((t) => t.bookedOn).sort();
  const [first, last] = [days[0], days.at(-1)];
  return {
    ...g,
    count: rows.length,
    spent: { amountMinor: spent, currency: 'PLN' },
    income: { amountMinor: income, currency: 'PLN' },
    ...(first && last ? { firstOn: first, lastOn: last } : {}),
  };
}

/** The group window's report, computed from `transactions` like the server (top 10 payees). */
function groupReportOf(g: Db['groups'][number]): Schemas['GroupReport'] {
  const group = groupOf(g);
  const out = db.transactions.filter((t) => t.group?.id === g.id && t.amount.amountMinor < 0);
  const sum = (key: (t: Schemas['TransactionRow']) => string) => {
    const m = new Map<string, { v: number; n: number }>();
    for (const t of out) {
      const k = key(t);
      const e = m.get(k) ?? { v: 0, n: 0 };
      m.set(k, { v: e.v - t.amount.amountMinor, n: e.n + 1 });
    }
    return [...m].sort((a, b) => b[1].v - a[1].v);
  };
  const days =
    group.firstOn && group.lastOn
      ? Math.round((Date.parse(group.lastOn) - Date.parse(group.firstOn)) / 86_400_000) + 1
      : 1;
  const payees = sum((t) => t.payee);
  return {
    group,
    currency: 'PLN',
    days,
    avgPerDayMinor: Math.round(group.spent.amountMinor / days),
    daily: sum((t) => t.bookedOn)
      .map(([date, e]) => ({ date, spentMinor: e.v, count: e.n }))
      .sort((a, b) => a.date.localeCompare(b.date)),
    categories: sum((t) => t.category?.name ?? '').map(([name, e], i) => ({
      ...(name ? { category: { id: `cat-${name}`, name } } : {}),
      spentMinor: e.v,
      share: e.v / group.spent.amountMinor,
      ...(i < 7 ? { colorSlot: i + 1 } : {}),
    })),
    payees: payees.slice(0, 10).map(([payee, e]) => ({ payee, spentMinor: e.v, count: e.n })),
    payeeCount: payees.length,
  };
}

/** A transaction row for the fake query endpoint. */
export const txRow = (i: number, over: Partial<Schemas['TransactionRow']> = {}): Schemas['TransactionRow'] => ({
  id: `00000000-0000-4000-8000-${String(i).padStart(12, '0')}`,
  accountId: '00000000-0000-4000-8000-0000000000a1',
  accountName: 'Konto',
  bookedOn: `2025-${String((i % 12) + 1).padStart(2, '0')}-${String((i % 28) + 1).padStart(2, '0')}`,
  amount: { amountMinor: -(1000 + i), currency: 'PLN' },
  payee: `Sklep ${String(i)}`,
  description: `Zakup ${String(i)}`,
  categorySource: 'bank',
  bankCategory: 'Zakupy',
  txType: 'Karta',
  reference: '',
  counterpartyAccount: '',
  note: '',
  isTransfer: false,
  source: 'Bank',
  ai: false,
  ...over,
});
