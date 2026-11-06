import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { applyProposal, type Proposal } from '../api/hooks/assistant';
import { API, server } from '../test/server';
import { proposalTitle } from './Proposal';

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const CHAT = '00000000-0000-4000-8000-0000000000f1';
const CAT = '00000000-0000-4000-8000-0000000000c1';
const base = (over: Partial<Proposal>): Proposal => ({
  key: '00000000000000a1',
  action: 'set_budget',
  matched: 1,
  sample: [],
  ...over,
});
type Call = {
  method: string;
  path: string;
  chat: string | null;
  key: string | null;
  auto: string | null;
  body: unknown;
};

function record(calls: Call[]) {
  const capture = async (method: string, request: Request) => {
    calls.push({
      method,
      path: new URL(request.url).pathname.replace('/api/v1', ''),
      chat: request.headers.get('X-Assistant-Chat'),
      key: request.headers.get('X-Assistant-Proposal'),
      auto: request.headers.get('X-Assistant-Auto'),
      body: await request.json(),
    });
  };
  server.use(
    http.put(`${API}/budgets/:id`, async ({ request }) => {
      await capture('PUT', request);
      return new HttpResponse(null, { status: 204 });
    }),
    http.post(`${API}/categories`, async ({ request }) => {
      await capture('POST', request);
      return HttpResponse.json({ id: CAT, name: 'Kawa' }, { status: 201 });
    }),
    http.post(`${API}/transactions`, async ({ request }) => {
      await capture('POST', request);
      return HttpResponse.json({} as Schemas['TransactionDetail'], { status: 201 });
    }),
    http.post(`${API}/transactions/bulk`, async ({ request }) => {
      await capture('POST', request);
      return HttpResponse.json({ updated: 2 });
    }),
    http.patch(`${API}/shared-groups/:id/expenses/:expenseId`, async ({ request }) => {
      await capture('PATCH', request);
      return new HttpResponse(null, { status: 204 });
    }),
  );
}

describe('proposals v2 (ADR 0030)', () => {
  it('names each change in its card', () => {
    const pln = (amountMinor: number) => ({ amountMinor, currency: 'PLN' });
    expect(proposalTitle(base({ categoryName: 'Rachunki', budget: pln(80000), budgetBefore: pln(50000) }))).toMatch(
      /^Set the monthly budget of “Rachunki” to .*800.* \(now .*500.*\)$/,
    );
    expect(proposalTitle(base({ categoryName: 'Rachunki', removeBudget: true }))).toBe(
      'Remove the budget of “Rachunki” (back to the default from earlier months)',
    );
    expect(
      proposalTitle(
        base({
          action: 'add_expense',
          amount: pln(-2550),
          payee: 'Kawiarnia',
          date: '2026-11-05',
          accountName: 'Portfel',
          categoryName: 'Kawa',
        }),
      ),
    ).toMatch(/^Add .*25.* paid to “Kawiarnia” on 05\.11\.2026 to Portfel · Kawa$/);
    expect(proposalTitle(base({ action: 'add_to_shared_group', matched: 2, sharedGroupName: 'Wyjazd do Pragi' }))).toBe(
      'Share 2 transactions with “Wyjazd do Pragi”',
    );
  });

  it('splits a shared expense: the title names who pays what, Apply sends the split', async () => {
    const split = base({
      key: '00000000000000a5',
      action: 'split_expense',
      sharedGroupId: 'sg1',
      sharedGroupName: 'Wyjazd do Pragi',
      expenseId: 'x1',
      date: '2026-10-03',
      amount: { amountMinor: 4000, currency: 'PLN' },
      payee: 'KINO',
      expenseCategory: 'Rozrywka',
      paidBy: 'm1',
      split: {
        mode: 'exact',
        members: [
          { memberId: 'm1', name: 'Alex', amountMinor: 3000 },
          { memberId: 'm2', name: 'Bo', amountMinor: 1000 },
        ],
      },
    });
    expect(proposalTitle(split)).toMatch(
      /^Split “KINO” \(03\.10\.2026, .*40.*\) in “Wyjazd do Pragi” exactly: Alex .*30.*, Bo .*10.*$/,
    );
    expect(proposalTitle({ ...split, split: { mode: 'equal', members: [{ memberId: 'm2', name: 'Bo' }] } })).toMatch(
      /all for Bo$/,
    );
    const calls: Call[] = [];
    record(calls);
    await applyProposal(split, CHAT);
    expect(calls).toEqual([
      {
        method: 'PATCH',
        path: '/shared-groups/sg1/expenses/x1',
        chat: CHAT,
        key: '00000000000000a5',
        auto: null,
        body: {
          bookedOn: '2026-10-03',
          amountMinor: 4000,
          payee: 'KINO',
          category: 'Rozrywka',
          note: '',
          paidBy: 'm1',
          split: {
            mode: 'exact',
            exact: [
              { memberId: 'm1', shareMinor: 3000 },
              { memberId: 'm2', shareMinor: 1000 },
            ],
          },
        },
      },
    ]);
  });

  it('applies through the normal endpoints, every request marked with the chat and the proposal', async () => {
    const calls: Call[] = [];
    record(calls);
    await applyProposal(base({ categoryId: CAT, budget: { amountMinor: 80000, currency: 'PLN' } }), CHAT);
    await applyProposal(base({ key: '00000000000000a2', categoryId: CAT, removeBudget: true }), CHAT, true);
    await applyProposal(
      base({
        key: '00000000000000a3',
        action: 'add_expense',
        accountId: '00000000-0000-4000-8000-0000000000a9',
        date: '2026-11-05',
        amount: { amountMinor: -2550, currency: 'PLN' },
        payee: 'Kawiarnia',
        categoryName: 'Kawa',
        note: 'z Bo',
      }),
      CHAT,
    );
    const n = await applyProposal(
      base({ key: '00000000000000a4', action: 'add_to_shared_group', ids: ['1', '2'], groupId: 'g1', matched: 2 }),
      CHAT,
    );
    expect(n).toBe(2);
    expect(calls).toEqual([
      {
        method: 'PUT',
        path: `/budgets/${CAT}`,
        chat: CHAT,
        key: '00000000000000a1',
        auto: null,
        body: { amountMinor: 80000 },
      },
      {
        method: 'PUT',
        path: `/budgets/${CAT}`,
        chat: CHAT,
        key: '00000000000000a2',
        auto: 'true',
        body: { amountMinor: null },
      },
      // the new category first, then the expense with it
      { method: 'POST', path: '/categories', chat: CHAT, key: '00000000000000a3', auto: null, body: { name: 'Kawa' } },
      {
        method: 'POST',
        path: '/transactions',
        chat: CHAT,
        key: '00000000000000a3',
        auto: null,
        body: {
          accountId: '00000000-0000-4000-8000-0000000000a9',
          bookedOn: '2026-11-05',
          amountMinor: -2550,
          payee: 'Kawiarnia',
          categoryId: CAT,
          note: 'z Bo',
        },
      },
      {
        method: 'POST',
        path: '/transactions/bulk',
        chat: CHAT,
        key: '00000000000000a4',
        auto: null,
        body: { ids: ['1', '2'], set: { groupId: 'g1' } },
      },
    ]);
  });
});
