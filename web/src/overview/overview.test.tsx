import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Schemas } from '../api/client';
import { DEFAULT_CFG } from '../stores/prefs';
import type { ChartEnv } from '../charts/types';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { account, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';
import { budgetsParts, inputText } from './Budgets';
import { sentence } from './Highlights';
import { monthsOf } from './OverviewPage';
import { sides, topN } from './Sankey';
import { savingsRates } from './Savings';
import { weekdayAverages } from './Weekday';
import { fireParts, yieldParts } from './Investments';

type Data = Schemas['OverviewData'];

// jsdom has no layout: give the card bodies and the chart window a size so charts draw.
const sizes: [string, number, number][] = [
  ['card-b', 500, 240],
  ['modal-chart', 900, 500],
  ['grid-wrap', 1000, 600],
];
const saved = ['clientWidth', 'clientHeight'].map(
  (p) => [p, Object.getOwnPropertyDescriptor(HTMLElement.prototype, p)] as const,
);
beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
  for (const [prop, i] of [
    ['clientWidth', 1],
    ['clientHeight', 2],
  ] as const) {
    Object.defineProperty(HTMLElement.prototype, prop, {
      configurable: true,
      get(this: HTMLElement) {
        const s = sizes.find(([c]) => this.classList.contains(c));
        return s ? s[i] : 0;
      },
    });
  }
});
afterAll(() => {
  server.close();
  for (const [p, d] of saved) if (d) Object.defineProperty(HTMLElement.prototype, p, d);
});
afterEach(() => {
  server.resetHandlers();
});

const ACC = '00000000-0000-4000-8000-0000000000a1';
const month = (m: string, inc: number, exp: number) => ({ month: m, incomeMinor: inc, expenseMinor: exp, count: 5 });
const ref = (id: string, name: string) => ({ id, name });

/** March 2025 in progress (17 of 31 days), three earlier months. */
export function overviewData(over: Partial<Data> = {}): Data {
  const ppkRow = {
    name: 'PPK',
    type: '',
    ppk: true,
    investedMinor: 0,
    valueMinor: 0,
    incomeMinor: 0,
    allocationShare: 0,
  };
  return {
    month: '2025-03',
    prevMonth: '2025-02',
    firstMonth: '2024-12',
    lastMonth: '2025-03',
    dataMax: '2025-03-17',
    partial: true,
    elapsedDays: 17,
    daysInMonth: 31,
    current: month('2025-03', 932300, 515182),
    previous: month('2025-02', 958000, 556962),
    prevToDateMinor: 420946,
    projectedMinor: 939450,
    avgMonths: 3,
    months: [
      month('2024-12', 900000, 600000),
      month('2025-01', 950000, 500000),
      month('2025-02', 958000, 556962),
      month('2025-03', 932300, 515182),
    ],
    categories: [
      {
        category: ref('c1', 'Mieszkanie'),
        name: 'Mieszkanie',
        curMinor: 285000,
        prevMinor: 285000,
        avg3Minor: 285000,
        budgetMinor: 285000,
        budgetIsDefault: true,
        status: 'ok',
      },
      {
        category: ref('c2', 'Transport'),
        name: 'Transport',
        curMinor: 42900,
        prevMinor: 30000,
        avg3Minor: 35000,
        budgetMinor: 50000,
        budgetIsDefault: false,
        status: 'ahead',
      },
      {
        name: '(uncategorized)',
        curMinor: 9000,
        prevMinor: 0,
        avg3Minor: 4000,
        budgetMinor: 4000,
        budgetIsDefault: true,
        status: 'over',
      },
    ],
    incomeSources: [{ payee: 'PRACODAWCA', spentMinor: 932300, count: 1 }],
    colorSlots: [
      { category: ref('c1', 'Mieszkanie'), slot: 1 },
      { category: ref('c2', 'Transport'), slot: 2 },
    ],
    highlights: [
      {
        kind: 'projection',
        tone: 'info',
        amountMinor: 939450,
        otherMinor: 0,
        pct: 0,
        avgPct: 0,
        days: 17,
        of: 31,
        months: 0,
        count: 0,
        category: '',
        payee: '',
        sameDays: false,
      },
    ],
    recurring: [
      {
        payee: 'WSPÓLNOTA MIESZKANIOWA',
        category: 'Mieszkanie',
        medianMinor: 285000,
        last: '2025-03-02',
        next: '2025-04-02',
        active: true,
        yearlyMinor: 3420000,
        count: 15,
      },
    ],
    largest: [
      {
        id: txRow(7).id,
        bookedOn: '2025-03-02',
        payee: 'WSPÓLNOTA MIESZKANIOWA',
        payeeGroup: 'WSPÓLNOTA MIESZKANIOWA',
        category: ref('c1', 'Mieszkanie'),
        amountMinor: -285000,
      },
    ],
    weekday: [
      { totalMinor: 1001, days: 2 }, // 500.5 → 501 (half away from zero)
      { totalMinor: 900, days: 3 },
      { totalMinor: 0, days: 0 },
      { totalMinor: 0, days: 2 },
      { totalMinor: 30000, days: 2 },
      { totalMinor: 100, days: 2 },
      { totalMinor: 100, days: 2 },
    ],
    balance: {
      openingMinor: 125000,
      nowMinor: 4418254,
      points: [
        { date: '2024-12-01', balanceMinor: 3000000 },
        { date: '2025-03-17', balanceMinor: 4418254 },
      ],
    },
    investment: {
      rows: [ppkRow],
      investedMinor: 0,
      valueMinor: 0,
      incomeMinor: 0,
      ppkPaidInMinor: 0,
      ppkGainMinor: 0,
      count: 0,
    },
    yieldOnCost: { positions: [], investedMinor: 0, incomeMinor: 0, detectedMinor: 0, usesDetected: false },
    fire: {
      months: 4,
      annualExpensesMinor: 6516432,
      yearlySavingsMinor: 3000000,
      fiNumberMinor: 162910800,
      assetsMinor: 0,
      swrBp: 400,
      realReturnBp: 500,
      progress: 0,
      years: 27,
      reachable: true,
    },
    ...over,
  };
}

function env(over: Partial<ChartEnv> = {}): ChartEnv {
  return {
    viewId: 'all',
    w: 500,
    h: 240,
    big: false,
    cfg: DEFAULT_CFG,
    catMode: 'donut',
    period: 'all',
    filters: {},
    ovMonth: null,
    actions: {
      toggleFilter: vi.fn(),
      setPeriod: vi.fn(),
      locate: vi.fn(),
      setCatMode: vi.fn(),
      setOvMonth: vi.fn(),
      showTable: vi.fn(),
    },
    ready: true,
    ...over,
  };
}

beforeEach(() => {
  resetDb();
  useUi.setState({ mode: 'ov' });
  useUi.getState().setCfg({ ovMonths: 12 });
  useViews.getState().reset();
  db.accounts = [account({ txCount: 3, firstOn: '2024-12-01', lastOn: '2025-03-17' })];
  db.transactions = [txRow(1), txRow(7)];
  db.overview = { currency: 'PLN', data: overviewData() };
});

const card = (title: string) => screen.getByRole('region', { name: title });

describe('overview page', () => {
  it('asks for the view without its period and shows the KPIs and the twelve cards', async () => {
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    expect(db.overviewRequests.at(-1)).toEqual({ context: {}, months: 12 });
    act(() => {
      useViews.getState().patch('all', { period: 'm:2025-01' });
    });
    act(() => {
      useViews.getState().patch('all', { search: 'kino' });
    });
    await waitFor(() => {
      expect(db.overviewRequests.at(-1)).toEqual({ context: { search: 'kino' }, months: 12 });
    });
    // the period alone never asks again
    expect(db.overviewRequests.filter((r) => !r.context.search)).toHaveLength(1);
    const titles = [...document.querySelectorAll('.ov-card .card-h > span:first-child')].map((s) => s.textContent);
    expect(titles).toEqual([
      'Highlights',
      'Cash flow',
      'Budgets',
      'Recurring payments',
      'FI/RE number',
      'Yield on cost (PLN)',
      'Investment',
      'Money flow',
      'Largest expenses',
      'Savings rate',
      'Spending by weekday',
      'Account balance',
    ]);
    const spending = screen.getByRole('group', { name: 'Spending · PLN' });
    expect(spending.textContent).toContain('▲ 22% vs Feb (same days)');
    expect(screen.getByRole('group', { name: 'Net saved · PLN' }).textContent).toContain('+4,171.18savings rate 45%');
    expect(screen.getByRole('group', { name: 'Projected spend · PLN' }).textContent).toContain(
      '17 of 31 days · avg 303/day',
    );
    expect(screen.getByText(/Month in progress — data until Mar 17, 2025/)).toBeTruthy();
    // highlights have no larger view; every other card has
    expect(within(card('Highlights')).queryByTitle(/Open larger/)).toBeNull();
    expect(within(card('Budgets')).getByTitle(/Open larger/)).toBeTruthy();
    // a filter note with Clear
    expect(screen.getByText(/1 filter from the list applied/)).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(viewOf(useViews.getState().views, 'all').search).toBe('');
  });

  it('a complete month shows the average per day; the month selector and arrows pick months', async () => {
    db.overview = {
      currency: 'PLN',
      data: overviewData({ month: '2025-02', prevMonth: '2025-01', partial: false, elapsedDays: 28, daysInMonth: 28 }),
    };
    renderApp('/o/all');
    expect((await screen.findByRole('group', { name: 'Avg / day · PLN' })).textContent).toContain('over 28 days');
    const sel = screen.getByRole('combobox', { name: 'Month' });
    expect([...sel.querySelectorAll('option')].map((o) => o.value)).toEqual([
      '2025-03',
      '2025-02',
      '2025-01',
      '2024-12',
    ]);
    await userEvent.selectOptions(sel, '2024-12');
    await waitFor(() => {
      expect(db.overviewRequests.at(-1)?.month).toBe('2024-12');
    });
    await userEvent.click(screen.getByRole('button', { name: 'Next month' }));
    expect(viewOf(useViews.getState().views, 'all').ovMonth).toBe('2025-03');
  });

  it('a cash-flow or savings bar selects its month', async () => {
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    fireEvent.click(card('Cash flow').querySelector('rect[data-month="2025-01"]') as Element);
    expect(viewOf(useViews.getState().views, 'all').ovMonth).toBe('2025-01');
    fireEvent.click(card('Savings rate').querySelector('rect[data-month="2024-12"]') as Element);
    expect(viewOf(useViews.getState().views, 'all').ovMonth).toBe('2024-12');
  });

  it('a Sankey category filters the list and opens the table; Saved does nothing', async () => {
    const { router } = renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    const sankey = card('Money flow');
    expect(sankey.querySelector('[data-cat="Saved"]')).toBeNull();
    fireEvent.click(sankey.querySelector('rect[data-cat="Transport"]') as Element);
    expect(viewOf(useViews.getState().views, 'all').filters).toEqual({
      category: { cond: '=Transport', excluded: [] },
    });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/all');
    });
  });

  it('uncategorised spending filters with "="', async () => {
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    fireEvent.click(card('Money flow').querySelector('rect[data-cat="(uncategorized)"]') as Element);
    expect(viewOf(useViews.getState().views, 'all').filters).toEqual({ category: { cond: '=', excluded: [] } });
  });

  it('a largest expense opens the table at its row, leaving a period that hides it', async () => {
    const { router } = renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    act(() => {
      useViews.getState().patch('all', { period: 'm:2024-01' });
    });
    // the first query (with the period) doesn't find it; the second (all time) does
    let n = 0;
    const { http, HttpResponse } = await import('msw');
    server.use(
      http.post(`${window.location.origin}/api/v1/transactions/query`, async ({ request }) => {
        const body = (await request.json()) as Schemas['TransactionQuery'];
        db.queries.push(body);
        n++;
        const found = body.locateId && !body.context.period;
        return HttpResponse.json({ rows: [], ...(found ? { locatedOffset: 1 } : {}) });
      }),
    );
    await userEvent.click(within(card('Largest expenses')).getByText('WSPÓLNOTA MIESZKANIOWA'));
    await waitFor(() => {
      expect(viewOf(useViews.getState().views, 'all').selectedId).toBe(txRow(7).id);
    });
    expect(n).toBeGreaterThanOrEqual(2);
    expect(viewOf(useViews.getState().views, 'all').period).toBe('all');
    expect(router.state.location.pathname).toBe('/t/all');
  });

  it('a budget is changed in place; emptying it goes back to the default; uncategorised is read-only', async () => {
    renderApp('/o/all');
    const input = await screen.findByRole('textbox', { name: 'Budget for Transport' });
    expect((input as HTMLInputElement).value).toBe('500');
    await userEvent.clear(input);
    await userEvent.type(input, '650,5{Enter}');
    await waitFor(() => {
      expect(db.budgetPuts).toEqual([{ categoryId: 'c2', amountMinor: 65050 }]);
    });
    const housing = screen.getByRole('textbox', { name: 'Budget for Mieszkanie' });
    await userEvent.clear(housing);
    fireEvent.blur(housing);
    // already the default: nothing to send, the value comes back
    expect(db.budgetPuts).toHaveLength(1);
    expect((housing as HTMLInputElement).value).toBe('2850');
    const transport = screen.getByRole('textbox', { name: 'Budget for Transport' });
    await userEvent.clear(transport);
    fireEvent.blur(transport);
    await waitFor(() => {
      expect(db.budgetPuts.at(-1)).toEqual({ categoryId: 'c2', amountMinor: null });
    });
    expect(screen.queryByRole('textbox', { name: 'Budget for (uncategorized)' })).toBeNull();
    // the card refreshes after a change
    expect(db.overviewRequests.length).toBeGreaterThan(1);
  });

  it('the opening balance is an input in an account view and the read-only sum in all accounts', async () => {
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    expect(within(card('Account balance')).queryByRole('textbox')).toBeNull();
    expect(within(card('Account balance')).getByText('Opening 1,250')).toBeTruthy();
  });

  it('saves a new opening balance with the account’s other fields', async () => {
    db.accounts = [account({ txCount: 3, numberHint: '1234' })];
    renderApp(`/o/${ACC}`);
    const input = await screen.findByRole('textbox', { name: 'Opening balance' });
    expect((input as HTMLInputElement).value).toBe('1250');
    await userEvent.clear(input);
    await userEvent.type(input, '-80.5{Enter}');
    await waitFor(() => {
      expect(db.accountPatches).toEqual([
        {
          id: ACC,
          body: { name: 'Konto', currency: 'PLN', numberHint: '1234', archived: false, openingBalanceMinor: -8050 },
        },
      ]);
    });
    expect(db.overviewRequests.at(-1)?.context).toEqual({ accountIds: [ACC] });
  });

  it('edits investments in the larger view and saves them after a pause', async () => {
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    await userEvent.click(within(card('Investment')).getByTitle(/Open larger/));
    const dialog = await screen.findByRole('dialog');
    const own = await within(dialog).findByRole('textbox', { name: 'Your contributions' });
    await userEvent.clear(own);
    await userEvent.type(own, '1000{Enter}');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add position' }));
    await waitFor(
      () => {
        expect(db.investmentPuts).toHaveLength(1);
      },
      { timeout: 2000 },
    );
    const put = db.investmentPuts[0];
    expect(put?.version).toBe(0);
    expect(put?.ppk.employeeMinor).toBe(100000);
    expect(put?.positions.map((p) => p.name)).toEqual(['New position']);
    // the next edit carries the new version
    const name = within(dialog).getByRole('textbox', { name: 'Name' });
    await userEvent.clear(name);
    await userEvent.type(name, 'ETF World{Enter}');
    await waitFor(
      () => {
        expect(db.investmentPuts).toHaveLength(2);
      },
      { timeout: 2000 },
    );
    expect(db.investmentPuts[1]?.version).toBe(1);
    expect(db.investmentPuts[1]?.positions[0]?.name).toBe('ETF World');
  });

  it('a stale save shows the stored document', async () => {
    db.investments = { ...db.investments, version: 3, swrBp: 350 };
    renderApp('/o/all');
    await screen.findByText('Projected spend · PLN');
    await userEvent.click(within(card('FI/RE number')).getByTitle(/Open larger/));
    const dialog = await screen.findByRole('dialog');
    const swr = await within(dialog).findByRole('textbox', { name: 'Safe withdrawal rate %' });
    expect((swr as HTMLInputElement).value).toBe('3.5');
    db.investments = { ...db.investments, version: 4, swrBp: 300 }; // another window saved meanwhile
    await userEvent.clear(swr);
    await userEvent.type(swr, '4{Enter}');
    await waitFor(
      () => {
        expect(within(dialog).getByRole<HTMLInputElement>('textbox', { name: 'Safe withdrawal rate %' }).value).toBe(
          '3',
        );
      },
      { timeout: 2000 },
    );
    expect(await screen.findByText(/changed in another window/)).toBeTruthy();
  });

  it('without dated rows: the filters message with Clear', async () => {
    db.overview = { currency: 'PLN' };
    renderApp('/o/all');
    await screen.findByText(/needs transactions with a date/);
    act(() => {
      useViews.getState().patch('all', { filters: { payee: { cond: 'nothing', excluded: [] } } });
    });
    expect(await screen.findByText(/No transactions match the filters from the list/)).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(viewOf(useViews.getState().views, 'all').filters).toEqual({});
  });
});

describe('overview cards (fixed data)', () => {
  it('writes every highlight as the legacy sentence', () => {
    const h = (o: Partial<Schemas['Highlight']>): Schemas['Highlight'] => ({
      kind: 'noIncome',
      tone: 'info',
      amountMinor: 0,
      otherMinor: 0,
      pct: 0,
      avgPct: 0,
      days: 0,
      of: 0,
      months: 0,
      count: 0,
      category: '',
      payee: '',
      sameDays: false,
      ...o,
    });
    const text = (x: Schemas['Highlight']) => render(<p>{sentence(x, 'PLN')}</p>).container.textContent;
    expect(text(h({ kind: 'projection', amountMinor: 939450, days: 17, of: 31 }))).toBe(
      'Projected month-end spending 9,394.50 PLN — 17 of 31 days in.',
    );
    expect(
      text(
        h({
          kind: 'vsPrev',
          tone: 'up',
          pct: 22.4,
          amountMinor: 515182,
          otherMinor: 420946,
          month: '2025-02',
          sameDays: true,
        }),
      ),
    ).toBe('Spending is up 22% vs Feb at the same point (5,151.82 PLN vs 4,209.46 PLN).');
    expect(text(h({ kind: 'vsAverage', tone: 'down', months: 3, amountMinor: 1000 }))).toBe(
      'Below your 3-month average by 10.00 PLN.',
    );
    expect(text(h({ kind: 'topCategory', category: 'Mieszkanie', pct: 55.3 }))).toBe(
      'Mieszkanie is the biggest category — 55% of spending.',
    );
    expect(text(h({ kind: 'categoryUp', category: 'Kino', amountMinor: 6001 }))).toBe(
      'Kino is 60.01 PLN above its usual level.',
    );
    expect(text(h({ kind: 'categoryDown', category: 'Kino', amountMinor: 6001 }))).toBe(
      'Kino is 60.01 PLN below its usual level.',
    );
    expect(text(h({ kind: 'savings', pct: 44.7, avgPct: 33.2, months: 12 }))).toBe(
      'You kept 45% of income (12-month average 33%).',
    );
    expect(text(h({ kind: 'overspend', amountMinor: 5000 }))).toBe('You spent 50.00 PLN more than you earned.');
    expect(text(h({ kind: 'noIncome' }))).toBe('No income recorded this month.');
    expect(text(h({ kind: 'noSpendDays', days: 1 }))).toBe('1 no-spend day so far this month.');
    expect(text(h({ kind: 'noSpendDays', days: 4 }))).toBe('4 no-spend days so far this month.');
    expect(text(h({ kind: 'recurring', count: 3, amountMinor: 319028, otherMinor: 3828336 }))).toBe(
      '3 recurring payments ≈ 3,190.28 PLN/month (38,283.36 PLN/year).',
    );
    expect(text(h({ kind: 'largest', amountMinor: -285000, payee: 'WSPÓLNOTA' }))).toBe(
      'Largest expense: -2,850.00 PLN at WSPÓLNOTA.',
    );
  });

  it('budget twin, total row and statuses', () => {
    const p = budgetsParts(overviewData(), 'PLN');
    expect(p.sub).toBe('default = 3-month average');
    expect(p.table?.rows).toEqual([
      ['Mieszkanie', '2,850.00 PLN', '2,850.00 PLN', '0.00 PLN', 'On track'],
      ['Transport', '429.00 PLN', '500.00 PLN', '71.00 PLN', 'Ahead of pace'],
      ['(uncategorized)', '90.00 PLN', '40.00 PLN', '-50.00 PLN', 'Over budget'],
    ]);
    const { container } = render(<QueryClientProvider client={new QueryClient()}>{p.body}</QueryClientProvider>);
    const rows = [...container.querySelectorAll('.br')].map((r) => r.querySelector('.bst')?.textContent);
    expect(rows).toEqual(['✓ 21 left', '✓ 0 left', '! 71 left', '▲ over 50']);
    expect(container.querySelectorAll('.bpace')).toHaveLength(4); // a month in progress
    expect(budgetsParts(overviewData({ categories: [] }), 'PLN').table).toBeNull();
    expect(budgetsParts(overviewData({ avgMonths: 0 }), 'PLN').sub).toBe('set your own limits');
  });

  it('input text of an amount', () => {
    expect(inputText(285000, 'PLN')).toBe('2850');
    expect(inputText(8550, 'PLN')).toBe('85.5');
    expect(inputText(-8050, 'PLN')).toBe('-80.5');
    expect(inputText(400, 'PLN')).toBe('4');
  });

  it('Sankey sides: top N, the rest as Other, and the balance', () => {
    expect(
      topN(
        [
          { k: 'a', v: 5 },
          { k: 'b', v: 3 },
          { k: 'c', v: 2 },
        ],
        2,
        'Other',
      ),
    ).toEqual([
      { k: 'a', v: 5 },
      { k: 'b', v: 3 },
      { k: 'Other', v: 2, other: true },
    ]);
    const { L, R, T } = sides(overviewData(), false);
    expect(L.map((n) => n.k)).toEqual(['PRACODAWCA']);
    expect(R.map((n) => [n.k, n.v, n.filter ?? false])).toEqual([
      ['Mieszkanie', 285000, true],
      ['Transport', 42900, true],
      ['(uncategorized)', 9000, true],
      ['Saved', 932300 - 515182, false],
    ]);
    expect(T).toBe(932300);
    const deficit = sides(overviewData({ current: month('2025-03', 1000, 5000) }), false);
    expect(deficit.L.at(-1)).toMatchObject({ k: 'From savings', v: 4000 });
  });

  it('savings rates are capped; weekday averages round half away from zero', () => {
    expect(savingsRates([month('a', 100, 50), month('b', 100, 500), month('c', 0, 10)])).toEqual([50, -100, null]);
    expect(weekdayAverages(overviewData())).toEqual([501, 300, 0, 0, 15000, 50, 50]);
  });

  it('months of the selector, newest first', () => {
    expect(monthsOf({ firstMonth: '2024-11', lastMonth: '2025-02' })).toEqual([
      '2025-02',
      '2025-01',
      '2024-12',
      '2024-11',
    ]);
  });

  it('yield on cost: empty, detected income, positions', () => {
    const empty = yieldParts(
      overviewData({
        yieldOnCost: { positions: [], investedMinor: 0, incomeMinor: 0, detectedMinor: 97755, usesDetected: true },
      }),
    );
    expect(empty.table?.rows).toEqual([['Total', '0.00 PLN', '0.00 PLN', '–']]);
    expect(render(<>{empty.body}</>).container.textContent).toContain('(12 months): 977.55 PLN — used as income');
    const y = yieldParts(
      overviewData({
        yieldOnCost: {
          positions: [{ id: 'p1', name: 'Bonds', investedMinor: 2000000, incomeMinor: 136000, yield: 0.068 }],
          investedMinor: 2000000,
          incomeMinor: 136000,
          detectedMinor: 0,
          usesDetected: false,
          total: 0.068,
        },
      }),
    );
    expect(y.sub).toBe('6.80% total');
    expect(y.table?.rows.at(-1)).toEqual(['Total', '20,000.00 PLN', '1,360.00 PLN', '6.80%']);
  });

  it('FI/RE: progress, years and the unreachable case', () => {
    const f = fireParts(overviewData(), 'PLN', env());
    expect(f.sub).toBe('25 × annual expenses');
    expect(f.table?.rows).toContainEqual(['Years to FI', '27']);
    const text = render(<>{f.body}</>).container.textContent;
    expect(text).toContain('≈ 27 years to FI saving 30,000.00 PLN/year at 5.0% real return.');
    expect(text).toContain('(annualised from 4 months)');
    const u = fireParts(overviewData({ fire: { ...overviewData().fire, reachable: false, years: 100 } }), 'PLN', env());
    expect(u.table?.rows).toContainEqual(['Years to FI', '–']);
    expect(render(<>{u.body}</>).container.textContent).toContain('Not reachable at the current savings rate.');
  });
});
