import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { renderApp, warmUpApp } from '../test/render';
import { API, db, resetDb, server } from '../test/server';
import { previewShares, toApiSplit, type SplitState } from './split';
import { hintText } from './text';

const GID = '00000000-0000-4000-8000-0000000005a1';
const ME = '00000000-0000-4000-8000-00000000a001';
const BO = '00000000-0000-4000-8000-00000000a002';

const member = (id: string, name: string, over: Partial<Schemas['SharedMember']> = {}): Schemas['SharedMember'] => ({
  id,
  displayName: name,
  role: 'member',
  state: 'active',
  isMe: false,
  former: false,
  ...over,
});

const me = member(ME, 'Alex', { role: 'owner', isMe: true });
const bo = member(BO, 'Bo');

const group = (over: Partial<Schemas['SharedGroup']> = {}): Schemas['SharedGroup'] => ({
  id: GID,
  name: 'Kraków',
  color: 2,
  currency: 'PLN',
  kind: 'trip',
  archived: false,
  createdAt: '2026-06-01T10:00:00Z',
  me,
  ...over,
});

const expense = (over: Partial<Schemas['SharedExpense']> = {}): Schemas['SharedExpense'] => ({
  id: '00000000-0000-4000-8000-0000000e0001',
  addedBy: BO,
  paidBy: BO,
  bookedOn: '2026-06-03',
  amountMinor: 3000,
  payee: 'Muzeum',
  category: 'Kultura',
  note: '',
  origin: 'typed',
  splitMode: 'equal',
  shares: [
    { memberId: ME, shareMinor: 1500 },
    { memberId: BO, shareMinor: 1500 },
  ],
  mine: false,
  sourceMissing: false,
  ...over,
});

let detail: Schemas['SharedGroupDetail'];
let posted: { path: string; body: unknown }[];

const report = (): Schemas['SharedGroupReport'] => ({
  currency: 'PLN',
  totalMinor: 3000,
  count: 1,
  firstOn: '2026-06-03',
  lastOn: '2026-06-03',
  days: 1,
  avgPerDayMinor: 3000,
  daily: [{ date: '2026-06-03', spentMinor: 3000, count: 1 }],
  categories: [{ name: 'Kultura', spentMinor: 3000, count: 1 }],
  payees: [{ name: 'Muzeum', spentMinor: 3000, count: 1 }],
  largest: detail.expenses,
  members: [
    { memberId: ME, paidMinor: 0, shareMinor: 1500 },
    { memberId: BO, paidMinor: 3000, shareMinor: 1500 },
  ],
});

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
  db.user = { ...db.user, displayName: 'Alex' };
  db.friends = [{ id: '00000000-0000-4000-8000-0000000000e1', displayName: 'Cy', since: '2026-11-01T10:00:00Z' }];
  posted = [];
  detail = {
    group: group(),
    members: [me, bo],
    expenses: [expense()],
    settlements: [],
    balances: [
      { memberId: ME, balanceMinor: -1500 },
      { memberId: BO, balanceMinor: 1500 },
    ],
    transfers: [{ from: ME, to: BO, amountMinor: 1500 }],
  };
  const record = async (path: string, request: Request) => {
    posted.push({ path, body: await request.json().catch(() => null) });
  };
  server.use(
    http.get(`${API}/shared-groups`, () => HttpResponse.json([detail.group])),
    http.get(`${API}/shared-groups/:id`, () => HttpResponse.json(detail)),
    http.get(`${API}/shared-groups/:id/report`, () => HttpResponse.json(report())),
    http.get(`${API}/groups/timeline`, () =>
      HttpResponse.json({
        groups: [
          {
            id: GID,
            source: 'shared',
            name: 'Kraków',
            color: 2,
            kind: 'trip',
            currency: 'PLN',
            firstOn: '2026-06-02',
            lastOn: '2026-06-03',
            days: 2,
            spentMinor: 9000,
            count: 2,
            perDayMinor: 4500,
            topCategory: { name: 'Kultura', spentMinor: 3000, count: 1 },
            topCategoryShare: 0.33,
          },
          {
            id: '00000000-0000-4000-8000-0000000005b2',
            source: 'personal',
            name: 'Gdańsk',
            color: 1,
            kind: 'personal',
            currency: 'PLN',
            firstOn: '2025-08-01',
            lastOn: '2025-08-04',
            days: 4,
            spentMinor: 40000,
            count: 9,
            perDayMinor: 10000,
          },
        ],
        years: [
          { year: 2026, groups: 1, spentMinor: 9000, currency: 'PLN', days: 2 },
          { year: 2025, groups: 1, spentMinor: 40000, currency: 'PLN', days: 4 },
        ],
        hints: [{ groupId: GID, kind: 'category_high', category: 'Kultura', ratio: 2.5 }],
      } satisfies Schemas['GroupTimeline']),
    ),
    http.post(`${API}/shared-groups/:id/settlements`, async ({ request }) => {
      await record('settlements', request);
      detail = {
        ...detail,
        balances: detail.balances.map((b) => ({ ...b, balanceMinor: 0 })),
        transfers: [],
        settlements: [
          {
            id: '00000000-0000-4000-8000-0000000f0001',
            from: ME,
            to: BO,
            amountMinor: 1500,
            settledOn: '2026-06-10',
            note: '',
            mine: true,
          },
        ],
      };
      return HttpResponse.json(detail.settlements[0], { status: 201 });
    }),
    http.post(`${API}/shared-groups/:id/expenses`, async ({ request }) => {
      await record('expenses', request);
      return HttpResponse.json({ id: '00000000-0000-4000-8000-0000000e0002' }, { status: 201 });
    }),
    http.post(`${API}/shared-groups/:id/accept`, async ({ request }) => {
      await record('accept', request);
      detail = { ...detail, group: group({ me: { ...me, role: 'member', state: 'active' } }) };
      return HttpResponse.json(detail.group);
    }),
    http.post(`${API}/shared-groups`, async ({ request }) => {
      await record('create', request);
      return HttpResponse.json(group({ name: 'Trip' }), { status: 201 });
    }),
    http.post(`${API}/shared-groups/:id/invites`, async ({ request }) => {
      await record('invite', request);
      return HttpResponse.json(member('00000000-0000-4000-8000-00000000a003', 'Cy', { state: 'invited' }), {
        status: 201,
      });
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

describe('split arithmetic', () => {
  const base: SplitState = { mode: 'equal', members: ['a', 'b', 'c'], weights: {}, exact: {} };
  it('splits equally with the remainder to the first members, keeping the sign', () => {
    expect(previewShares(base, 100, ['a', 'b', 'c'])).toEqual({ a: 34, b: 33, c: 33 });
    expect(previewShares(base, -100, ['a', 'b', 'c'])).toEqual({ a: -34, b: -33, c: -33 });
    expect(previewShares({ ...base, members: ['c'] }, 100, ['a', 'b', 'c'])).toEqual({ c: 100 });
    expect(previewShares({ ...base, members: [] }, 100, ['a'])).toBeNull();
  });
  it('splits by shares with the largest remainders', () => {
    const s: SplitState = { ...base, mode: 'shares', members: ['a', 'b'], weights: { a: 2, b: 1 } };
    expect(previewShares(s, 100, ['a', 'b'])).toEqual({ a: 67, b: 33 });
    expect(toApiSplit(s, ['a', 'b'])).toEqual({ mode: 'shares', members: ['a', 'b'], weights: [2, 1] });
  });
  it('takes exact amounts only when they add up', () => {
    const s: SplitState = { ...base, mode: 'exact', exact: { b: 600 } };
    expect(previewShares(s, 600, ['a', 'b'])).toEqual({ b: 600 });
    expect(previewShares(s, 700, ['a', 'b'])).toBeNull();
    expect(toApiSplit(s, ['a', 'b'])).toEqual({ mode: 'exact', exact: [{ memberId: 'b', shareMinor: 600 }] });
  });
});

describe('hint sentences', () => {
  it('words each kind', () => {
    const g = { name: 'Kraków', kind: 'trip' } as Schemas['TimelineGroup'];
    expect(hintText({ groupId: GID, kind: 'per_day_high', ratio: 2 }, g)).toBe(
      'Kraków cost 2,0× per day what your other trips usually do.',
    );
    expect(hintText({ groupId: GID, kind: 'over_budget', ratio: 1.25 }, g)).toMatch(/over its budget: 1,3×/);
    expect(hintText({ groupId: GID, kind: 'category_high', category: 'Jedzenie', ratio: 2.5 }, g)).toMatch(
      /2,5× your usual per day on Jedzenie/,
    );
  });
});

describe('the Shared tab', () => {
  it('lists the timeline with years and hints, and opens a shared group', async () => {
    const user = userEvent.setup();
    renderApp('/s');
    const timeline = await screen.findByRole('region', { name: 'Timeline' });
    expect(await within(timeline).findByText(/Gdańsk/)).toBeTruthy();
    expect(screen.getByText(/2,5× your usual per day on Kultura/)).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Per year' })).getAllByRole('row')).toHaveLength(3);
    // the title bar's toggle is on
    expect(screen.getByRole('button', { name: /^Shared/ }).getAttribute('aria-pressed')).toBe('true');
    await user.click(within(timeline).getByRole('button', { name: 'Kraków' }));
    expect(await screen.findByRole('region', { name: 'Balances' })).toBeTruthy();
  });

  it('shows balances, who owes whom and records a payment with Settle up', async () => {
    const user = userEvent.setup();
    renderApp(`/s/${GID}`);
    const bal = await screen.findByRole('region', { name: 'Balances' });
    expect(within(bal).getByText(/You → Bo/).textContent).toMatch(/15[.,]00/);
    await user.click(within(bal).getByRole('button', { name: 'Settle up' }));
    const dlg = await screen.findByRole('dialog', { name: 'Settle up' });
    // pre-filled with the suggested transfer
    expect(within(dlg).getByLabelText<HTMLInputElement>(/Amount/).value).toMatch(/^15[.,]00$/);
    await user.click(within(dlg).getByRole('button', { name: 'Record payment' }));
    await waitFor(() => {
      expect(posted.find((p) => p.path === 'settlements')?.body).toMatchObject({ from: ME, to: BO, amountMinor: 1500 });
    });
    expect(await screen.findByText('Everyone is settled up.')).toBeTruthy();
  });

  it('adds an expense for someone else with an exact split', async () => {
    const user = userEvent.setup();
    renderApp(`/s/${GID}`);
    await user.click(await screen.findByRole('button', { name: 'Add expense' }));
    const dlg = await screen.findByRole('dialog', { name: 'Add an expense' });
    await user.type(within(dlg).getByLabelText(/Amount/), '60');
    await user.type(within(dlg).getByLabelText('What for'), 'Bilety');
    await user.click(within(dlg).getByRole('radio', { name: 'Exact amounts' }));
    await user.type(within(dlg).getByLabelText("Bo's amount"), '50');
    await user.click(within(dlg).getByRole('button', { name: 'Add' }));
    expect(await within(dlg).findByRole('alert')).toHaveProperty(
      'textContent',
      expect.stringMatching(/^The amounts must add up to 60[.,]00/),
    );
    await user.clear(within(dlg).getByLabelText("Bo's amount"));
    await user.type(within(dlg).getByLabelText("Bo's amount"), '60');
    expect(within(dlg).getByText('Adds up.')).toBeTruthy();
    await user.click(within(dlg).getByRole('button', { name: 'Add' }));
    await waitFor(() => {
      expect(posted.find((p) => p.path === 'expenses')?.body).toMatchObject({
        amountMinor: 6000,
        payee: 'Bilety',
        paidBy: ME,
        split: { mode: 'exact', exact: [{ memberId: BO, shareMinor: 6000 }] },
      });
    });
  });

  it('every card has a table twin', async () => {
    const user = userEvent.setup();
    renderApp(`/s/${GID}`);
    const card = await screen.findByRole('region', { name: 'Paid vs share' });
    await user.click(within(card).getByRole('button', { name: 'Show Paid vs share as a table' }));
    const twin = within(card).getByRole('region', { name: 'Paid vs share, table' });
    const rows = within(twin).getAllByRole('row');
    expect(rows[2]?.textContent).toMatch(/^Bo30[.,]00\s?\S+15[.,]00\s?\S+15[.,]00\s?\S+$/);
  });

  it('an invited user sees the members and joins', async () => {
    const user = userEvent.setup();
    detail = {
      ...detail,
      group: group({ me: { ...me, role: 'member', state: 'invited' } }),
      members: [
        { ...bo, role: 'owner' },
        { ...me, role: 'member', state: 'invited' },
      ],
      expenses: [],
      balances: [],
      transfers: [],
    };
    renderApp(`/s/${GID}`);
    const inv = await screen.findByRole('region', { name: 'Invitation' });
    expect(inv.textContent).toMatch(/Bo invited you to Kraków/);
    expect(screen.queryByRole('region', { name: 'Expenses' })).toBeNull();
    await user.click(within(inv).getByRole('button', { name: 'Join' }));
    expect(await screen.findByRole('region', { name: 'Balances' })).toBeTruthy();
    expect(posted.some((p) => p.path === 'accept')).toBe(true);
  });

  it('shares a new group with friends from the timeline', async () => {
    const user = userEvent.setup();
    renderApp('/s');
    await user.click(await screen.findByRole('button', { name: 'New shared group' }));
    const dlg = await screen.findByRole('dialog', { name: 'New shared group' });
    await user.type(within(dlg).getByLabelText('Name'), 'Trip');
    await user.click(within(dlg).getByRole('checkbox', { name: 'Cy' }));
    await user.click(within(dlg).getByRole('button', { name: 'Create' }));
    await waitFor(() => {
      expect(posted.map((p) => p.path)).toEqual(['create', 'invite']);
    });
    expect(posted[0]?.body).toMatchObject({ name: 'Trip', kind: 'trip' });
    expect(posted[1]?.body).toEqual({ friendId: '00000000-0000-4000-8000-0000000000e1' });
  });

  it('is gone when the instance switches shared groups off', async () => {
    db.features = { ...db.features, sharedGroups: false };
    renderApp('/t/all');
    await screen.findByRole('button', { name: 'Toggle sidebar' });
    expect(screen.queryByRole('button', { name: /^Shared/ })).toBeNull();
  });
});
