import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { fmtMoney } from '../lib/format';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp, warmUpApp } from '../test/render';

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

const cats: Schemas['Category'][] = [
  { id: '00000000-0000-4000-8000-0000000000c1', name: 'Rozrywka', txCount: 4 },
  { id: '00000000-0000-4000-8000-0000000000c2', name: 'Transport', txCount: 2 },
];
let patches: Schemas['TransactionPatch'][] = [];

const detailOf = (r: Schemas['TransactionRow']): Schemas['TransactionDetail'] => ({
  ...r,
  origin: 'import',
  raw: [
    { header: 'Data księgowania', value: '05.01.2025' },
    { header: 'Kwota', value: '-10,01' },
  ],
  sources: [
    {
      importId: '00000000-0000-4000-8000-0000000000f1',
      filename: 'pekao.csv',
      committedAt: '2026-10-01T10:00:00Z',
      rowIndex: 4,
      raw: [],
    },
  ],
  createdAt: '2026-10-01T10:00:00Z',
  updatedAt: '2026-10-01T10:00:00Z',
});

beforeEach(() => {
  resetDb();
  patches = [];
  db.accounts = [account({ txCount: 3 })];
  db.transactions = [
    txRow(1, { payee: 'KINO POD GWIAZDAMI', category: { id: cats[0]?.id ?? '', name: 'Rozrywka' } }),
    txRow(2, { payee: 'Sklep Żabka', amount: { amountMinor: 250000, currency: 'PLN' } }),
    txRow(3, { payee: 'Apteka' }),
  ];
  server.use(
    http.get(`${API}/transactions/:id`, ({ params }) => {
      const r = db.transactions.find((t) => t.id === params['id']);
      return r ? HttpResponse.json(detailOf(r)) : HttpResponse.json({}, { status: 404 });
    }),
    http.patch(`${API}/transactions/:id`, async ({ params, request }) => {
      const body = (await request.json()) as Schemas['TransactionPatch'];
      patches.push(body);
      const i = db.transactions.findIndex((t) => t.id === params['id']);
      const r = db.transactions[i];
      if (!r) return HttpResponse.json({}, { status: 404 });
      const cat = body.categoryId
        ? cats.find((c) => c.id === body.categoryId)
        : body.categoryName
          ? { id: '00000000-0000-4000-8000-0000000000c9', name: body.categoryName }
          : undefined;
      const next: Schemas['TransactionRow'] = {
        ...r,
        ...(body.note !== undefined ? { note: body.note } : {}),
        ...(cat ? { category: { id: cat.id, name: cat.name }, categorySource: 'manual' as const } : {}),
      };
      db.transactions[i] = next;
      return HttpResponse.json(detailOf(next));
    }),
    http.get(`${API}/categories`, () => HttpResponse.json(cats)),
  );
});
afterEach(() => {
  server.resetHandlers();
});

const inspector = () => screen.getByRole('complementary', { name: 'Transaction details' });
const status = () => document.querySelector('footer.statusbar') as HTMLElement;

async function openRow(user: ReturnType<typeof userEvent.setup>, payee: string) {
  await user.click(await screen.findByText(payee));
  return within(await screen.findByRole('complementary', { name: 'Transaction details' }));
}

describe('inspector', () => {
  it('shows every field, the original row and the source; prev/next and close', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const ins = await openRow(user, 'KINO POD GWIAZDAMI');
    expect(await ins.findByText('Data księgowania')).toBeTruthy();
    expect(ins.getByText('-10,01')).toBeTruthy();
    expect(ins.getByRole('link', { name: 'pekao.csv' }).getAttribute('href')).toBe(
      '/imports/00000000-0000-4000-8000-0000000000f1',
    );
    expect(ins.getByText(/row 5/)).toBeTruthy();
    expect(ins.getByText('1 / 3')).toBeTruthy();
    expect(ins.getByRole<HTMLInputElement>('combobox', { name: 'Category' }).value).toBe('Rozrywka');
    expect(ins.getByText('from the bank')).toBeTruthy();
    expect(ins.queryByText('Balance after')).toBeNull();
    expect(document.getElementById('app')?.className).toContain('has-insp');

    await user.click(ins.getByRole('button', { name: 'Next row (↓)' }));
    expect(await ins.findByText('Sklep Żabka', { selector: '.fv' })).toBeTruthy();
    expect(ins.getByText('2 / 3')).toBeTruthy();
    expect(within(status()).getByText('Row 2')).toBeTruthy();

    await user.click(ins.getByRole('button', { name: 'Close (Esc)' }));
    await waitFor(() => {
      expect(screen.queryByRole('complementary', { name: 'Transaction details' })).toBeNull();
    });
    expect(document.getElementById('app')?.className).not.toContain('has-insp');
  });

  it('copies the transaction as JSON', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const ins = await openRow(user, 'Apteka');
    await ins.findByText('Data księgowania');
    await user.click(ins.getByRole('button', { name: 'Copy as JSON' }));
    const json = JSON.parse(await navigator.clipboard.readText()) as Record<string, unknown>;
    expect(json['Payee']).toBe('Apteka');
    expect(json['Original row']).toEqual({ 'Data księgowania': '05.01.2025', Kwota: '-10,01' });
    expect(await screen.findByText('Copied the transaction as JSON')).toBeTruthy();
  });

  it('picks an existing category and creates a new one from the combobox', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const ins = await openRow(user, 'Apteka');
    const box = await ins.findByRole('combobox', { name: 'Category' });
    await user.click(box);
    const list = await ins.findByRole('listbox', { name: 'Categories' });
    expect(
      within(list)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Rozrywka', 'Transport']);
    await user.type(box, 'trans');
    expect(
      within(list)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Transport', 'Create “trans”']);
    await user.keyboard('{Enter}');
    await waitFor(() => {
      expect(patches).toEqual([{ categoryId: cats[1]?.id }]);
    });
    expect(await screen.findByText('Category set to “Transport”')).toBeTruthy();
    await waitFor(() => {
      expect(ins.getByRole<HTMLInputElement>('combobox', { name: 'Category' }).value).toBe('Transport');
    });

    const box2 = ins.getByRole('combobox', { name: 'Category' });
    await user.click(box2);
    await user.clear(box2);
    await user.type(box2, 'Leki');
    await user.click(ins.getByRole('option', { name: /Create “Leki”/ }));
    await waitFor(() => {
      expect(patches.at(-1)).toEqual({ categoryName: 'Leki' });
    });
    // a category set by hand can go back to the bank's
    await waitFor(() => {
      expect(ins.getByText('set by you')).toBeTruthy();
    });
    await user.click(ins.getByRole('combobox', { name: 'Category' }));
    await user.click(ins.getByRole('option', { name: /Use the bank's category/ }));
    await waitFor(() => {
      expect(patches.at(-1)).toEqual({ resetCategory: true });
    });
  });

  it('Esc in the category box drops the text without saving', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const ins = await openRow(user, 'KINO POD GWIAZDAMI');
    const box = await ins.findByRole<HTMLInputElement>('combobox', { name: 'Category' });
    await user.click(box);
    await user.type(box, 'xyz');
    await user.keyboard('{Escape}');
    expect(box.value).toBe('Rozrywka');
    expect(patches).toEqual([]);
    // the first Esc stayed in the box: the inspector is still open
    expect(inspector()).toBeTruthy();
  });

  it('saves the note when the field is left', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    const ins = await openRow(user, 'Apteka');
    const note = await ins.findByRole('textbox', { name: 'Note' });
    await user.type(note, 'zwrot do firmy');
    await user.tab();
    await waitFor(() => {
      expect(patches).toEqual([{ note: 'zwrot do firmy' }]);
    });
    expect(await screen.findByText('Note saved')).toBeTruthy();
  });
});

describe('status bar', () => {
  it('shows shown of total, columns and the totals of the rows shown', async () => {
    db.accounts = [account({ txCount: 5 })];
    renderApp('/t/all');
    await screen.findByText('Apteka');
    const st = within(status());
    await waitFor(() => {
      expect(st.getByText(/of 5 rows/).className).toBe('st accent');
    });
    expect(st.getByText('6 / 13 columns')).toBeTruthy();
    const sum = 250000 - 1001 - 1003;
    expect(st.getByText(fmtMoney({ amountMinor: sum, currency: 'PLN' }))).toBeTruthy();
    expect(st.getByText(fmtMoney({ amountMinor: -1003, currency: 'PLN' })).closest('.st')?.textContent).toMatch(/^min/);
    // one number column: no picker, the totals name it in a tooltip
    expect(st.queryByRole('combobox', { name: 'Totals of' })).toBeNull();
    expect(st.getByTitle('Totals of Amount for the rows shown')).toBeTruthy();
  });

  it('offers the totals picker when the rows have balances', async () => {
    db.transactions = db.transactions.map((r, i) => ({
      ...r,
      balanceAfter: { amountMinor: 100000 + i, currency: 'PLN' },
    }));
    db.duplicatesSkipped = 2;
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('Apteka');
    const st = within(status());
    expect(st.getByText('2 duplicates skipped')).toBeTruthy();
    expect(st.getByText(/^3$/).closest('.st')?.className).toBe('st');
    await user.selectOptions(st.getByRole('combobox', { name: 'Totals of' }), 'balanceAfter');
    await waitFor(() => {
      expect(db.queries.at(-1)?.statColumn).toBe('balanceAfter');
    });
    expect(await st.findByText(fmtMoney({ amountMinor: 100002, currency: 'PLN' }))).toBeTruthy();
  });
});
