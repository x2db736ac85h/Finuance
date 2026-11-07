import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { renderApp, warmUpApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
let entered: string[] = [];
let removed: string[] = [];
beforeEach(() => {
  resetDb();
  entered = [];
  removed = [];
  db.user = { ...db.user, displayName: 'Alex' };
  db.friends = [{ id: '00000000-0000-4000-8000-0000000000e1', displayName: 'Bo', since: '2026-11-01T10:00:00Z' }];
  server.use(
    http.post(`${API}/friends/code`, () =>
      HttpResponse.json(
        { code: 'K7Q4-9MZD', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() },
        { status: 201 },
      ),
    ),
    http.post(`${API}/friends`, async ({ request }) => {
      const { code } = (await request.json()) as Schemas['FriendCodeInput'];
      entered.push(code);
      if (code !== 'ab12-cd34')
        return problem(422, 'validation_failed', 'this code is wrong or has expired; ask for a new one');
      const f = { id: '00000000-0000-4000-8000-0000000000e2', displayName: 'Cy', since: '2026-11-03T10:00:00Z' };
      db.friends = [f, ...db.friends];
      return HttpResponse.json(f, { status: 201 });
    }),
    http.delete(`${API}/friends/:id`, ({ params }) => {
      removed.push(String(params['id']));
      db.friends = db.friends.filter((f) => f.id !== params['id']);
      return new HttpResponse(null, { status: 204 });
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

describe('friends', () => {
  it('shows a code with a countdown, adds a friend by code and removes one', async () => {
    const user = userEvent.setup();
    renderApp('/settings/friends');
    await user.click(await screen.findByRole('button', { name: 'Show my code' }));
    expect((await screen.findByLabelText('Your friend code')).textContent).toBe('K7Q4-9MZD');
    expect(screen.getByRole('timer').textContent).toMatch(/^1[45]:\d\d left$/);

    const code = screen.getByLabelText('Their code');
    await user.type(code, 'nope');
    await user.click(screen.getByRole('button', { name: 'Add friend' }));
    expect(await screen.findByText(/wrong or has expired/)).toBeTruthy();
    await user.clear(code);
    await user.type(code, 'ab12-cd34');
    await user.click(screen.getByRole('button', { name: 'Add friend' }));
    expect(await screen.findByText('You and Cy are friends now')).toBeTruthy();
    expect(entered).toEqual(['nope', 'ab12-cd34']);

    const list = await screen.findByRole('list', { name: 'Friends' });
    await waitFor(() => {
      expect(within(list).getAllByRole('listitem')).toHaveLength(2);
    });
    const bo = within(list).getByText('Bo').closest('li') as HTMLElement;
    await user.click(within(bo).getByRole('button', { name: 'Remove' }));
    const dialog = await screen.findByRole('dialog', { name: 'Remove Bo?' });
    await user.click(within(dialog).getByRole('button', { name: 'Remove' }));
    await waitFor(() => {
      expect(removed).toEqual(['00000000-0000-4000-8000-0000000000e1']);
    });
    expect(screen.queryByText(/@/)).toBeNull(); // friends never see an e-mail address
  });

  it('asks for a display name first', async () => {
    db.user = { ...db.user, displayName: '' };
    renderApp('/settings/friends');
    expect(await screen.findByText('A display name first')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Set yours in Profile' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Show my code' })).toBeNull();
  });

  it('is not offered when the instance switched friends off', async () => {
    db.features = { ...db.features, friends: false };
    renderApp('/settings/friends');
    await screen.findByRole('heading', { name: 'General' });
    expect(screen.queryByRole('link', { name: 'Friends' })).toBeNull();
  });
});

describe('notifications', () => {
  it('counts unread ones in the bell, lists them and marks them read when opened', async () => {
    db.notifications = [
      {
        id: '00000000-0000-4000-8000-0000000000f1',
        kind: 'friend.added',
        data: { name: 'Cy' },
        createdAt: new Date().toISOString(),
      },
      {
        id: '00000000-0000-4000-8000-0000000000f2',
        kind: 'friend.removed',
        data: { name: 'Dee' },
        createdAt: '2026-10-01T10:00:00Z',
        readAt: '2026-10-01T11:00:00Z',
      },
    ];
    const user = userEvent.setup();
    renderApp('/t/all');
    const bell = await screen.findByRole('button', { name: 'Notifications, 1 unread' });
    await user.click(bell);
    const pop = await screen.findByRole('dialog', { name: 'Notifications' });
    expect(within(pop).getByText('Cy entered your code: you are friends now')).toBeTruthy();
    expect(within(pop).getByText('Dee removed you as a friend')).toBeTruthy();
    await waitFor(() => {
      expect(db.notificationsRead).toBe(1);
    });
    expect(await screen.findByRole('button', { name: 'Notifications' })).toBeTruthy();
  });
});
