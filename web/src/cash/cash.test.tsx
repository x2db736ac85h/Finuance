import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp, warmUpApp } from '../test/render';

// Cash accounts (ADR 0029): a wallet created from the sidebar, transactions typed into it with
// the toolbar's Add (or N), changed and deleted from the inspector.

const WALLET = '00000000-0000-4000-8000-0000000000b9';
let created: Schemas['AccountInput'][] = [];
let added: Schemas['ManualTransactionInput'][] = [];
let patched: Schemas['TransactionPatch'][] = [];
let deleted: string[] = [];

const clientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains('grid-wrap') ? 600 : 0;
    },
  });
  await warmUpApp();
});
afterAll(() => {
  server.close();
  if (clientHeight) Object.defineProperty(HTMLElement.prototype, 'clientHeight', clientHeight);
});

const manual = (r: Schemas['TransactionRow']): Schemas['TransactionDetail'] => ({
  ...r,
  origin: 'manual',
  raw: [],
  sources: [],
  createdAt: '2026-10-01T10:00:00Z',
  updatedAt: '2026-10-01T10:00:00Z',
});

beforeEach(() => {
  resetDb();
  created = [];
  added = [];
  patched = [];
  deleted = [];
  db.accounts = [account({ txCount: 1 })];
  db.transactions = [txRow(1, { payee: 'Sklep Żabka' })];
  server.use(
    http.post(`${API}/accounts`, async ({ request }) => {
      const b = (await request.json()) as Schemas['AccountInput'];
      created.push(b);
      const a = account({ id: WALLET, name: b.name, kind: b.kind ?? 'bank', currency: b.currency ?? 'PLN' });
      db.accounts = [...db.accounts, a];
      return HttpResponse.json(a, { status: 201 });
    }),
    http.post(`${API}/transactions`, async ({ request }) => {
      const b = (await request.json()) as Schemas['ManualTransactionInput'];
      added.push(b);
      const r = txRow(9, {
        accountId: b.accountId,
        payee: b.payee,
        bookedOn: b.bookedOn,
        amount: { amountMinor: b.amountMinor, currency: 'PLN' },
      });
      db.transactions = [r];
      return HttpResponse.json(manual(r), { status: 201 });
    }),
    http.get(`${API}/transactions/:id`, ({ params }) => {
      const r = db.transactions.find((t) => t.id === params['id']);
      return r ? HttpResponse.json(manual(r)) : HttpResponse.json({}, { status: 404 });
    }),
    http.patch(`${API}/transactions/:id`, async ({ params, request }) => {
      const b = (await request.json()) as Schemas['TransactionPatch'];
      patched.push(b);
      const r = db.transactions.find((t) => t.id === params['id']);
      if (!r) return HttpResponse.json({}, { status: 404 });
      const next = {
        ...r,
        ...(b.payee ? { payee: b.payee } : {}),
        ...(b.amountMinor ? { amount: { amountMinor: b.amountMinor, currency: 'PLN' } } : {}),
      };
      db.transactions = [next];
      return HttpResponse.json(manual(next));
    }),
    http.delete(`${API}/transactions/:id`, ({ params }) => {
      deleted.push(String(params['id']));
      db.transactions = [];
      return new HttpResponse(null, { status: 204 });
    }),
    http.get(`${API}/categories`, () =>
      HttpResponse.json([{ id: '00000000-0000-4000-8000-0000000000c1', name: 'Jedzenie', txCount: 1 }]),
    ),
  );
});
afterEach(() => {
  server.resetHandlers();
});

describe('cash accounts', () => {
  it('creates a cash account from the sidebar and opens it', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await user.click(await screen.findByRole('button', { name: 'New cash account' }));
    const dialog = await screen.findByRole('dialog', { name: 'New cash account' });
    await user.clear(within(dialog).getByLabelText('Name'));
    await user.type(within(dialog).getByLabelText('Name'), 'Portfel');
    await user.click(within(dialog).getByRole('button', { name: 'Create' }));
    await waitFor(() => {
      expect(created).toEqual([{ name: 'Portfel', currency: 'PLN', kind: 'cash' }]);
    });
    expect(await screen.findByText('Created the cash account Portfel')).toBeTruthy();
  });

  it('adds an expense with the toolbar, then edits and deletes it in the inspector', async () => {
    db.accounts = [account(), account({ id: WALLET, name: 'Portfel', kind: 'cash', txCount: 0 })];
    db.transactions = [];
    const user = userEvent.setup();
    renderApp(`/t/${WALLET}`);
    await user.click(await screen.findByRole('button', { name: 'Add' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a transaction' });
    await user.type(within(dialog).getByLabelText('Amount (PLN)'), '24,50');
    await user.type(within(dialog).getByLabelText('Paid to'), 'Piekarnia');
    await user.type(within(dialog).getByLabelText('Category'), 'Jedzenie');
    await user.click(within(dialog).getByRole('button', { name: 'Add' }));
    await waitFor(() => {
      expect(added).toHaveLength(1);
    });
    expect(added[0]).toMatchObject({
      accountId: WALLET,
      amountMinor: -2450,
      payee: 'Piekarnia',
      categoryName: 'Jedzenie',
    });
    expect(added[0]?.bookedOn).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    // the new row is selected: the inspector offers Edit and Delete
    const ins = within(await screen.findByRole('complementary', { name: 'Transaction details' }));
    await user.click(await ins.findByRole('button', { name: 'Edit' }));
    const edit = await screen.findByRole('dialog', { name: 'Change the transaction' });
    const amount = within(edit).getByLabelText('Amount (PLN)');
    expect((amount as HTMLInputElement).value).toBe('24,50');
    await user.clear(amount);
    await user.type(amount, '30');
    await user.click(within(edit).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(patched[0]).toMatchObject({ amountMinor: -3000, payee: 'Piekarnia', note: '' });
    });

    await user.click(await ins.findByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByRole('dialog', { name: 'Delete this transaction?' });
    await user.click(within(confirm).getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(deleted).toEqual(['00000000-0000-4000-8000-000000000009']);
    });
  });

  it('asks for the amount and the payee, and N opens the form only in a cash account', async () => {
    db.accounts = [account({ txCount: 1 }), account({ id: WALLET, name: 'Portfel', kind: 'cash', txCount: 0 })];
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Sklep Żabka');
    await user.keyboard('n');
    expect(screen.queryByRole('dialog', { name: 'Add a transaction' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Add' })).toBeNull();

    await user.click(screen.getByRole('link', { name: /Portfel/ }));
    await screen.findByRole('button', { name: 'Add' });
    await user.keyboard('n');
    const dialog = await screen.findByRole('dialog', { name: 'Add a transaction' });
    await user.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(within(dialog).getByRole('alert').textContent).toBe('Type the amount, like 24,50.');
    await user.type(within(dialog).getByLabelText('Amount (PLN)'), '5');
    await user.click(within(dialog).getByRole('radio', { name: 'Money in' }));
    await user.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(within(dialog).getByRole('alert').textContent).toBe('Say who was paid (or who paid you).');
    expect(within(dialog).getByLabelText('Received from')).toBeTruthy();
    expect(added).toHaveLength(0);
  });
});
