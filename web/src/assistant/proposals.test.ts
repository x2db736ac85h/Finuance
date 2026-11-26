import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { ApiError } from '../api/client';
import { applyProposal, missingTarget, type Proposal } from '../api/hooks/assistant';
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
    http.post(`${API}/goals`, async ({ request }) => {
      await capture('POST', request);
      return HttpResponse.json({} as Schemas['Goal'], { status: 201 });
    }),
    http.patch(`${API}/transactions/:id`, async ({ request }) => {
      await capture('PATCH', request);
      return HttpResponse.json({} as Schemas['TransactionDetail']);
    }),
    http.patch(`${API}/goals/:id`, async ({ request }) => {
      await capture('PATCH', request);
      return HttpResponse.json({} as Schemas['Goal']);
    }),
  );
}

describe('receipt proposals (ADR 0032)', () => {
  it('names the payment and the receipt, and writes the note through PATCH /transactions/{id}', async () => {
    const p = base({
      key: '00000000000000c1',
      action: 'attach_receipt',
      ids: ['t1'],
      payee: 'BIEDRONKA SKLEP 123',
      date: '2026-11-03',
      amount: { amountMinor: -5430, currency: 'PLN' },
      accountName: 'Konto',
      note: 'zakupy\nReceipt: Biedronka · chleb',
      receiptLine: 'Receipt: Biedronka · chleb',
    });
    expect(proposalTitle(p)).toMatch(
      /^Add the receipt to “BIEDRONKA SKLEP 123” \(03\.11\.2026, .*54.*\) in Konto: Biedronka · chleb$/,
    );
    const calls: Call[] = [];
    record(calls);
    expect(await applyProposal(p, CHAT)).toBe(1);
    expect(calls).toEqual([
      {
        method: 'PATCH',
        path: '/transactions/t1',
        chat: CHAT,
        key: '00000000000000c1',
        auto: null,
        body: { note: 'zakupy\nReceipt: Biedronka · chleb' },
      },
    ]);
  });
});

describe('goal proposals (ADR 0031)', () => {
  const holiday: NonNullable<Proposal['goal']> = {
    id: 'g1',
    name: 'Wakacje',
    kind: 'save',
    targetMinor: 1_000_000,
    currency: 'PLN',
    baselineMonths: 3,
    deadline: '2027-06-30',
    status: 'active',
    version: 4,
  };
  it('names the goal or what changes, and applies it through POST and PATCH /goals', async () => {
    const create = base({
      key: '00000000000000b1',
      action: 'create_goal',
      goal: {
        name: 'Less food',
        kind: 'reduce',
        currency: 'PLN',
        categoryId: CAT,
        categoryName: 'Jedzenie',
        reducePct: 20,
        baselineMonths: 3,
        deadline: '2027-04-30',
      },
    });
    expect(proposalTitle(create)).toBe(
      'New goal “Less food”: spend 20 % less on Jedzenie than the last 3 months, until 30.04.2027',
    );
    const update = base({
      key: '00000000000000b2',
      action: 'update_goal',
      goalBefore: holiday,
      goal: { ...holiday, targetMinor: 1_200_000, deadline: '2027-08-31' },
    });
    expect(proposalTitle(update)).toMatch(/^Change the goal “Wakacje”: target .*12.*, deadline 31\.08\.2027$/);
    expect(
      proposalTitle(base({ action: 'update_goal', goalBefore: holiday, goal: { ...holiday, status: 'done' } })),
    ).toBe('Change the goal “Wakacje”: close it');

    const calls: Call[] = [];
    record(calls);
    await applyProposal(create, CHAT);
    await applyProposal(update, CHAT, true);
    expect(calls).toEqual([
      {
        method: 'POST',
        path: '/goals',
        chat: CHAT,
        key: '00000000000000b1',
        auto: null,
        body: {
          name: 'Less food',
          kind: 'reduce',
          currency: 'PLN',
          categoryId: CAT,
          reducePct: 20,
          baselineMonths: 3,
          deadline: '2027-04-30',
        },
      },
      // the whole goal with the version it was proposed on
      {
        method: 'PATCH',
        path: '/goals/g1',
        chat: CHAT,
        key: '00000000000000b2',
        auto: 'true',
        body: {
          name: 'Wakacje',
          targetMinor: 1_200_000,
          baselineMonths: 3,
          deadline: '2027-08-31',
          status: 'active',
          version: 4,
        },
      },
    ]);
  });
});

describe('a proposal without its target (WEB-2)', () => {
  const g = { kind: 'save' as const, name: 'Wakacje', currency: 'PLN', deadline: '2027-06-30', baselineMonths: 6 };
  const incomplete: [string, Proposal][] = [
    ['shared group', base({ action: 'add_to_shared_group', ids: ['1', '2'], matched: 2 })],
    ['transactions', base({ action: 'add_to_shared_group', ids: [], groupId: 'g1', matched: 0 })],
    ['transactions', base({ action: 'set_category', ids: [], categoryId: CAT })],
    ['transactions', base({ action: 'set_group', groupName: 'Trip' })],
    ['category', base({ action: 'set_budget', budget: { amountMinor: 1000, currency: 'PLN' } })],
    ['account', base({ action: 'add_expense', date: '2026-11-05', amount: { amountMinor: -100, currency: 'PLN' } })],
    ['shared expense', base({ action: 'split_expense', sharedGroupId: 'sg1' })],
    ['shared expense', base({ action: 'split_expense', expenseId: 'x1' })],
    ['goal', base({ action: 'create_goal' })],
    ['goal', base({ action: 'update_goal', goal: g })],
    ['transaction', base({ action: 'attach_receipt', ids: [] })],
  ];

  it('is refused before any request, with a message that names what is missing', async () => {
    const calls: Call[] = [];
    record(calls);
    for (const [what, p] of incomplete) {
      expect(missingTarget(p), p.action).toBe(`the ${what}`);
      const err = await applyProposal(p, CHAT).catch((e: unknown) => e);
      expect(err, p.action).toBeInstanceOf(ApiError);
      expect((err as ApiError).message).toContain(`which ${what} it is for`);
    }
    expect(calls).toEqual([]);
  });

  it('lets a complete proposal through', () => {
    expect(missingTarget(base({ action: 'add_to_shared_group', ids: ['1'], groupId: 'g1' }))).toBeNull();
    expect(missingTarget(base({ action: 'set_budget', categoryId: CAT }))).toBeNull();
    expect(missingTarget(base({ action: 'update_goal', goal: { ...g, id: 'goal1' } }))).toBeNull();
  });
});

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

describe('the opposites of set_group and set_category (Phase 27)', () => {
  it('names them and sends no group and a category reset, creating nothing first', async () => {
    const out = base({ key: '00000000000000b1', action: 'set_group', ids: ['1', '2'], matched: 2, removeGroup: true });
    const clear = base({
      key: '00000000000000b2',
      action: 'set_category',
      ids: ['3'],
      matched: 1,
      clearCategory: true,
    });
    expect(proposalTitle(out)).toBe('Take 2 transactions out of their group');
    expect(proposalTitle(clear)).toBe('Take the hand-set category off 1 transaction (back to the bank’s or a rule’s)');
    expect(missingTarget(out)).toBeNull();
    expect(missingTarget(clear)).toBeNull();
    const calls: Call[] = [];
    record(calls);
    expect(await applyProposal(out, CHAT)).toBe(2);
    await applyProposal(clear, CHAT);
    // one request each: no POST /groups or /categories comes first
    expect(calls.map((c) => [c.method, c.path, c.key, c.body])).toEqual([
      ['POST', '/transactions/bulk', '00000000000000b1', { ids: ['1', '2'], set: { groupId: null } }],
      ['POST', '/transactions/bulk', '00000000000000b2', { ids: ['3'], category: { reset: true } }],
    ]);
  });
});
