import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { chipsOf, cleared, withoutChip } from '../table/chips';
import { contextOf } from '../table/context';
import { ruleDraftOf } from '../table/Toolbar';
import { newView, useViews, viewOf } from '../stores/viewStore';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp } from '../test/render';
import { matchOf, ruleSummary } from './RuleEditor';

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

const C1 = '00000000-0000-4000-8000-0000000000c1';
const C2 = '00000000-0000-4000-8000-0000000000c2';
const R1 = '00000000-0000-4000-8000-0000000000e1';
const R2 = '00000000-0000-4000-8000-0000000000e2';

let stats: Schemas['CategoryStat'][] = [];
let rules: Schemas['Rule'][] = [];
let calls: { method: string; path: string; body: unknown }[] = [];

const rule = (id: string, name: string, over: Partial<Schemas['Rule']> = {}): Schemas['Rule'] => ({
  id,
  name,
  enabled: true,
  position: 0,
  match: { filters: [{ column: 'payee', cond: 'kawiarnia' }] },
  setCategory: { id: C1, name: 'Kawa' },
  markTransfer: false,
  updatedAt: '2026-10-23T10:00:00Z',
  ...over,
});

const record = async (method: string, path: string, request: Request) => {
  const body: unknown = request.method === 'DELETE' ? null : await request.json().catch(() => null);
  calls.push({ method, path, body });
  return body;
};

beforeEach(() => {
  resetDb();
  useViews.getState().reset();
  calls = [];
  stats = [
    { id: C1, name: 'Kawa', txCount: 12, manualCount: 2, ruleCount: 5, aliases: ['kawiarnie'] },
    { id: C2, name: 'Zakupy', txCount: 30, manualCount: 0, ruleCount: 0, aliases: [] },
  ];
  const makler = rule(R2, 'Makler', { position: 1, markTransfer: true });
  delete makler.setCategory;
  rules = [rule(R1, 'Kawiarnie'), makler];
  db.accounts = [account({ txCount: 3 })];
  db.transactions = [
    txRow(1, { payee: 'PRZELEW WLASNY', isTransfer: true }),
    txRow(2, { payee: 'KAWIARNIA ZIARNO' }),
    txRow(3, { payee: 'ZABKA' }),
  ];
  server.use(
    http.get(`${API}/categories/stats`, () => HttpResponse.json(stats)),
    http.get(`${API}/categories`, () =>
      HttpResponse.json(stats.map((s) => ({ id: s.id, name: s.name, txCount: s.txCount }))),
    ),
    http.post(`${API}/categories`, async ({ request }) => {
      const b = (await record('POST', '/categories', request)) as { name: string };
      return HttpResponse.json({ id: '00000000-0000-4000-8000-0000000000c3', name: b.name }, { status: 201 });
    }),
    http.patch(`${API}/categories/:id`, async ({ params, request }) => {
      const b = (await record('PATCH', `/categories/${String(params['id'])}`, request)) as { name: string };
      return HttpResponse.json({ id: String(params['id']), name: b.name });
    }),
    http.post(`${API}/categories/:id/merge`, async ({ params, request }) => {
      await record('POST', `/categories/${String(params['id'])}/merge`, request);
      return HttpResponse.json({ moved: 12 });
    }),
    http.delete(`${API}/categories/:id`, async ({ params, request }) => {
      await record('DELETE', `/categories/${String(params['id'])}`, request);
      return new HttpResponse(null, { status: 204 });
    }),
    http.get(`${API}/rules`, () => HttpResponse.json(rules)),
    http.post(`${API}/rules/preview`, async ({ request }) => {
      await record('POST', '/rules/preview', request);
      return HttpResponse.json({ matched: 3, manual: 1, sample: [db.transactions[1]] });
    }),
    http.post(`${API}/rules/apply`, async ({ request }) => {
      await record('POST', '/rules/apply', request);
      return new HttpResponse(null, { status: 202 });
    }),
    http.put(`${API}/rules/order`, async ({ request }) => {
      const b = (await record('PUT', '/rules/order', request)) as { ids: string[] };
      rules = b.ids.map((id, i) => ({ ...(rules.find((r) => r.id === id) ?? rule(id, id)), position: i }));
      return HttpResponse.json(rules);
    }),
    http.post(`${API}/rules`, async ({ request }) => {
      const b = (await record('POST', '/rules', request)) as Schemas['RuleInput'];
      return HttpResponse.json(rule('00000000-0000-4000-8000-0000000000e3', b.name), { status: 201 });
    }),
    http.put(`${API}/rules/:id`, async ({ params, request }) => {
      const b = (await record('PUT', `/rules/${String(params['id'])}`, request)) as Schemas['RuleInput'];
      return HttpResponse.json(rule(String(params['id']), b.name, { enabled: b.enabled }));
    }),
    http.delete(`${API}/rules/:id`, async ({ params, request }) => {
      await record('DELETE', `/rules/${String(params['id'])}`, request);
      return new HttpResponse(null, { status: 204 });
    }),
  );
});

const lastCall = (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path).at(-1);

describe('Settings → Categories', () => {
  it('lists the counts and creates, renames, merges and deletes categories', async () => {
    const user = userEvent.setup();
    renderApp('/settings/categories');
    const list = await screen.findByRole('list', { name: 'Categories' });
    expect(
      await within(list).findByText(/12 transactions · 2 set by hand · 5 by rules · also gets the bank's “kawiarnie”/),
    ).toBeTruthy();

    await user.type(screen.getByRole('textbox', { name: 'New category name' }), 'Media');
    await user.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => {
      expect(lastCall('POST', '/categories')?.body).toEqual({ name: 'Media' });
    });

    await user.click(screen.getByRole('button', { name: 'Rename Kawa' }));
    const rn = await screen.findByRole('dialog', { name: 'Rename category' });
    const input = within(rn).getByRole('textbox', { name: 'Category name' });
    await user.clear(input);
    await user.type(input, 'Kawiarnie');
    await user.click(within(rn).getByRole('button', { name: 'Rename' }));
    await waitFor(() => {
      expect(lastCall('PATCH', `/categories/${C1}`)?.body).toEqual({ name: 'Kawiarnie' });
    });

    await user.click(screen.getByRole('button', { name: 'Merge Kawa' }));
    const mg = await screen.findByRole('dialog', { name: 'Merge category' });
    expect(within(mg).getByRole<HTMLSelectElement>('combobox', { name: 'Merge into' }).value).toBe(C2);
    await user.click(within(mg).getByRole('button', { name: 'Merge' }));
    await waitFor(() => {
      expect(lastCall('POST', `/categories/${C1}/merge`)?.body).toEqual({ intoId: C2 });
    });
    // earlier toasts are still queued
    expect(
      await screen.findByText('Merged “Kawa” into “Zakupy” · 12 transactions moved', {}, { timeout: 5000 }),
    ).toBeTruthy();

    await user.click(screen.getByRole('button', { name: 'Delete Zakupy' }));
    const del = await screen.findByRole('dialog', { name: 'Delete category' });
    expect(del.textContent).toContain('30 transactions become uncategorised');
    await user.click(within(del).getByRole('button', { name: 'Delete category' }));
    await waitFor(() => {
      expect(lastCall('DELETE', `/categories/${C2}`)).toBeTruthy();
    });
  });
});

describe('Settings → Rules', () => {
  it('lists the rules in order and reorders, disables, applies and deletes them', async () => {
    const user = userEvent.setup();
    renderApp('/settings/rules');
    const list = await screen.findByRole('list', { name: 'Rules' });
    expect(await within(list).findByText('Payee kawiarnia → Kawa')).toBeTruthy();
    expect(within(list).getByText('Payee kawiarnia → transfer')).toBeTruthy();
    expect(within(list).getByRole<HTMLButtonElement>('button', { name: 'Move Kawiarnie up' }).disabled).toBe(true);

    await user.click(within(list).getByRole('button', { name: 'Move Makler up' }));
    await waitFor(() => {
      expect(lastCall('PUT', '/rules/order')?.body).toEqual({ ids: [R2, R1] });
    });

    await user.click(within(list).getByRole('checkbox', { name: 'Enable Kawiarnie' }));
    await waitFor(() => {
      expect(lastCall('PUT', `/rules/${R1}`)?.body).toEqual({
        name: 'Kawiarnie',
        enabled: false,
        match: { filters: [{ column: 'payee', cond: 'kawiarnia' }] },
        setCategoryId: C1,
        markTransfer: false,
      });
    });

    await user.click(screen.getByRole('button', { name: 'Apply rules now' }));
    expect(await screen.findByText('Applying rules…')).toBeTruthy();

    await user.click(within(list).getByRole('button', { name: 'Delete Makler' }));
    const del = await screen.findByRole('dialog', { name: 'Delete rule' });
    await user.click(within(del).getByRole('button', { name: 'Delete rule' }));
    await waitFor(() => {
      expect(lastCall('DELETE', `/rules/${R2}`)).toBeTruthy();
    });
  });

  it('creates a rule with a live preview, and needs a match and an action first', async () => {
    const user = userEvent.setup();
    renderApp('/settings/rules');
    await user.click(await screen.findByRole('button', { name: 'New rule' }));
    const dlg = await screen.findByRole('dialog', { name: 'New rule' });
    const create = within(dlg).getByRole<HTMLButtonElement>('button', { name: 'Create rule' });
    expect(create.disabled).toBe(true);
    expect(within(dlg).getByText('Add a search or a condition to see what the rule matches.')).toBeTruthy();

    await user.type(within(dlg).getByRole('textbox', { name: 'Rule name' }), 'Kawiarnie');
    await user.type(within(dlg).getByRole('textbox', { name: 'Condition 1' }), 'kawiarnia');
    expect(await within(dlg).findByText('3 transactions', {}, { timeout: 2000 })).toBeTruthy();
    expect(lastCall('POST', '/rules/preview')?.body).toEqual({
      match: { filters: [{ column: 'payee', cond: 'kawiarnia' }] },
    });
    expect(create.disabled).toBe(true); // no action yet

    await user.selectOptions(within(dlg).getByRole('combobox', { name: 'Category to set' }), C1);
    expect(within(dlg).getByText(/1 with a category set by hand stay as they are/)).toBeTruthy();
    await user.click(create);
    await waitFor(() => {
      expect(lastCall('POST', '/rules')?.body).toEqual({
        name: 'Kawiarnie',
        enabled: true,
        match: { filters: [{ column: 'payee', cond: 'kawiarnia' }] },
        setCategoryId: C1,
        markTransfer: false,
      });
    });
    expect(await screen.findByText('Rule “Kawiarnie” created · applying…')).toBeTruthy();
  });
});

describe('the table', () => {
  it('turns the view filters into a rule', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    await screen.findByText('ZABKA');
    expect(screen.queryByRole('button', { name: 'Save filters as rule…' })).toBeNull();
    act(() => {
      useViews.getState().patch('all', { filters: { payee: { cond: 'kawiarnia', excluded: [] } }, search: 'blik' });
    });
    await user.click(await screen.findByRole('button', { name: 'Save filters as rule…' }));
    const dlg = await screen.findByRole('dialog', { name: 'New rule from filters' });
    expect(within(dlg).getByRole<HTMLInputElement>('textbox', { name: 'Rule name' }).value).toBe('Payee kawiarnia');
    expect(within(dlg).getByRole<HTMLInputElement>('textbox', { name: 'Search all columns' }).value).toBe('blik');
    expect(within(dlg).getByRole<HTMLInputElement>('textbox', { name: 'Condition 1' }).value).toBe('kawiarnia');
  });

  it('marks transfers and hides them from the Columns menu, with a chip to show them again', async () => {
    const user = userEvent.setup();
    renderApp('/t/all');
    expect((await screen.findAllByText('Transfer')).length).toBeGreaterThan(0);
    expect(db.queries.at(-1)?.context.includeTransfers).toBe(true);
    await user.click(screen.getByRole('button', { name: 'Columns' }));
    await user.click(screen.getByRole('checkbox', { name: 'Show transfers between my accounts' }));
    await waitFor(() => {
      expect(db.queries.at(-1)?.context.includeTransfers).toBe(false);
    });
    await user.keyboard('{Escape}');
    await user.click(await screen.findByRole('button', { name: 'Remove transfers hidden' }));
    await waitFor(() => {
      expect(db.queries.at(-1)?.context.includeTransfers).toBe(true);
    });
  });

  it('shows the pair in the inspector and unpairs it', async () => {
    const user = userEvent.setup();
    const t = db.transactions[0];
    if (!t) throw new Error('no row');
    const detail = (over: Partial<Schemas['TransactionDetail']> = {}): Schemas['TransactionDetail'] => ({
      ...t,
      origin: 'import',
      raw: [],
      sources: [],
      createdAt: '2026-10-01T10:00:00Z',
      updatedAt: '2026-10-01T10:00:00Z',
      transferSource: 'pair',
      transferPeer: {
        id: '00000000-0000-4000-8000-0000000000d9',
        accountName: 'Oszczędności',
        bookedOn: '2025-01-06',
        amount: { amountMinor: 1001, currency: 'PLN' },
      },
      ...over,
    });
    server.use(
      http.get(`${API}/transactions/:id`, () => HttpResponse.json(detail())),
      http.patch(`${API}/transactions/:id`, async ({ request }) => {
        await record('PATCH', '/transactions', request);
        const unpaired = detail({ isTransfer: false, transferSource: 'manual' });
        delete unpaired.transferPeer;
        return HttpResponse.json(unpaired);
      }),
    );
    renderApp('/t/all');
    await user.click(await screen.findByText('PRZELEW WLASNY'));
    const xfer = within(await screen.findByRole('group', { name: 'Transfer' }));
    expect(xfer.getByText(/Transfer · paired with Oszczędności, 06\.01\.2025/)).toBeTruthy();
    await user.click(xfer.getByRole('button', { name: 'Not a transfer' }));
    await waitFor(() => {
      expect(lastCall('PATCH', '/transactions')?.body).toEqual({ transfer: 'no' });
    });
    expect(await xfer.findByText('Not a transfer · decided by you')).toBeTruthy();
    expect(xfer.getByRole('button', { name: 'Automatic' })).toBeTruthy();
  });
});

describe('helpers', () => {
  it('contextOf includes transfers unless the view hides them', () => {
    expect(contextOf('all', newView()).includeTransfers).toBe(true);
    expect(contextOf('all', { ...newView(), hideTransfers: true }).includeTransfers).toBe(false);
  });

  it('the hidden-transfers chip, its removal and Clear all', () => {
    const v = { ...newView(), hideTransfers: true };
    expect(chipsOf('all', v, undefined).map((c) => c.value)).toEqual(['transfers hidden']);
    expect(withoutChip('all', v, 'transfers')).toEqual({ hideTransfers: false });
    expect(cleared('all').hideTransfers).toBe(false);
    expect(viewOf(useViews.getState().views, 'x').hideTransfers).toBe(false);
  });

  it('ruleDraftOf takes the search and filters, nothing else', () => {
    expect(ruleDraftOf({ search: ' ', filters: {} })).toBeNull();
    expect(ruleDraftOf({ search: '', filters: { amount: { cond: '', excluded: ['-1,00'] } } })).toMatchObject({
      name: '',
      filters: [{ column: 'amount', cond: '', excluded: ['-1,00'] }],
    });
    expect(ruleDraftOf({ search: 'blik', filters: {} })?.name).toBe('blik');
  });

  it('matchOf drops empty conditions; ruleSummary reads like the list', () => {
    expect(
      matchOf({
        search: '  ',
        filters: [
          { column: 'payee', cond: ' zabka ', excluded: [] },
          { column: 'amount', cond: '', excluded: [] },
        ],
      }),
    ).toEqual({ filters: [{ column: 'payee', cond: 'zabka' }] });
    expect(
      ruleSummary({
        match: { search: 'blik', filters: [{ column: 'category', excluded: ['A', 'B'] }] },
        setCategory: { id: C1, name: 'Kawa' },
        markTransfer: true,
      }),
    ).toBe('search “blik” · Category −2 values → Kawa, transfer');
  });
});
