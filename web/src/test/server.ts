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
  queries: Schemas['TransactionQuery'][];
  puts: Schemas['Preferences'][];
};

export const freshDb = (): Db => ({
  signedIn: true,
  signupEnabled: true,
  user: {
    id: '00000000-0000-4000-8000-000000000001',
    email: 'irwin@example.com',
    displayName: 'Irwin',
    isDemo: false,
    createdAt: '2026-10-01T10:00:00Z',
  },
  prefs: { version: 0, prefs: {} },
  accounts: [],
  sessions: [],
  imports: [],
  transactions: [],
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
    const sum = db.transactions.reduce((n, r) => n + r.amount.amountMinor, 0);
    return HttpResponse.json({
      rows,
      offset,
      total: db.transactions.length,
      duplicatesSkipped: 0,
      ...(db.transactions.length ? { stats: { column: 'amount', sum } } : {}),
    });
  }),
  http.post(`${API}/auth/logout`, () => {
    db.signedIn = false;
    return new HttpResponse(null, { status: 204 });
  }),
);

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
