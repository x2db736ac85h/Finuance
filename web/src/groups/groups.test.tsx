import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { fmtMoney } from '../lib/format';
import { useSelection } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';

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
afterAll(() => {
  server.close();
  if (clientHeight) Object.defineProperty(HTMLElement.prototype, 'clientHeight', clientHeight);
});
beforeEach(() => {
  resetDb();
  useViews.getState().reset();
  useSelection.getState().clear();
  db.accounts = [account({ txCount: 4 })];
  db.transactions = [
    txRow(1, { payee: 'Pizzeria' }),
    txRow(2, { payee: 'Hotel' }),
    txRow(3, { payee: 'Prom' }),
    txRow(4, { payee: 'Apteka' }),
  ];
  server.use(
    http.get(`${API}/transactions/:id`, ({ params }) => {
      const r = db.transactions.find((t) => t.id === params['id']);
      if (!r) return HttpResponse.json({}, { status: 404 });
      const d: Schemas['TransactionDetail'] = {
        ...r,
        origin: 'import',
        raw: [],
        sources: [],
        createdAt: '2026-10-01T10:00:00Z',
        updatedAt: '2026-10-01T10:00:00Z',
      };
      return HttpResponse.json(d);
    }),
    http.get(`${API}/categories`, () => HttpResponse.json([])),
  );
});
afterEach(() => {
  server.resetHandlers();
});

const row = (payee: string) => screen.getByText(payee).closest('tr') as HTMLTableRowElement;
const qbar = () => screen.getByRole('toolbar', { name: 'Selected transactions' });
const lastQuery = () => db.queries.at(-1);

describe('multi-selection and the quick bar', () => {
  it('⌘/Ctrl-click toggles rows; the bar shows the count and Σ; Esc clears', async () => {
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    fireEvent.mouseDown(row('Pizzeria'), { ctrlKey: true });
    fireEvent.mouseDown(row('Prom'), { metaKey: true });
    expect(await screen.findByRole('toolbar', { name: 'Selected transactions' })).toBeTruthy();
    expect(within(qbar()).getByText('2')).toBeTruthy();
    expect(within(qbar()).getByText(fmtMoney({ amountMinor: -1001 - 1003, currency: 'PLN' }))).toBeTruthy();
    expect(row('Pizzeria').className).toContain('msel');
    expect(row('Hotel').className).not.toContain('msel');
    expect(document.body.classList.contains('has-qbar')).toBe(true);
    // toggle one off
    fireEvent.mouseDown(row('Prom'), { ctrlKey: true });
    await waitFor(() => {
      expect(row('Prom').className).not.toContain('msel');
    });
    fireEvent.keyDown(document.body, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Selected transactions' })).toBeNull();
    });
    expect(document.body.classList.contains('has-qbar')).toBe(false);
  });

  it('Shift-click selects a range from the anchor; ranges past the loaded rows come from the server', async () => {
    db.transactions = [...db.transactions, ...Array.from({ length: 500 }, (_, i) => txRow(10 + i))];
    db.accounts = [account({ txCount: 504 })];
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    fireEvent.mouseDown(row('Hotel')); // a plain press sets the anchor
    fireEvent.mouseUp(window);
    fireEvent.mouseDown(row('Apteka'), { shiftKey: true });
    await waitFor(() => {
      expect(within(qbar()).getByText('3')).toBeTruthy();
    });
    expect(db.idRequests).toEqual([]);
    expect(['Pizzeria', 'Hotel', 'Prom', 'Apteka'].map((p) => row(p).className.includes('msel'))).toEqual([
      false,
      true,
      true,
      true,
    ]);

    // An anchor in a window that isn't loaded: the range is resolved with POST /transactions/ids.
    act(() => {
      useSelection.getState().setAnchor('all', 420);
    });
    fireEvent.mouseDown(row('Pizzeria'), { shiftKey: true });
    await waitFor(() => {
      expect(db.idRequests.at(-1)).toMatchObject({ offset: 0, limit: 421 });
    });
    await waitFor(() => {
      expect(within(qbar()).getByText('421')).toBeTruthy();
    });
  });

  it('a plain click ends the multi-selection and opens the row', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    fireEvent.mouseDown(row('Pizzeria'), { ctrlKey: true });
    await screen.findByRole('toolbar', { name: 'Selected transactions' });
    await user.click(screen.getByText('Hotel'));
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Selected transactions' })).toBeNull();
    });
    expect(await screen.findByRole('complementary', { name: 'Transaction details' })).toBeTruthy();
  });

  it('creates a group from the bar and adds the selection; Ungroup removes it', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    fireEvent.mouseDown(row('Pizzeria'), { ctrlKey: true });
    fireEvent.mouseDown(row('Hotel'), { ctrlKey: true });
    await user.click(
      within(await screen.findByRole('toolbar', { name: 'Selected transactions' })).getByRole('button', {
        name: 'New group',
      }),
    );
    const form = await screen.findByRole('dialog', { name: 'New group' });
    await user.type(within(form).getByRole('textbox', { name: 'Group name' }), '  Italy   trip ');
    await user.click(within(form).getByRole('radio', { name: 'Colour 3' }));
    await user.keyboard('{Enter}');
    expect(await screen.findByText('2 transactions added to “Italy trip”')).toBeTruthy();
    expect(db.groups).toEqual([{ id: expect.any(String) as string, name: 'Italy trip', color: 2 }]);
    expect(db.bulks.at(-1)?.ids.length).toBe(2);
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Selected transactions' })).toBeNull();
    });

    // the Group column appears with the pills
    expect(await screen.findByRole('columnheader', { name: /Group/ })).toBeTruthy();
    await waitFor(() => {
      expect(row('Pizzeria').querySelector('.gtag')?.textContent).toBe('Italy trip');
    });

    // select a grouped row: Ungroup is offered
    fireEvent.mouseDown(row('Hotel'), { ctrlKey: true });
    await user.click(
      within(await screen.findByRole('toolbar', { name: 'Selected transactions' })).getByRole('button', {
        name: 'Ungroup',
      }),
    );
    expect(await screen.findByText('1 transaction removed from their group', {}, { timeout: 3000 })).toBeTruthy();
    expect(db.bulks.at(-1)?.set.groupId).toBeNull();
  });

  it('adds the selection to an existing group with its dot', async () => {
    db.groups = [{ id: '00000000-0000-4000-8000-0000000000e1', name: 'Wesele', color: 4 }];
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    fireEvent.mouseDown(row('Prom'), { ctrlKey: true });
    const bar = await screen.findByRole('toolbar', { name: 'Selected transactions' });
    expect(within(bar).queryByRole('button', { name: 'Ungroup' })).toBeNull();
    await user.click(within(bar).getByRole('button', { name: 'Add to Wesele' }));
    expect(await screen.findByText('1 transaction added to “Wesele”')).toBeTruthy();
    expect(db.bulks).toEqual([
      { ids: [db.transactions[2]?.id], set: { groupId: '00000000-0000-4000-8000-0000000000e1' } },
    ]);
  });
});

describe('groups menu', () => {
  beforeEach(() => {
    db.groups = [
      { id: '00000000-0000-4000-8000-0000000000e1', name: 'Italy', color: 0 },
      { id: '00000000-0000-4000-8000-0000000000e2', name: 'Wesele', color: 1 },
    ];
    db.transactions = db.transactions.map((t, i) =>
      i < 2 ? { ...t, group: { id: '00000000-0000-4000-8000-0000000000e1', name: 'Italy' } } : t,
    );
  });

  it('lists groups with stats and toggles the group filter', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    const btn = screen.getByRole('button', { name: /Groups/ });
    expect(within(btn).getByText('2')).toBeTruthy();
    await user.click(btn);
    const menu = await screen.findByRole('dialog', { name: 'Groups' });
    const italy = within(menu).getByRole('button', { name: /^Italy/ });
    expect(italy.textContent).toContain(`2 · ${fmtMoney({ amountMinor: 2003, currency: 'PLN' }, { whole: true })}`);
    await user.click(italy);
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toEqual([{ column: 'group', cond: '=Italy' }]);
    });
    expect(screen.getByRole('button', { name: 'Remove Group =Italy' })).toBeTruthy();
    // again → everything
    await user.click(screen.getByRole('button', { name: /Groups/ }));
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Groups' })).getByRole('button', { name: /^Italy/ }),
    );
    await waitFor(() => {
      expect(lastQuery()?.context.filters).toBeUndefined();
    });
  });

  it('renames a group and the active filter follows; delete asks first and clears it', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Pizzeria');
    await user.click(screen.getByRole('button', { name: /Groups/ }));
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Groups' })).getByRole('button', { name: /^Italy/ }),
    );
    await waitFor(() => {
      expect(viewOf(useViews.getState().views, 'all').filters.group?.cond).toBe('=Italy');
    });

    await user.click(screen.getByRole('button', { name: /Groups/ }));
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Groups' })).getByRole('button', {
        name: 'Rename or recolour Italy',
      }),
    );
    const form = await screen.findByRole('dialog', { name: 'Edit group' });
    const name = within(form).getByRole('textbox', { name: 'Group name' });
    await user.clear(name);
    await user.type(name, 'wesele');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('A group with this name already exists')).toBeTruthy();
    await user.clear(name);
    await user.type(name, 'Włochy{Enter}');
    await waitFor(() => {
      expect(viewOf(useViews.getState().views, 'all').filters.group?.cond).toBe('=Włochy');
    });
    expect(db.groups[0]?.name).toBe('Włochy');

    // the menu comes back after the form (legacy)
    const menu = await screen.findByRole('dialog', { name: 'Groups' });
    await user.click(await within(menu).findByRole('button', { name: 'Delete Włochy' }));
    const confirm = await screen.findByRole('dialog', { name: 'Delete group' });
    expect(confirm.textContent).toContain('2 transactions only lose this group label');
    await user.click(within(confirm).getByRole('button', { name: 'Delete group' }));
    expect(await screen.findByText('Group “Włochy” deleted — transactions kept', {}, { timeout: 3000 })).toBeTruthy();
    expect(viewOf(useViews.getState().views, 'all').filters.group).toBeUndefined();
    expect(db.groups.map((g) => g.name)).toEqual(['Wesele']);
  });

  it('the inspector pills set and remove the group of one transaction', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await user.click(await screen.findByText('Apteka'));
    const ins = within(await screen.findByRole('complementary', { name: 'Transaction details' }));
    const pills = within(await ins.findByRole('group', { name: 'Group' }));
    expect(pills.getByRole('button', { name: 'Italy' }).getAttribute('aria-pressed')).toBe('false');
    await user.click(pills.getByRole('button', { name: 'Wesele' }));
    expect(await screen.findByText('Added to “Wesele”')).toBeTruthy();
    await waitFor(() => {
      expect(pills.getByRole('button', { name: 'Wesele' }).getAttribute('aria-pressed')).toBe('true');
    });
    await user.click(pills.getByRole('button', { name: 'Wesele' }));
    expect(await screen.findByText('Removed from its group', {}, { timeout: 3000 })).toBeTruthy();
    expect(db.bulks.map((b) => b.set.groupId)).toEqual(['00000000-0000-4000-8000-0000000000e2', null]);
  });
});
