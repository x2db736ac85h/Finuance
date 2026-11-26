import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { renderApp, warmUpApp } from '../test/render';
import { useToasts } from '../ui/toastStore';
import { API, db, problem, resetDb, server } from '../test/server';
import { themeFileText } from '../styles/themeFile';
import { BUILTIN_THEMES } from '../styles/themes';
import { browserName } from './Sessions';

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

const FF = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:131.0) Gecko/20100101 Firefox/131.0';
const CHROME = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36';

describe('settings', () => {
  it('lists sessions and signs out another browser', async () => {
    db.sessions = [
      {
        id: 'aaaaaaaaaaaaaaaa',
        createdAt: '2026-10-20T08:00:00Z',
        lastSeenAt: '2026-10-21T08:00:00Z',
        expiresAt: '2026-11-20T08:00:00Z',
        userAgent: FF,
        current: true,
      },
      {
        id: 'bbbbbbbbbbbbbbbb',
        createdAt: '2026-10-19T08:00:00Z',
        lastSeenAt: '2026-10-19T09:00:00Z',
        expiresAt: '2026-11-19T08:00:00Z',
        userAgent: CHROME,
        ip: '192.0.2.4',
        current: false,
      },
    ];
    let revoked = '';
    server.use(
      http.delete(`${API}/me/sessions/:id`, ({ params }) => {
        revoked = String(params['id']);
        db.sessions = db.sessions.filter((s) => s.id !== revoked);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/security');
    // Sessions is a part of Security since Phase 26 (U3)
    const list = await within(await screen.findByRole('region', { name: 'Sessions' })).findByRole('list');
    await within(list).findByText('Chrome on Linux');
    expect(within(list).getByText('this browser')).toBeTruthy();
    expect(within(list).getAllByRole('button', { name: 'Sign out' })).toHaveLength(1);
    await user.click(within(list).getByRole('button', { name: 'Sign out' }));
    await waitFor(() => {
      expect(within(list).queryByText('Chrome on Linux')).toBeNull();
    });
    expect(revoked).toBe('bbbbbbbbbbbbbbbb');
  });

  it('checks that the new passwords match before sending', async () => {
    const user = userEvent.setup();
    renderApp('/settings/profile');
    await user.type(await screen.findByLabelText('Current password'), 'old password 123');
    await user.type(screen.getByLabelText('New password'), 'a new password 1');
    await user.type(screen.getByLabelText('Repeat the new password'), 'a new password 2');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(screen.getByText("The new passwords don't match. Type the same password twice.")).toBeTruthy();
  });

  it('shows the server message for a wrong current password', async () => {
    server.use(
      http.post(`${API}/me/password`, () => problem(422, 'validation_failed', 'The current password is wrong.')),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/settings/profile');
    await user.type(await screen.findByLabelText('Current password'), 'wrong password 1');
    await user.type(screen.getByLabelText('New password'), 'a new password 1');
    await user.type(screen.getByLabelText('Repeat the new password'), 'a new password 1');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(await screen.findByText('The current password is wrong.')).toBeTruthy();
    expect(router.state.location.pathname).toBe('/settings/profile');
  });

  it('saves the display name', async () => {
    server.use(
      http.patch(`${API}/me`, async ({ request }) => {
        const { displayName } = (await request.json()) as { displayName: string };
        db.user = { ...db.user, displayName };
        return HttpResponse.json(db.user);
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/profile');
    const name = await screen.findByLabelText('Name');
    await user.clear(name);
    await user.type(name, 'Alex K.');
    await user.click(screen.getByRole('button', { name: 'Save name' }));
    expect(await screen.findByText('Saved your name')).toBeTruthy();
    expect(db.user.displayName).toBe('Alex K.');
  });

  it('deletes the account after the password and returns to sign-in', async () => {
    let password = '';
    server.use(
      http.delete(`${API}/me`, async ({ request }) => {
        password = ((await request.json()) as { password: string }).password;
        db.signedIn = false;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/settings/data');
    expect((await screen.findByRole('link', { name: 'Download export' })).getAttribute('href')).toBe(
      '/api/v1/me/export',
    );
    await user.click(screen.getByRole('button', { name: 'Delete account…' }));
    const dlg = screen.getByRole('dialog', { name: 'Delete account' });
    await user.type(within(dlg).getByLabelText('Password'), 'my password 123');
    await user.click(within(dlg).getByRole('button', { name: 'Delete account' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login');
    });
    expect(password).toBe('my password 123');
  });

  it('sends unknown sections to General, where the import setting lives', async () => {
    const { router } = renderApp('/settings/nope');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/general');
    });
    expect(await screen.findByRole('checkbox', { name: 'Import known layouts without review' })).toBeTruthy();
  });
});

describe('colour themes', () => {
  it('offers the built-in and approved themes and saves the choice', async () => {
    const tokens = {
      dark: { bg: '#000000', panel: '#111111', text: '#ffffff', accent: '#ffff00' },
      light: { bg: '#ffffff' },
    };
    db.themes = [{ id: 'sea', name: 'Sea', tokens }];
    renderApp('/settings/general');
    const group = await screen.findByRole('radiogroup', { name: 'Colour theme' });
    await waitFor(() => {
      const names = within(group)
        .getAllByRole('radio')
        .map((r) => r.textContent);
      expect(names).toEqual(['AaAaIndigo', 'AaAaFjord', 'AaAaGrove', 'AaAaEmber', 'AaAaSea']);
    });
    expect(
      within(group)
        .getByRole('radio', { name: /Indigo/ })
        .getAttribute('aria-checked'),
    ).toBe('true');
    await userEvent.click(within(group).getByRole('radio', { name: /Grove/ }));
    expect(document.documentElement.dataset['themeId']).toBe('grove');
    await waitFor(() => {
      expect(db.puts.at(-1)?.prefs['themeId']).toBe('grove');
    });
    await userEvent.click(within(group).getByRole('radio', { name: /Sea/ }));
    expect(document.documentElement.style.getPropertyValue('--accent')).toBe('#ffff00');
  });

  it('imports a theme from a file, keeps it with the preferences and removes it again (Phase 27)', async () => {
    renderApp('/settings/general');
    const group = await screen.findByRole('radiogroup', { name: 'Colour theme' });
    const input = screen.getByLabelText<HTMLInputElement>('Theme file');
    const upload = (text: string) => {
      fireEvent.change(input, { target: { files: [new File([text], 'x.json', { type: 'application/json' })] } });
    };
    // not a theme: said, and nothing changes
    upload('{"format":"other"}');
    expect((await screen.findByRole('alert')).textContent).toContain('not a Finuance theme file');
    // Ember's colours under a name of its own
    const ember = BUILTIN_THEMES.find((t) => t.id === 'ember');
    if (!ember) throw new Error('no Ember theme');
    upload(themeFileText('Campfire', ember.tokens));
    const mine = await within(group).findByRole('radio', { name: /Campfire/ });
    expect(screen.queryByRole('alert')).toBeNull();
    // chosen at once, applied as colours, and saved
    expect(mine.getAttribute('aria-checked')).toBe('true');
    expect(document.documentElement.style.getPropertyValue('--bg')).toBe(ember.tokens.dark['bg']);
    await waitFor(() => {
      expect(db.puts.at(-1)?.prefs['themeId']).toBe('my-campfire');
    });
    expect(db.puts.at(-1)?.prefs['myThemes']).toEqual([{ id: 'my-campfire', name: 'Campfire', tokens: ember.tokens }]);
    const list = screen.getByRole('list', { name: 'Imported themes' });
    await userEvent.click(within(list).getByRole('button', { name: 'Remove the theme Campfire' }));
    await waitFor(() => {
      expect(within(group).queryByRole('radio', { name: /Campfire/ })).toBeNull();
    });
    // back to the default look, with the imported colours taken off again
    expect(document.documentElement.style.getPropertyValue('--bg')).toBe('');
    expect(
      within(group)
        .getByRole('radio', { name: /Indigo/ })
        .getAttribute('aria-checked'),
    ).toBe('true');
  });

  it('keeps the assistant’s memory: a note to write, switch off and clear (Phase 27)', async () => {
    let stored: Schemas['AssistantSettings'] = {
      retentionDays: 2,
      cloudConsent: ['claude'],
      memory: { enabled: true, auto: true, text: 'Paid on the 10th.' },
    };
    const puts: Schemas['AssistantSettings'][] = [];
    server.use(
      http.get(`${API}/assistant/settings`, () => HttpResponse.json(stored)),
      http.put(`${API}/assistant/settings`, async ({ request }) => {
        stored = (await request.json()) as Schemas['AssistantSettings'];
        puts.push(stored);
        return HttpResponse.json(stored);
      }),
    );
    const user = userEvent.setup();
    renderApp('/settings/general');
    // the form starts again from what is stored after every save, so everything is looked up each time
    const note = () => screen.getByRole<HTMLTextAreaElement>('textbox', { name: /^The note/ });
    const card = () => note().closest('.set-card') as HTMLElement;
    const save = () => within(card()).getByRole<HTMLButtonElement>('button', { name: 'Save' });
    await screen.findByRole('textbox', { name: /^The note/ });
    expect(note().value).toBe('Paid on the 10th.');
    expect(card().textContent).toContain('17 of 4,000 characters, about 5 tokens');
    expect(save().disabled).toBe(true);
    // a longer note: saved with everything else of the settings as it was
    await user.type(note(), ' Answer in Polish.');
    await user.click(save());
    await waitFor(() => {
      expect(puts).toHaveLength(1);
    });
    expect(puts[0]).toEqual({
      retentionDays: 2,
      cloudConsent: ['claude'],
      memory: { enabled: true, auto: true, text: 'Paid on the 10th. Answer in Polish.' },
    });
    await waitFor(() => {
      expect(note().value).toBe('Paid on the 10th. Answer in Polish.');
    });
    // too long: said, and Save is closed
    fireEvent.change(note(), { target: { value: 'x'.repeat(4001) } });
    expect(save().disabled).toBe(true);
    expect(note().getAttribute('aria-invalid')).toBe('true');
    fireEvent.change(note(), { target: { value: 'Paid on the 10th. Answer in Polish.' } });
    // switched off: kept, not used
    await user.click(within(card()).getByRole('checkbox', { name: 'Use this note in my questions' }));
    await waitFor(() => {
      expect(puts.at(-1)?.memory).toEqual({ enabled: false, auto: true, text: 'Paid on the 10th. Answer in Polish.' });
    });
    await waitFor(() => {
      expect(
        within(card()).getByRole<HTMLInputElement>('checkbox', { name: 'Use this note in my questions' }).checked,
      ).toBe(false);
    });
    // cleared
    await user.click(within(card()).getByRole('button', { name: 'Clear' }));
    await waitFor(() => {
      expect(puts.at(-1)?.memory?.text).toBe('');
    });
    // each step said so (toasts come one at a time, so the queue is read, not the screen)
    await waitFor(() => {
      expect(useToasts.getState().queue.map((t) => t.msg)).toContain('Memory cleared');
    });
  });

  it('starts from the instance default theme', async () => {
    db.defaults = { themeId: 'ember' };
    renderApp('/settings/general');
    const group = await screen.findByRole('radiogroup', { name: 'Colour theme' });
    await waitFor(() => {
      expect(within(group).getByRole('radio', { name: /Ember/ }).getAttribute('aria-checked')).toBe('true');
    });
    expect(document.documentElement.dataset['themeId']).toBe('ember');
  });
});

describe('browserName', () => {
  it('names common browsers', () => {
    expect(browserName(FF)).toBe('Firefox on macOS');
    expect(browserName(CHROME)).toBe('Chrome on Linux');
    expect(browserName('')).toBe('Unknown browser');
    expect(browserName('curl/8.0')).toBe('curl/8.0');
  });

  it('the sections that moved in Phase 26 still open from their old addresses, without a way "back"', async () => {
    for (const [old, now, hash] of [
      ['/settings/appearance', '/settings/general', '#appearance'],
      ['/settings/sessions', '/settings/security', '#sessions'],
      ['/settings/tokens', '/settings/security', '#tokens'],
    ] as const) {
      const { router, unmount } = renderApp(old);
      await waitFor(() => {
        expect(router.state.location.pathname).toBe(now);
      });
      expect(router.state.location.hash).toBe(hash);
      const nav = await screen.findByRole('navigation', { name: 'Settings' });
      expect(within(nav).queryByRole('link', { name: 'Back to transactions' })).toBeNull();
      for (const gone of ['Appearance', 'Sessions', 'Access tokens'])
        expect(within(nav).queryByRole('link', { name: gone })).toBeNull();
      unmount();
    }
    renderApp('/settings/general');
    expect(await screen.findByText('Appearance')).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: 'Import known layouts without review' })).toBeTruthy();
  });
});
