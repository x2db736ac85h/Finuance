import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { fmtMoney } from '../lib/format';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';

// jsdom has no layout: give the grid's scroll element a height so the virtualiser renders rows.
const clientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains('grid-wrap') ? 600 : 0;
    },
  });
});
beforeEach(() => {
  resetDb();
  db.accounts = [account({ txCount: 3 })];
  db.transactions = [
    txRow(1, { payee: 'KINO POD GWIAZDAMI' }),
    txRow(2, { payee: 'Sklep Żabka', amount: { amountMinor: 250000, currency: 'PLN' } }),
    txRow(3, { payee: 'Apteka' }),
  ];
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
  if (clientHeight) Object.defineProperty(HTMLElement.prototype, 'clientHeight', clientHeight);
});

const lastQuery = () => db.queries.at(-1);
const grid = () => screen.getByRole('region', { name: 'Transactions' });

describe('table', () => {
  it('renders the rows with formatted cells and the toolbar meta', async () => {
    renderApp('/t/all');
    expect(await screen.findByText('KINO POD GWIAZDAMI')).toBeTruthy();
    const row = screen.getByText('Sklep Żabka').closest('tr');
    expect(row?.querySelector('td.number')?.textContent).toBe(fmtMoney({ amountMinor: 250000, currency: 'PLN' }));
    expect(row?.querySelector('td.neg')).toBeNull();
    expect(screen.getByText('KINO POD GWIAZDAMI').closest('tr')?.querySelector('td.neg')).toBeTruthy();
    expect(screen.getByText(/^3 × \d+$/)).toBeTruthy();
    // the head window asks for totals; the all-accounts view has no account filter
    expect(lastQuery()?.includeTotals).toBe(true);
    expect(lastQuery()?.context.accountIds).toBeUndefined();
  });

  it('cycles the sort on header clicks: ascending, descending, off', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    const th = screen.getByRole('columnheader', { name: /Payee/ });
    await user.click(th);
    await waitFor(() => {
      expect(lastQuery()?.sort).toEqual({ column: 'payee', dir: 'asc' });
    });
    expect(th.getAttribute('aria-sort')).toBe('ascending');
    await user.click(th);
    await waitFor(() => {
      expect(lastQuery()?.sort).toEqual({ column: 'payee', dir: 'desc' });
    });
    await user.click(th);
    await waitFor(() => {
      expect(th.getAttribute('aria-sort')).toBe('none');
    });
    expect(screen.queryByText('Sort')).toBeNull();
  });

  it('filters from the column menu: condition, value checklist, chips, clear', async () => {
    const distinct: Schemas['DistinctRequest'][] = [];
    server.use(
      http.post(`${API}/transactions/distinct`, async ({ request }) => {
        distinct.push((await request.json()) as Schemas['DistinctRequest']);
        return HttpResponse.json([
          { value: 'Apteka', count: 2 },
          { value: '', count: 1 },
        ]);
      }),
    );
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    await user.click(screen.getByRole('button', { name: 'Filter and sort Payee' }));
    const menu = await screen.findByRole('dialog', { name: 'Payee column' });
    expect(await within(menu).findByText('(empty)')).toBeTruthy();
    expect(within(menu).getByText('text · 2 values')).toBeTruthy();
    expect(distinct[0]?.column).toBe('payee');

    await user.type(within(menu).getByRole('textbox', { name: 'Condition for Payee' }), 'kino');
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toEqual([{ column: 'payee', cond: 'kino' }]);
    });
    await user.click(within(menu).getByRole('checkbox', { name: /Apteka/ }));
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toEqual([{ column: 'payee', cond: 'kino', excluded: ['Apteka'] }]);
    });
    expect(screen.getByRole('columnheader', { name: /Payee/ }).className).toContain('filtered');

    await user.click(within(menu).getByRole('button', { name: 'Clear filter' }));
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toBeUndefined();
    });
  });

  it('applies the condition on Enter and closes the menu; the chip removes it', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    await user.click(screen.getByRole('button', { name: 'Filter and sort Amount' }));
    const menu = await screen.findByRole('dialog', { name: 'Amount column' });
    await user.type(within(menu).getByRole('textbox', { name: 'Condition for Amount' }), '>100{Enter}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Amount column' })).toBeNull();
    });
    expect(lastQuery()?.context.filters).toEqual([{ column: 'amount', cond: '>100' }]);
    await user.click(screen.getByRole('button', { name: /^Remove Amount/ }));
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toBeUndefined();
    });
  });

  it('shows the empty result with Clear all', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    db.transactions = [];
    await user.click(screen.getByRole('button', { name: 'Filter and sort Payee' }));
    const menu = await screen.findByRole('dialog', { name: 'Payee column' });
    await user.type(within(menu).getByRole('textbox', { name: 'Condition for Payee' }), 'zzz{Enter}');
    expect(await screen.findByText(/No rows match the current search and filters/)).toBeTruthy();
    db.transactions = [txRow(3, { payee: 'Apteka' })];
    await user.click(within(grid()).getByRole('button', { name: 'Clear all' }));
    expect(await screen.findByText('Apteka')).toBeTruthy();
    expect(lastQuery()?.context.filters).toBeUndefined();
  });

  it('selects a row on click and moves the selection with the arrow keys', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await user.click(await screen.findByText('KINO POD GWIAZDAMI'));
    const rows = () =>
      within(grid())
        .getAllByRole('row')
        .filter((r) => r.hasAttribute('data-i'));
    await waitFor(() => {
      expect(rows()[0]?.getAttribute('aria-selected')).toBe('true');
    });
    await user.keyboard('{ArrowDown}');
    await waitFor(() => {
      expect(rows()[1]?.getAttribute('aria-selected')).toBe('true');
    });
    await user.keyboard('k');
    await waitFor(() => {
      expect(rows()[0]?.getAttribute('aria-selected')).toBe('true');
    });
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(rows().some((r) => r.getAttribute('aria-selected') === 'true')).toBe(false);
    });
  });

  it('hides a column from the column menu and restores it from the hidden chip', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    await user.click(screen.getByRole('button', { name: 'Filter and sort Description' }));
    const menu = await screen.findByRole('dialog', { name: 'Description column' });
    await user.click(within(menu).getByRole('button', { name: 'Hide column' }));
    await waitFor(() => {
      expect(screen.queryByRole('columnheader', { name: /Description/ })).toBeNull();
    });
    await user.click(screen.getByRole('button', { name: 'Remove 1 hidden' }));
    expect(await screen.findByRole('columnheader', { name: /Description/ })).toBeTruthy();
  });
});
