import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { renderApp, warmUpApp } from '../test/render';
import { account, API, db, resetDb, server, txRow } from '../test/server';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
afterAll(() => {
  server.close();
});
afterEach(() => {
  server.resetHandlers();
});

const ACC = '00000000-0000-4000-8000-0000000000a1';
const suggestion = (typedId: string) => ({
  typedId,
  importedId: `i-${typedId}`,
  typedOn: '2026-11-04',
  importedOn: '2026-11-05',
  typedPayee: `Kiosk ${typedId}`,
  importedPayee: 'KIOSK RUCH',
  amount: { amountMinor: -1200, currency: 'PLN' },
  account: { id: ACC, name: 'Konto' },
});

beforeEach(() => {
  resetDb();
  db.accounts = [account({ id: ACC, name: 'Konto', txCount: 1 })];
  db.transactions = [txRow(1, { payee: 'KIOSK RUCH' })];
  db.mergeSuggestions = [suggestion('t1'), suggestion('t2')];
  server.use(http.get(`${API}/categories`, () => HttpResponse.json([])));
});

describe('merge suggestions (ADR 0032)', () => {
  it('offers to merge a typed card payment into the bank row, one at a time', async () => {
    renderApp(`/t/${ACC}`);
    const card = await screen.findByRole('region', { name: 'Merge suggestion' });
    expect(card.textContent).toContain('You typed Kiosk t1 (04.11.2026, 12.00');
    expect(card.textContent).toContain('KIOSK RUCH');
    fireEvent.click(within(card).getByRole('button', { name: 'Merge' }));
    await waitFor(() => {
      expect(db.merges).toEqual([{ id: 't1', body: { into: 'i-t1', keepApart: false } }]);
    });
    // the next one; Keep both remembers the pair
    const next = await screen.findByText('Kiosk t2');
    fireEvent.click(within(next.closest('[role=region]') as HTMLElement).getByRole('button', { name: 'Keep both' }));
    await waitFor(() => {
      expect(db.merges.at(-1)).toEqual({ id: 't2', body: { into: 'i-t2', keepApart: true } });
    });
    await waitFor(() => {
      expect(screen.queryByRole('region', { name: 'Merge suggestion' })).toBeNull();
    });
  });

  it('shows nothing in another account', async () => {
    const other = '00000000-0000-4000-8000-0000000000b2';
    db.accounts = [...db.accounts, account({ id: other, name: 'Karta', txCount: 1 })];
    renderApp(`/t/${other}`);
    await screen.findByRole('region', { name: 'Transactions' });
    expect(screen.queryByRole('region', { name: 'Merge suggestion' })).toBeNull();
  });
});
