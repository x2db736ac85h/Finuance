// MSW API for component tests: a small in-memory Finuance with one signed-in user. Tests change
// `db` or add handlers with server.use(...).

import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

import type { Schemas } from '../api/client';

export const API = `${window.location.origin}/api/v1`;

export type Db = {
  signedIn: boolean;
  signupEnabled: boolean;
  user: Schemas['User'];
  prefs: Schemas['Preferences'];
  accounts: Schemas['Account'][];
  sessions: Schemas['Session'][];
  imports: Schemas['ImportSummary'][];
  /** Rows the fake query endpoint pages through (no filtering). */
  transactions: Schemas['TransactionRow'][];
  duplicatesSkipped: number;
  /** Groups; their stats are computed from `transactions`. */
  groups: { id: string; name: string; color: number }[];
  bulks: Schemas['BulkUpdate'][];
  idRequests: Schemas['IdsRequest'][];
  queries: Schemas['TransactionQuery'][];
  puts: Schemas['Preferences'][];
};

export const freshDb = (): Db => ({
  signedIn: true,
  signupEnabled: true,
  user: {
    id: '00000000-0000-4000-8000-000000000001',
    email: 'alex@example.com',
    displayName: 'Alex',
    isDemo: false,
    createdAt: '2026-10-01T10:00:00Z',
  },
  prefs: { version: 0, prefs: {} },
  accounts: [],
  sessions: [],
  imports: [],
  transactions: [],
  duplicatesSkipped: 0,
  groups: [],
  bulks: [],
  idRequests: [],
  queries: [],
  puts: [],
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
    HttpResponse.json({ version: 'test', signupEnabled: db.signupEnabled, demoMode: false }),
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
    db.transactions = db.transactions.map((t) =>
      t.group?.id === g.id ? { ...t, group: { id: g.id, name: g.name } } : t,
    );
    return HttpResponse.json(groupOf(g));
  }),
  http.delete(`${API}/groups/:id`, ({ params }) => {
    db.groups = db.groups.filter((g) => g.id !== params['id']);
    db.transactions = db.transactions.map((t) => (t.group?.id === params['id'] ? ungrouped(t) : t));
    return new HttpResponse(null, { status: 204 });
  }),
  http.post(`${API}/transactions/bulk`, async ({ request }) => {
    const b = (await request.json()) as Schemas['BulkUpdate'];
    db.bulks.push(b);
    const g = db.groups.find((x) => x.id === b.set.groupId);
    let updated = 0;
    db.transactions = db.transactions.map((t) => {
      if (!b.ids.includes(t.id)) return t;
      updated++;
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

function groupOf(g: { id: string; name: string; color: number }): Schemas['Group'] {
  const rows = db.transactions.filter((t) => t.group?.id === g.id);
  const spent = rows.reduce((n, t) => n + Math.max(0, -t.amount.amountMinor), 0);
  const income = rows.reduce((n, t) => n + Math.max(0, t.amount.amountMinor), 0);
  return {
    ...g,
    count: rows.length,
    spent: { amountMinor: spent, currency: 'PLN' },
    income: { amountMinor: income, currency: 'PLN' },
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
  ...over,
});
