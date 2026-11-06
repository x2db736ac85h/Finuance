import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Goal } from '../api/hooks/goals';
import { API, account, db, resetDb, server } from '../test/server';
import { goalsChart, goalsParts } from './Goals';

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
beforeEach(() => {
  resetDb();
  db.accounts = [account({ name: 'Konto' })];
  server.use(http.get(`${API}/categories`, () => HttpResponse.json([{ id: 'c1', name: 'Jedzenie', txCount: 4 }])));
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const goal = (over: Partial<Goal>): Goal => ({
  id: 'g1',
  name: 'Wakacje',
  kind: 'save',
  ...(over.kind === 'reduce' ? {} : { targetMinor: 1_000_000 }),
  currency: 'PLN',
  baselineMonths: 3,
  startsOn: '2026-11-01',
  deadline: '2027-06-30',
  status: 'active',
  version: 1,
  ai: false,
  progress: {
    state: 'on_track',
    percent: 25,
    daysLeft: 236,
    savedMinor: 250_000,
    expectedMinor: 24_793,
    projectedMinor: 0,
  },
  ...over,
});

const cut = goal({
  id: 'g2',
  name: 'Mniej na jedzenie',
  kind: 'reduce',
  category: { id: 'c1', name: 'Jedzenie' },
  reducePct: 20,
  ai: true,
  progress: {
    state: 'behind',
    percent: 25,
    daysLeft: 175,
    baselineMinor: 50_000,
    monthlyLimitMinor: 40_000,
    thisMonthMinor: 10_000,
    allowedSoFarMinor: 8_000,
    monthsKept: 1,
    monthsPast: 2,
  },
});

function show(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

/** The card as the overview draws it (useGoals → parts). */
function Card() {
  const p = goalsChart.use(
    { context: {}, ctxKey: '{}', sort: undefined, sortKey: 'null' },
    {} as Parameters<typeof goalsChart.use>[1],
  );
  return (
    <section>
      <span data-testid="sub">{p.sub}</span>
      {p.body}
    </section>
  );
}

describe('goals card', () => {
  it('draws each goal with its progress and pace; archived goals only in the table', () => {
    const old = goal({ id: 'g3', name: 'Old', status: 'archived' });
    const p = goalsParts([cut, goal({}), old]);
    expect(p.sub).toBe('2 active · 1 archived');
    expect(p.table?.cols).toEqual(['Goal', 'Kind', 'So far', 'Target', 'Deadline', 'Status']);
    expect(p.table?.rows.map((r) => [r[0], r[1], r[4], r[5]])).toEqual([
      ['Mniej na jedzenie', 'Spend 20 % less on Jedzenie', '2027-06-30', 'Behind'],
      ['Wakacje', 'Save', '2027-06-30', 'On track'],
      ['Old', 'Save', '2027-06-30', 'Archived'],
    ]);
    const { container } = show(p.body);
    const rows = [...container.querySelectorAll('.gr')];
    expect(rows).toHaveLength(2);
    const [reduce, save] = rows as [HTMLElement, HTMLElement];
    expect(reduce.textContent).toContain('20 % less on Jedzenie · 1 of 2 months kept · 175 days left');
    expect(reduce.textContent).toContain('this month');
    expect(within(reduce).getByTitle(/Set by the assistant/)).toBeTruthy();
    expect(reduce.querySelector<HTMLElement>('.bfill')?.style.width).toBe('25%');
    expect(reduce.querySelector<HTMLElement>('.bpace')?.style.left).toBe('20%');
    expect(save.textContent).toContain('Wakacje236 days left');
    expect(save.textContent).toContain('On track');
    expect(save.querySelector<HTMLElement>('.bfill')?.style.width).toBe('25%');
    expect(save.querySelector<HTMLElement>('.bpace')?.style.left).toBe('2.5%');
    expect(within(save).queryByTitle(/Set by the assistant/)).toBeNull();
  });

  it('adds a savings goal and a reduction through the form', async () => {
    const user = userEvent.setup();
    show(<Card />);
    expect(await screen.findByText(/No goals yet/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'New goal' }));
    const dialog = await screen.findByRole('dialog', { name: 'New goal' });
    await user.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(within(dialog).getByRole('alert').textContent).toBe('Give the goal a name.');
    await user.type(within(dialog).getByLabelText('Name'), 'Wakacje');
    await user.type(within(dialog).getByLabelText('Amount (PLN)'), '10 000');
    fireEvent.change(within(dialog).getByLabelText('Deadline'), { target: { value: '2027-06-30' } });
    await user.click(within(dialog).getByRole('button', { name: 'Add' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(db.goalWrites.at(-1)).toMatchObject({
      method: 'POST',
      body: {
        name: 'Wakacje',
        kind: 'save',
        targetMinor: 1_000_000,
        currency: 'PLN',
        baselineMonths: 3,
        deadline: '2027-06-30',
      },
    });
    expect(await screen.findByText('Wakacje')).toBeTruthy();

    await user.click(screen.getByRole('button', { name: 'New goal' }));
    const d2 = await screen.findByRole('dialog', { name: 'New goal' });
    await user.click(within(d2).getByLabelText('Spend less on a category'));
    await user.type(within(d2).getByLabelText('Name'), 'Mniej na jedzenie');
    await user.selectOptions(await within(d2).findByLabelText('Category'), 'c1');
    fireEvent.change(within(d2).getByLabelText('Deadline'), { target: { value: '2027-04-30' } });
    await user.click(within(d2).getByRole('button', { name: 'Add' }));
    await waitFor(() => {
      expect(db.goalWrites).toHaveLength(2);
    });
    expect(db.goalWrites[1]?.body).toMatchObject({
      kind: 'reduce',
      categoryId: 'c1',
      reducePct: 20,
      baselineMonths: 3,
    });
    expect(db.goalWrites[1]?.body).not.toHaveProperty('targetMinor');
  });

  it('changes a goal with the version it read; a stale one says so', async () => {
    const user = userEvent.setup();
    db.goals = [goal({})];
    show(<Card />);
    await user.click(await screen.findByRole('button', { name: 'Change Wakacje' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change the goal' });
    expect(within(dialog).queryByLabelText('Spend less on a category')).toBeNull(); // the kind stays
    fireEvent.change(within(dialog).getByLabelText('Deadline'), { target: { value: '2027-08-31' } });
    db.goals = [goal({ version: 2 })]; // changed meanwhile (another tab, the assistant)
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(within(dialog).getByRole('alert').textContent).toBe('The goal was changed meanwhile.');
    });
    expect(db.goalWrites.at(-1)).toMatchObject({
      method: 'PATCH',
      id: 'g1',
      body: { version: 1, deadline: '2027-08-31' },
    });
  });
});
