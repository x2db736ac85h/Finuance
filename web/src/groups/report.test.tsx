// The group window and group budgets (PLAN 15.9).
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Schemas } from '../api/client';
import { useSelection } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';

const ITALY = '00000000-0000-4000-8000-000000000001';
const cat = (name: string) => ({ id: `cat-${name}`, name });

let exports: Schemas['ExportRequest'][] = [];
const clientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains('grid-wrap') ? 600 : 0;
    },
  });
  URL.createObjectURL = vi.fn(() => 'blob:x');
  URL.revokeObjectURL = vi.fn();
});
afterAll(() => {
  server.close();
  if (clientHeight) Object.defineProperty(HTMLElement.prototype, 'clientHeight', clientHeight);
});
beforeEach(() => {
  resetDb();
  exports = [];
  useViews.getState().reset();
  useSelection.getState().clear();
  db.accounts = [account({ txCount: 5 })];
  db.groups = [{ id: ITALY, name: 'Italy', color: 2, budgetMinor: 300000 }];
  const g = { group: { id: ITALY, name: 'Italy' } };
  db.transactions = [
    txRow(1, {
      ...g,
      payee: 'Pizzeria',
      bookedOn: '2025-06-01',
      amount: { amountMinor: -12000, currency: 'PLN' },
      category: cat('Jedzenie'),
    }),
    txRow(2, {
      ...g,
      payee: 'Hotel',
      bookedOn: '2025-06-01',
      amount: { amountMinor: -200000, currency: 'PLN' },
      category: cat('Noclegi'),
    }),
    txRow(3, {
      ...g,
      payee: 'Pizzeria',
      bookedOn: '2025-06-04',
      amount: { amountMinor: -8000, currency: 'PLN' },
      category: cat('Jedzenie'),
    }),
    txRow(4, { ...g, payee: 'Zwrot', bookedOn: '2025-06-05', amount: { amountMinor: 5000, currency: 'PLN' } }),
    txRow(5, { payee: 'Apteka' }),
  ];
  server.use(
    http.get(`${API}/categories`, () => HttpResponse.json([])),
    http.post(`${API}/transactions/export`, async ({ request }) => {
      exports.push((await request.json()) as Schemas['ExportRequest']);
      return new HttpResponse('﻿Date;Payee\r\n', {
        headers: { 'Content-Type': 'text/csv', 'Content-Disposition': 'attachment; filename="finuance-italy.csv"' },
      });
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
});

async function openMenu(user: ReturnType<typeof userEvent.setup>) {
  renderApp('/t/all');
  await screen.findByText('Apteka');
  await user.click(screen.getByRole('button', { name: /Groups/ }));
  return screen.findByRole('dialog', { name: 'Groups' });
}

describe('group window', () => {
  it('the menu shows the budget left; the window shows totals, span, per day, categories and payees', async () => {
    const user = userEvent.setup();
    const menu = await openMenu(user);
    // 3,000.00 budget − 2,200.00 spent
    expect(within(menu).getByText(/800 left/)).toBeTruthy();
    await user.click(within(menu).getByRole('button', { name: 'Report for Italy' }));
    const win = await screen.findByRole('dialog', { name: 'Italy' });
    await waitFor(() => {
      expect(within(win).getByRole('group', { name: 'Span' }).textContent).toContain('5 days');
    });
    expect(within(win).getByRole('group', { name: 'Span' }).textContent).toContain('01.06.2025 – 05.06.2025');
    expect(within(win).getByRole('group', { name: 'Spent' }).textContent).toMatch(/2,200\.00/);
    expect(within(win).getByRole('group', { name: 'Income' }).textContent).toMatch(/50\.00/);
    // 220,000 / 5 days
    expect(within(win).getByRole('group', { name: 'Per day' }).textContent).toMatch(/440\.00/);
    expect(within(win).getByRole('region', { name: 'Budget' }).textContent).toMatch(/800 left/);
    // two days with money out in the per-day chart; the donut legend; payees with counts
    expect(win.querySelectorAll('rect[data-date]')).toHaveLength(2);
    const legend = [...win.querySelectorAll('.lg .nm')].map((n) => n.textContent);
    expect(legend).toEqual(['Noclegi', 'Jedzenie']);
    const payees = within(within(win).getByRole('list', { name: 'Top payees' })).getAllByRole('listitem');
    expect(payees.map((p) => p.textContent)).toEqual([
      expect.stringMatching(/^Hotel · 1×2,000/),
      expect.stringMatching(/^Pizzeria · 2×200/),
    ]);
  });

  it('exports the group by id and shows it in the table', async () => {
    const user = userEvent.setup();
    const menu = await openMenu(user);
    await user.click(within(menu).getByRole('button', { name: 'Report for Italy' }));
    const win = await screen.findByRole('dialog', { name: 'Italy' });
    await user.click(within(win).getByRole('button', { name: 'Export CSV' }));
    await waitFor(() => {
      expect(exports).toHaveLength(1);
    });
    expect(exports[0]).toMatchObject({
      context: { groupId: ITALY, includeTransfers: true },
      sort: { column: 'bookedOn', dir: 'asc' },
      view: 'italy',
    });
    expect(await screen.findByText('Exported 4 rows')).toBeTruthy();
    await user.click(within(win).getByRole('button', { name: 'Show in table' }));
    expect(viewOf(useViews.getState().views, 'all').filters.group?.cond).toBe('=Italy');
    expect(screen.queryByRole('dialog', { name: 'Italy' })).toBeNull();
  });

  it('over budget shows how much; the budget is set, kept and removed in the group form', async () => {
    db.groups = [{ id: ITALY, name: 'Italy', color: 2, budgetMinor: 200000 }];
    const user = userEvent.setup();
    let menu = await openMenu(user);
    expect(within(menu).getByText(/over 200/)).toBeTruthy();

    // keep: a rename without touching the budget doesn't send it
    await user.click(within(menu).getByRole('button', { name: 'Edit Italy' }));
    let form = await screen.findByRole('dialog', { name: 'Edit group' });
    const budget = within(form).getByRole<HTMLInputElement>('textbox', { name: 'Group budget' });
    expect(budget.value).toBe('2000');
    await user.clear(budget);
    await user.type(budget, 'lots');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Enter the budget as an amount, e.g. 2500, or leave it empty')).toBeTruthy();
    await user.clear(budget);
    await user.type(budget, '2 500,50{Enter}');
    await waitFor(() => {
      expect(db.groups[0]?.budgetMinor).toBe(250050);
    });

    // removed: an empty field sends 0
    menu = await screen.findByRole('dialog', { name: 'Groups' });
    await user.click(within(menu).getByRole('button', { name: 'Edit Italy' }));
    form = await screen.findByRole('dialog', { name: 'Edit group' });
    await user.clear(within(form).getByRole('textbox', { name: 'Group budget' }));
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(db.groups[0]?.budgetMinor).toBeUndefined();
    });
    menu = await screen.findByRole('dialog', { name: 'Groups' });
    await waitFor(() => {
      expect(within(menu).queryByText(/left|over/)).toBeNull();
    });

    // a new group with a budget
    await user.click(within(menu).getByRole('button', { name: 'New group' }));
    form = await screen.findByRole('dialog', { name: 'New group' });
    await user.type(within(form).getByRole('textbox', { name: 'Group name' }), 'Ski');
    await user.type(within(form).getByRole('textbox', { name: 'Group budget' }), '4000');
    await user.click(within(form).getByRole('button', { name: 'Create' }));
    await waitFor(() => {
      expect(db.groups.find((g) => g.name === 'Ski')?.budgetMinor).toBe(400000);
    });
  });
});
