import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { fmtMoney } from '../lib/format';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { renderApp, warmUpApp } from '../test/render';
import { useDupDialog } from './store';

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

const ACC = '00000000-0000-4000-8000-0000000000a1';
const V1 = '00000000-0000-4000-8000-0000000000f1';
const V2 = '00000000-0000-4000-8000-0000000000f2';

const fields = (over: Partial<Schemas['DuplicateFields']> = {}): Schemas['DuplicateFields'] => ({
  bookedOn: '2025-02-28',
  amount: { amountMinor: -12000, currency: 'PLN' },
  payee: 'SKLEP ALFA',
  description: 'Zakupy luty',
  reference: 'D5',
  txType: 'KARTA',
  bankCategory: 'Bez kategorii',
  counterpartyAccount: '',
  ...over,
});

function group(i: number, over: Partial<Schemas['DuplicateGroup']> = {}): Schemas['DuplicateGroup'] {
  const fp = i.toString(16).padStart(64, '0');
  return {
    accountId: ACC,
    fingerprint: fp,
    keyFields: fields({ payee: `SKLEP ${String(i)}` }),
    differingFields: ['bankCategory'],
    versions: [
      {
        importId: V1,
        filename: 'v1_jan-feb.csv',
        rows: [{ occurrence: 1, fields: fields({ payee: `SKLEP ${String(i)}` }), raw: [], inData: true }],
      },
      {
        importId: V2,
        filename: 'v2_jan-mar.csv',
        rows: [
          {
            occurrence: 1,
            fields: fields({ payee: `SKLEP ${String(i)}`, bankCategory: 'Artykuły spożywcze' }),
            raw: [],
            inData: false,
          },
        ],
      },
    ],
    ...over,
  };
}

let groups: Schemas['DuplicateGroup'][] = [];
let lists: URL[] = [];
let puts: { path: string; body: Schemas['DuplicateDecision'] }[] = [];
let deletes: string[] = [];

beforeEach(() => {
  useDupDialog.getState().close();
  resetDb();
  db.accounts = [account({ txCount: 7 })];
  db.transactions = Array.from({ length: 7 }, (_, i) => txRow(i + 1));
  db.duplicatesSkipped = 4;
  groups = [group(1), group(2, { override: { mode: 'keep_both' } })];
  lists = [];
  puts = [];
  deletes = [];
  server.use(
    http.get(`${API}/duplicates`, ({ request }) => {
      const url = new URL(request.url);
      lists.push(url);
      const q = url.searchParams.get('q') ?? '';
      const offset = Number(url.searchParams.get('offset') ?? 0);
      const limit = Number(url.searchParams.get('limit') ?? 60);
      const match = groups.filter((g) => !q || g.keyFields.payee.toLowerCase().includes(q.toLowerCase()));
      return HttpResponse.json({ total: match.length, groups: match.slice(offset, offset + limit) });
    }),
    http.put(`${API}/duplicates/:acc/:fp`, async ({ params, request }) => {
      const body = (await request.json()) as Schemas['DuplicateDecision'];
      puts.push({ path: `${String(params['acc'])}/${String(params['fp'])}`, body });
      const g = groups.find((x) => x.fingerprint === params['fp']);
      if (!g) return HttpResponse.json({}, { status: 404 });
      g.override = body;
      if (body.mode === 'prefer') {
        for (const v of g.versions) for (const r of v.rows) r.inData = v.importId === body.importId;
      }
      return HttpResponse.json(g);
    }),
    http.delete(`${API}/duplicates/:acc/:fp`, ({ params }) => {
      deletes.push(String(params['fp']));
      const g = groups.find((x) => x.fingerprint === params['fp']);
      if (!g) return HttpResponse.json({}, { status: 404 });
      delete g.override;
      return HttpResponse.json(g);
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
});

async function openDialog(user: ReturnType<typeof userEvent.setup>) {
  renderApp(`/t/${ACC}`);
  const link = await screen.findByRole('button', { name: '4 duplicates skipped' }, { timeout: 5000 }); // the app boots first
  await user.click(link);
  return within(await screen.findByRole('dialog', { name: 'Duplicates' }));
}

describe('duplicates window', () => {
  it('opens from the status bar for the view and shows each group with its versions', async () => {
    const user = userEvent.setup();
    const dlg = await openDialog(user);
    expect(dlg.getByText('4 rows skipped')).toBeTruthy();
    const g1 = within(await dlg.findByRole('region', { name: /SKLEP 1/ }));
    expect(lists[0]?.searchParams.get('accountIds')).toBe(ACC);
    expect(g1.getByText('1 field differs')).toBeTruthy();
    expect(g1.getByText(fmtMoney({ amountMinor: -12000, currency: 'PLN' }), { selector: '.dg-a' })).toBeTruthy();
    const rows = g1.getAllByRole('row').slice(1);
    expect(rows.map((r) => r.className)).toEqual(['k', 's']);
    expect(within(rows[0] as HTMLElement).getByText('✓')).toBeTruthy();
    expect(within(rows[1] as HTMLElement).getByText('Artykuły spożywcze').className).toBe('df');
    expect(g1.getByRole('columnheader', { name: 'Bank category' })).toBeTruthy();
    // a kept-apart group offers Merge again and no versions to pick
    const g2 = within(dlg.getByRole('region', { name: /SKLEP 2/ }));
    expect(g2.getByText('Kept as separate transactions')).toBeTruthy();
    expect(g2.queryByRole('button', { name: 'Use this version' })).toBeNull();
  });

  it('Use this version, Keep both and Merge again call the API and refresh the table', async () => {
    const user = userEvent.setup();
    const dlg = await openDialog(user);
    const g1 = within(await dlg.findByRole('region', { name: /SKLEP 1/ }));
    const before = db.queries.length;
    await user.click(g1.getByRole('button', { name: 'Use this version' }));
    await waitFor(() => {
      expect(puts).toEqual([
        { path: `${ACC}/${groups[0]?.fingerprint ?? ''}`, body: { mode: 'prefer', importId: V2 } },
      ]);
    });
    // the window and the table refetch
    await waitFor(() => {
      expect(g1.getAllByRole('row')[2]?.className).toBe('k');
    });
    await waitFor(() => {
      expect(db.queries.length).toBeGreaterThan(before);
    });
    expect(g1.getByRole('button', { name: 'Reset choice' })).toBeTruthy();

    await user.click(g1.getByRole('button', { name: 'Keep both' }));
    await waitFor(() => {
      expect(puts.at(-1)?.body).toEqual({ mode: 'keep_both' });
    });
    await user.click(within(dlg.getByRole('region', { name: /SKLEP 2/ })).getByRole('button', { name: 'Merge again' }));
    await waitFor(() => {
      expect(deletes).toEqual([groups[1]?.fingerprint]);
    });
  });

  // 75 groups render in ~2 s alone, but over 5 s when the full parallel suite shares a busy machine
  // (the maintainer, 2026-11-06: a longer limit for this test; Backlog: a cheaper first render)
  it('filters and pages 60 at a time', async () => {
    groups = Array.from({ length: 75 }, (_, i) => group(i + 1));
    const user = userEvent.setup();
    const dlg = await openDialog(user);
    await dlg.findByRole('region', { name: /SKLEP 1$/ });
    expect(dlg.getAllByRole('region')).toHaveLength(60);
    await user.click(dlg.getByRole('button', { name: 'Show 15 more of 15' }));
    await waitFor(() => {
      expect(dlg.getAllByRole('region')).toHaveLength(75);
    });
    expect(lists.at(-1)?.searchParams.get('offset')).toBe('60');

    await user.type(dlg.getByRole('textbox', { name: 'Filter duplicates' }), 'sklep 7');
    await waitFor(() => {
      expect(lists.at(-1)?.searchParams.get('q')).toBe('sklep 7');
    });
    await waitFor(() => {
      expect(dlg.getAllByRole('region').length).toBe(7); // 7 and 70–75
    });
    await user.clear(dlg.getByRole('textbox', { name: 'Filter duplicates' }));
    await user.type(dlg.getByRole('textbox', { name: 'Filter duplicates' }), 'nothing');
    expect(await dlg.findByText('No duplicates match the filter.')).toBeTruthy();
  }, 15_000);
});
