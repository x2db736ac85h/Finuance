import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { ActivityAction, ActivityEvent } from '../api/hooks/activity';
import { renderApp, warmUpApp } from '../test/render';
import { account, API, db, resetDb, server, txRow } from '../test/server';
import { useViews, viewOf } from '../stores/viewStore';
import { describeActivity } from './Activity';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const ev = (id: number, action: ActivityAction, extra: Partial<ActivityEvent> = {}): ActivityEvent => ({
  id,
  at: '2026-10-26T08:00:00Z',
  action,
  ...extra,
});

describe('activity', () => {
  it("lists the assistant's changes with Undo and shows the rows it changed", async () => {
    db.accounts = [account({ txCount: 2 })];
    db.transactions = [txRow(1, { ai: true }), txRow(2)];
    server.use(http.get(`${API}/categories`, () => HttpResponse.json([])));
    db.aiChanges = [
      {
        id: '00000000-0000-4000-8000-0000000000e2',
        chatId: '00000000-0000-4000-8000-0000000000f1',
        proposalKey: '00000000000000e1',
        action: 'set_category',
        auto: true,
        appliedAt: '2026-11-06T11:00:00Z',
        items: 15,
      },
      {
        id: '00000000-0000-4000-8000-0000000000e1',
        chatId: '00000000-0000-4000-8000-0000000000f1',
        proposalKey: '00000000000000e2',
        action: 'create_rule',
        auto: false,
        appliedAt: '2026-11-05T11:00:00Z',
        revertedAt: '2026-11-05T12:00:00Z',
        items: 1,
      },
    ];
    const user = userEvent.setup();
    const { router } = renderApp('/settings/activity');
    const list = await screen.findByRole('list', { name: 'Assistant changes' });
    const [open, undone] = within(list).getAllByRole('listitem');
    expect(open?.textContent).toContain('Set a category · 15 items');
    expect(open?.textContent).toContain('applied automatically');
    expect(undone?.textContent).toContain('undone');
    expect(within(undone as HTMLElement).queryByRole('button', { name: 'Undo' })).toBeNull();
    await user.click(within(open as HTMLElement).getByRole('button', { name: 'Undo' }));
    await waitFor(() => {
      expect(within(list).getAllByText('undone')).toHaveLength(2);
    });
    expect(db.aiChanges[0]?.revertedAt).toBeTruthy();

    await user.click(screen.getByRole('button', { name: 'Show what the assistant changed' }));
    expect(router.state.location.pathname).toBe('/t/all');
    expect(viewOf(useViews.getState().views, 'all').aiOnly).toBe(true);
    expect(await screen.findByRole('button', { name: 'Remove ✦ AI assisted' })).toBeTruthy();
    expect(db.queries.at(-1)?.context.aiAssisted).toBe(true);
  });

  it('shows no assistant section before the assistant changed anything', async () => {
    renderApp('/settings/activity');
    await screen.findByRole('list', { name: 'Activity' });
    expect(screen.queryByRole('list', { name: 'Assistant changes' })).toBeNull();
  });

  it('lists events newest first, flags failed sign-ins and loads more', async () => {
    const first = Array.from({ length: 50 }, (_, i) =>
      ev(100 - i, 'auth.login', { ip: '192.0.2.4', browser: 'Firefox on Linux' }),
    );
    first[0] = ev(100, 'auth.login_failed', { ip: '203.0.113.9', browser: 'Chrome on Windows' });
    first[1] = ev(99, 'import.committed', { meta: { file: 'pekao.csv', new: 12, duplicates: 3 } });
    const asked: (string | null)[] = [];
    server.use(
      http.get(`${API}/me/activity`, ({ request }) => {
        const before = new URL(request.url).searchParams.get('before');
        asked.push(before);
        return before
          ? HttpResponse.json({ events: [ev(10, 'export.data', { ip: '192.0.2.4' })] })
          : HttpResponse.json({ events: first, nextBefore: 51 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/activity');
    const list = await screen.findByRole('list', { name: 'Activity' });
    const items = await within(list).findAllByRole('listitem');
    expect(items).toHaveLength(50);
    expect(items[0]?.textContent).toContain('Failed sign-in: wrong password');
    expect(items[0]?.textContent).toContain('attention');
    expect(items[0]?.textContent).toContain('203.0.113.9 · Chrome on Windows');
    expect(items[1]?.textContent).toContain('Imported pekao.csv: 12 new transactions, 3 already there');
    expect(items[1]?.textContent).toContain('in the background');

    await user.click(screen.getByRole('button', { name: 'Load more' }));
    expect(await within(list).findByText('Downloaded all data')).toBeTruthy();
    expect(asked).toEqual([null, '51']);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('says when nothing is recorded', async () => {
    server.use(http.get(`${API}/me/activity`, () => HttpResponse.json({ events: [] })));
    renderApp('/settings/activity');
    expect(await screen.findByText('Nothing recorded yet.')).toBeTruthy();
  });
});

describe('describeActivity', () => {
  it('words every kind of event', () => {
    const cases: [ActivityEvent, string][] = [
      [ev(1, 'auth.logout'), 'Signed out'],
      [ev(1, 'auth.session_revoked'), 'Signed out another browser'],
      [
        ev(1, 'import.deleted', { meta: { file: 'a.csv', transactionsRemoved: 1 } }),
        'Deleted the import a.csv (1 transaction removed)',
      ],
      [ev(1, 'import.deleted', { meta: { file: 'a.csv', transactionsRemoved: 0 } }), 'Deleted the import a.csv'],
      [ev(1, 'account.deleted', { meta: { name: 'Konto' } }), 'Deleted the account Konto'],
      [ev(1, 'export.csv', { meta: { file: 'finuance-all.csv' } }), 'Exported transactions to finuance-all.csv'],
      [ev(1, 'legacy.import_started', { meta: { files: 3 } }), 'Started a migration from the old app (3 files)'],
      [ev(1, 'duplicates.decided', { meta: { mode: 'keep_both' } }), 'Kept both versions of a duplicate'],
      [ev(1, 'duplicates.decided', { meta: { mode: 'prefer' } }), 'Chose a version of a duplicate'],
      [ev(1, 'duplicates.reset'), 'Merged a duplicate again'],
      [
        ev(1, 'assistant.action_applied', { meta: { action: 'set_category', rows: 14 } }),
        'Applied an assistant proposal: set a category for 14 transactions',
      ],
      [
        ev(1, 'assistant.action_applied', { meta: { action: 'create_rule', rows: 1 } }),
        'Applied an assistant proposal: created a rule',
      ],
    ];
    for (const [e, want] of cases) expect(describeActivity(e)).toBe(want);
  });
});
