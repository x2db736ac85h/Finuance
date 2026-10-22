import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { fmtMonthLong } from '../lib/dates';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';
import { stepped } from './PeriodPicker';
import { clampHeight } from './InsightsPane';
import { moved } from './ChartsMenu';
import { toggled } from './useChartActions';

// jsdom has no layout: give the cards, the modal and the grid a size so charts draw.
const sizes: [string, number, number][] = [
  ['card-b', 400, 220],
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

const cats: Schemas['InsightCategories'] = {
  currency: 'PLN',
  totalMinor: 70000,
  categories: [
    { category: { id: 'c1', name: 'Mieszkanie' }, spentMinor: 50000, share: 0.714, colorSlot: 1 },
    { category: { id: 'c2', name: 'Kino' }, spentMinor: 20000, share: 0.286, colorSlot: 2 },
  ],
};

beforeEach(() => {
  resetDb();
  useUi.setState({
    cards: ['sum', 'cat', 'flow', 'pace', 'payee', 'heat'],
    cardOrder: null,
    vizOpen: true,
    paneHeight: '',
    catMode: 'donut',
  });
  useViews.getState().reset();
  db.accounts = [account({ txCount: 3, firstOn: '2024-11-05', lastOn: '2025-02-20' })];
  db.transactions = [txRow(1, { payee: 'KINO' }), txRow(2, { payee: 'Apteka' }), txRow(3, { payee: 'Sklep' })];
  db.insights = {
    summary: {
      currency: 'PLN',
      count: 3,
      spentMinor: 70000,
      incomeMinor: 100000,
      netMinor: 30000,
      savingsRate: 0.3,
      days: 108,
      avgPerDayMinor: 648,
      largestExpense: { id: txRow(2).id, bookedOn: '2025-01-05', amountMinor: -50000, payee: 'WSPÓLNOTA' },
    },
    categories: cats,
    cashflow: {
      currency: 'PLN',
      months: [
        { month: '2024-12', incomeMinor: 50000, expenseMinor: 30000, count: 2, inPeriod: true },
        { month: '2025-01', incomeMinor: 50000, expenseMinor: 40000, count: 1, inPeriod: true },
      ],
    },
    payees: { currency: 'PLN', total: 1, payees: [{ payee: 'WSPÓLNOTA', spentMinor: 50000, count: 1 }] },
    daily: {
      currency: 'PLN',
      from: '2025-01-01',
      to: '2025-01-15',
      days: [{ date: '2025-01-05', spentMinor: 50000, count: 1 }],
    },
  };
});
afterEach(() => {
  server.resetHandlers();
});

const card = (title: string) => screen.getByRole('region', { name: title });
const findCard = (title: string) => screen.findByRole('region', { name: title });
const lastInsight = (name: string) => db.insightRequests.filter((r) => r.name === name).at(-1)?.body;
const lastQuery = () => db.queries.at(-1);

describe('insights pane', () => {
  it('shows the cards in their saved order and asks each endpoint with the view context', async () => {
    db.prefs = { version: 1, prefs: { cardOrder: ['heat', 'sum'] } };
    renderApp('/t/all');
    await screen.findByText('Spent · PLN');
    const titles = [...document.querySelectorAll('.card-h > span:first-child')].map((s) => s.textContent);
    expect(titles).toEqual([
      'Daily spending',
      'Summary',
      'Spending by category',
      'Cash flow by month',
      'Month pace',
      'Top payees',
    ]);
    await waitFor(() => {
      expect(new Set(db.insightRequests.map((r) => r.name))).toEqual(
        new Set(['summary', 'categories', 'cashflow', 'pace', 'payees', 'daily']),
      );
    });
    expect(lastInsight('pace')).toEqual({ context: {}, compare: 'prev' });
    expect(lastInsight('payees')).toEqual({ context: {}, top: 25, scope: 'period' });
    expect(lastInsight('daily')).toEqual({ context: {}, range: 'period' });
    expect(within(card('Spending by category')).getByText('2 categories')).toBeTruthy();
  });

  it('filters the list from a category and clears it on a second click', async () => {
    renderApp('/t/all');
    const legend = await within(await findCard('Spending by category')).findByText('Mieszkanie');
    fireEvent.click(legend);
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toEqual([{ column: 'category', cond: '=Mieszkanie' }]);
    });
    expect(lastInsight('summary')).toEqual({ context: { filters: [{ column: 'category', cond: '=Mieszkanie' }] } });
    expect(screen.getByRole('button', { name: /^Remove Category/ })).toBeTruthy();
    expect(within(card('Spending by category')).getByText('Mieszkanie').closest('.lg')?.className).toContain('on');
    fireEvent.click(within(card('Spending by category')).getByText('Mieszkanie'));
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toBeUndefined();
    });
  });

  it('focuses a month from the cash flow and a day from the calendar', async () => {
    renderApp('/t/all');
    await screen.findByText('Spent · PLN');
    const month = await waitFor(() => {
      const r = card('Cash flow by month').querySelector('rect[data-month="2025-01"]');
      expect(r).toBeTruthy();
      return r as Element;
    });
    fireEvent.click(month);
    await waitFor(() => {
      expect(lastQuery()?.context.period).toEqual({ month: '2025-01' });
    });
    expect(screen.getByRole<HTMLSelectElement>('combobox', { name: 'Period' }).value).toBe('m:2025-01');

    const day = await waitFor(() => {
      const r = card('Daily spending').querySelector('rect[data-day="2025-01-05"]');
      expect(r).toBeTruthy();
      return r as Element;
    });
    fireEvent.click(day);
    await waitFor(() => {
      expect(lastQuery()?.context.period).toEqual({ from: '2025-01-05', to: '2025-01-06' });
    });
    expect(screen.getByLabelText<HTMLInputElement>('From').value).toBe('2025-01-05');
  });

  it('locates the largest expense in the list', async () => {
    const located: Schemas['TransactionQuery'][] = [];
    server.use(
      http.post(`${API}/transactions/query`, async ({ request }) => {
        const q = (await request.json()) as Schemas['TransactionQuery'];
        db.queries.push(q);
        if (q.locateId) {
          located.push(q);
          return HttpResponse.json({ rows: db.transactions, offset: 0, locatedOffset: 1 });
        }
        return HttpResponse.json({
          rows: db.transactions,
          offset: 0,
          total: 3,
          duplicatesSkipped: 0,
          stats: { column: 'amount' },
        });
      }),
    );
    renderApp('/t/all');
    const big = await within(await findCard('Summary')).findByRole('button', { name: /500/ });
    fireEvent.click(big);
    await waitFor(() => {
      expect(viewOf(useViews.getState().views, 'all').selectedId).toBe(txRow(2).id);
    });
    expect(located[0]?.locateId).toBe(txRow(2).id);
    expect(viewOf(useViews.getState().views, 'all').selectedIndex).toBe(1);
  });
});

describe('period picker', () => {
  it('lists months and years of the data and steps through them', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const sel = await screen.findByRole<HTMLSelectElement>('combobox', { name: 'Period' });
    const months = [...sel.querySelectorAll('optgroup[label="Month"] option')].map((o) => o.textContent);
    expect(months).toEqual(['2025-02', '2025-01', '2024-12', '2024-11'].map(fmtMonthLong));
    expect([...sel.querySelectorAll('optgroup[label="Year"] option')].map((o) => o.textContent)).toEqual([
      '2025',
      '2024',
    ]);
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Previous period' }).disabled).toBe(true);

    await user.selectOptions(sel, 'm:2025-01');
    await user.click(screen.getByRole('button', { name: 'Previous period' }));
    expect(sel.value).toBe('m:2024-12');
    await waitFor(() => {
      expect(lastQuery()?.context.period).toEqual({ month: '2024-12' });
    });
    // stepping past the data keeps an option for the period shown
    await user.click(screen.getByRole('button', { name: 'Previous period' }));
    await user.click(screen.getByRole('button', { name: 'Previous period' }));
    expect(sel.value).toBe('m:2024-10');

    await user.selectOptions(sel, 'y:2024');
    await user.click(screen.getByRole('button', { name: 'Next period' }));
    expect(sel.value).toBe('y:2025');
  });

  it('starts a custom range from the data and keeps it inclusive', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const sel = await screen.findByRole<HTMLSelectElement>('combobox', { name: 'Period' });
    await user.selectOptions(sel, 'custom');
    const from = screen.getByLabelText<HTMLInputElement>('From');
    expect(from.value).toBe('2024-11-05');
    expect(screen.getByLabelText<HTMLInputElement>('To').value).toBe('2025-02-20');
    await waitFor(() => {
      expect(lastQuery()?.context.period).toEqual({ from: '2024-11-05', to: '2025-02-21' });
    });
    fireEvent.change(from, { target: { value: '2025-01-01' } });
    await waitFor(() => {
      expect(lastQuery()?.context.period).toEqual({ from: '2025-01-01', to: '2025-02-21' });
    });
  });

  it('steps months, years and custom ranges by their length', () => {
    expect(stepped('m:2025-01', null, -1)).toEqual({ period: 'm:2024-12' });
    expect(stepped('y:2024', null, 1)).toEqual({ period: 'y:2025' });
    expect(stepped('custom', { from: '2025-01-01', to: '2025-01-10' }, 1)).toEqual({
      period: 'custom',
      custom: { from: '2025-01-11', to: '2025-01-20' },
    });
    expect(stepped('l30', null, 1)).toBeNull();
    expect(stepped('all', null, 1)).toBeNull();
  });
});

describe('charts menu, collapse and resize', () => {
  it('hides and reorders cards, saved in the preferences', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Spent · PLN');
    await user.click(screen.getByRole('button', { name: 'Charts' }));
    const menu = await screen.findByRole('dialog', { name: 'Charts' });
    await user.click(within(menu).getByRole('checkbox', { name: 'Summary' }));
    expect(screen.queryByRole('region', { name: 'Summary' })).toBeNull();
    expect(useUi.getState().cards).not.toContain('sum');
    await user.click(within(menu).getByRole('button', { name: 'Move Daily spending up' }));
    expect(useUi.getState().cardOrder).toEqual(['sum', 'cat', 'flow', 'pace', 'heat', 'payee']);
    const titles = [...document.querySelectorAll('.card-h > span:first-child')].map((s) => s.textContent);
    expect(titles.slice(-2)).toEqual(['Daily spending', 'Top payees']);
    expect(within(menu).getByRole<HTMLButtonElement>('button', { name: 'Move Summary up' }).disabled).toBe(true);
  });

  it('collapses the pane and resizes it within limits', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Spent · PLN');
    const edge = screen.getByRole('separator', { name: 'Resize insights' });
    fireEvent.pointerDown(edge, { button: 0, clientY: 300 });
    act(() => {
      window.dispatchEvent(new PointerEvent('pointermove', { clientY: 420 }));
      window.dispatchEvent(new PointerEvent('pointerup'));
    });
    expect(useUi.getState().paneHeight).toBe('170px'); // jsdom: offsetHeight 0 → the minimum
    fireEvent.doubleClick(edge);
    expect(useUi.getState().paneHeight).toBe('');
    await user.click(screen.getByRole('button', { name: 'Hide insights' }));
    expect(useUi.getState().vizOpen).toBe(false);
    expect(clampHeight(50, 1000)).toBe(170);
    expect(clampHeight(900, 1000)).toBe(750);
    expect(clampHeight(400, 1000)).toBe(400);
  });
});

describe('chart window', () => {
  it('opens a card larger, switches to the table, steps with the arrows and saves range settings', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Spent · PLN');
    await user.click(within(card('Top payees')).getByRole('button', { name: /Open larger/ }));
    let dlg = await screen.findByRole('dialog', { name: 'Top payees' });
    expect(within(dlg).getByText('top 1 of 1')).toBeTruthy();
    await user.click(within(dlg).getByRole('button', { name: 'Table' }));
    expect(within(dlg).getByRole('columnheader', { name: 'Average' })).toBeTruthy();
    expect(within(dlg).getAllByRole('row')).toHaveLength(2);

    await user.selectOptions(within(dlg).getByRole('combobox', { name: 'Show' }), '10');
    expect(useUi.getState().cfg.payeeTop).toBe(10);
    await waitFor(() => {
      expect(lastInsight('payees')).toEqual({ context: {}, top: 10, scope: 'period' });
    });

    // arrows step only when no select has focus (it would change the select instead)
    await user.click(within(dlg).getByText('Top payees'));
    await user.keyboard('{ArrowRight}');
    dlg = await screen.findByRole('dialog', { name: 'Daily spending' });
    await user.selectOptions(within(dlg).getByRole('combobox', { name: 'Range' }), '3');
    expect(useUi.getState().cfg.heatRange).toBe(3);
    await waitFor(() => {
      expect(lastInsight('daily')).toEqual({ context: {}, range: '3m' });
    });
    await user.click(within(dlg).getByText('Daily spending'));
    await user.keyboard('{ArrowRight}');
    expect(await screen.findByRole('dialog', { name: 'Summary' })).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Previous chart (←)' }));
    expect(await screen.findByRole('dialog', { name: 'Daily spending' })).toBeTruthy();
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });
});

describe('chart actions', () => {
  it('toggles a condition like the legacy toggleFilter', () => {
    expect(toggled({}, 'payee', 'kino')).toEqual({ payee: { cond: 'kino', excluded: [] } });
    expect(toggled({ payee: { cond: 'kino', excluded: [] } }, 'payee', 'kino')).toEqual({});
    expect(toggled({ payee: { cond: 'kino', excluded: ['A'] } }, 'payee', 'kino')).toEqual({
      payee: { cond: '', excluded: ['A'] },
    });
    expect(toggled({ payee: { cond: 'kino', excluded: ['A'] } }, 'payee', 'apteka')).toEqual({
      payee: { cond: 'apteka', excluded: ['A'] },
    });
    expect(moved(['a', 'b', 'c'], 'c', 0)).toEqual(['c', 'a', 'b']);
    expect(moved(['a', 'b', 'c'], 'a', 9)).toEqual(['b', 'c', 'a']);
  });
});
