import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { account, API, db, problem, resetDb, server } from '../test/server';
import { renderApp, warmUpApp } from '../test/render';

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

const app = () => document.getElementById('app');

describe('shell', () => {
  it('shows the empty state with the sidebar hidden for a new user', async () => {
    renderApp('/');
    expect(await screen.findByRole('heading', { name: 'Drop CSV files here' })).toBeTruthy();
    expect(app()?.className).toContain('no-sb');
    expect(app()?.className).toContain('is-empty');
    expect(screen.getByText(/Ready — drop CSV files anywhere/)).toBeTruthy();
    expect(screen.getByRole<HTMLInputElement>('textbox', { name: 'Search all columns' }).disabled).toBe(true);
  });

  it('loads demo data: toast, demo account in the sidebar, navigation to it', async () => {
    const demo = account({
      id: '00000000-0000-4000-8000-0000000000d1',
      name: 'Demo account',
      isDemo: true,
      txCount: 412,
    });
    server.use(
      http.post(`${API}/demo-data`, () => {
        db.accounts = [demo];
        return HttpResponse.json({ accountId: demo.id, importId: demo.id, transactions: 412 }, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Load demo data' }));
    expect(await screen.findByText('Loaded demo data: 412 transactions')).toBeTruthy();
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/t/${demo.id}`);
    });
    await user.click(screen.getByRole('button', { name: 'Toggle sidebar' }));
    const nav = screen.getByRole('navigation', { name: 'Explorer' });
    const link = within(nav).getByRole('link', { name: /Demo account/ });
    expect(link.parentElement?.className).toBe('fi acc on');
    expect(within(link).getByText('demo')).toBeTruthy();
    expect(within(link).getByText((412).toLocaleString())).toBeTruthy();
    expect(within(nav).getByRole('link', { name: /All accounts/ })).toBeTruthy();
    expect(app()?.className).not.toContain('is-empty');
  });

  it('explains a failed demo load', async () => {
    server.use(http.post(`${API}/demo-data`, () => problem(409, 'conflict', 'You already have a demo account.')));
    const user = userEvent.setup();
    renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Load demo data' }));
    expect((await screen.findByRole('alert')).textContent).toBe('You already have a demo account.');
  });

  it('applies saved preferences before the first render', async () => {
    db.prefs = { version: 4, prefs: { theme: 'light', ui: 1.2, sbOpen: true } };
    db.accounts = [account({ txCount: 3 })];
    renderApp('/');
    await screen.findByRole('navigation', { name: 'Explorer' });
    expect(document.documentElement.dataset['theme']).toBe('light');
    expect(document.documentElement.style.getPropertyValue('--ui-scale')).toBe('1.2');
    expect(app()?.className).not.toContain('no-sb');
    expect(db.puts).toHaveLength(0);
  });

  it('saves a theme change from the settings popover', async () => {
    const user = userEvent.setup();
    renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    await user.click(screen.getByRole('button', { name: 'Light' }));
    expect(document.documentElement.dataset['theme']).toBe('light');
    await waitFor(
      () => {
        expect(db.puts.at(-1)?.prefs).toMatchObject({ theme: 'light' });
      },
      { timeout: 2000 },
    );
    expect(db.puts[0]?.version).toBe(1);
  });

  it('keeps the quick settings to looks, the shortcuts and All settings', async () => {
    const user = userEvent.setup();
    const { router } = renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    const pop = screen.getByRole('dialog', { name: 'Quick settings' });
    // the theme as one button, Mode and the size slider; no import option, account pages or sign-out
    expect(
      within(pop)
        .getByRole('button', { name: /Indigo/ })
        .getAttribute('aria-expanded'),
    ).toBe('false');
    expect(within(pop).queryByRole('radiogroup')).toBeNull();
    expect(within(pop).getByRole('slider', { name: 'Interface size' })).toBeTruthy();
    expect(within(pop).queryByRole('checkbox')).toBeNull();
    expect(within(pop).queryByRole('menuitem')).toBeNull();
    expect(within(pop).queryByRole('button', { name: 'Sign out' })).toBeNull();
    await user.click(within(pop).getByRole('button', { name: 'All settings' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/general');
    });
    await user.click(screen.getByRole('button', { name: 'Settings' }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Quick settings' })).getByRole('button', {
        name: /Keyboard shortcuts/,
      }),
    );
    expect(await screen.findByRole('dialog', { name: 'Keyboard shortcuts' })).toBeTruthy();
  });

  it('unfolds the other themes from the quick settings and picks one', async () => {
    const user = userEvent.setup();
    renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    const pop = screen.getByRole('dialog', { name: 'Quick settings' });
    await user.click(within(pop).getByRole('button', { name: /Indigo/ }));
    const group = within(pop).getByRole('radiogroup', { name: 'Colour theme' });
    await user.click(within(group).getByRole('radio', { name: /Fjord/ }));
    expect(document.documentElement.dataset['themeId']).toBe('fjord');
    // folded again, now showing the new theme
    expect(within(pop).queryByRole('radiogroup')).toBeNull();
    expect(within(pop).getByRole('button', { name: /Fjord/ })).toBeTruthy();
    await waitFor(
      () => {
        expect(db.puts.at(-1)?.prefs).toMatchObject({ themeId: 'fjord' });
      },
      { timeout: 2000 },
    );
  });

  it('sizes the interface with the slider: Auto in the middle, steps to each side', async () => {
    const user = userEvent.setup();
    renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Settings' }));
    const slider = within(screen.getByRole('dialog', { name: 'Quick settings' })).getByRole('slider', {
      name: 'Interface size',
    });
    expect((slider as HTMLInputElement).value).toBe('0');
    expect(slider.getAttribute('aria-valuetext')).toMatch(/^Auto · /);
    fireEvent.change(slider, { target: { value: '1' } });
    await waitFor(() => {
      expect(slider.getAttribute('aria-valuetext')).toMatch(/^Larger · /);
    });
    await waitFor(
      () => {
        expect(db.puts.at(-1)?.prefs).toMatchObject({ ui: 'auto', uiStep: 1 });
      },
      { timeout: 2000 },
    );
  });

  it('opens the keyboard shortcuts with ? and closes them with Esc', async () => {
    const user = userEvent.setup();
    renderApp('/');
    await screen.findByRole('heading', { name: 'Drop CSV files here' });
    await user.keyboard('?');
    const dlg = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' });
    expect(within(dlg).getByText('Search all columns')).toBeTruthy();
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('does not open the shortcuts while typing', async () => {
    db.accounts = [account({ txCount: 3 })];
    const user = userEvent.setup();
    renderApp('/');
    const search = await screen.findByRole('textbox', { name: 'Search all columns' });
    await waitFor(() => {
      expect((search as HTMLInputElement).disabled).toBe(false);
    });
    await user.keyboard('/');
    expect(document.activeElement).toBe(search);
    await user.keyboard('?');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect((search as HTMLInputElement).value).toBe('?');
    await user.keyboard('{Escape}');
    expect((search as HTMLInputElement).value).toBe('');
    await user.keyboard('{Escape}');
    expect(document.activeElement).not.toBe(search);
  });

  it('collapses the sidebar on a narrow window and restores it (legacy)', async () => {
    db.prefs = { version: 1, prefs: { sbOpen: true } };
    renderApp('/');
    await screen.findByRole('navigation', { name: 'Explorer' });
    expect(app()?.className).not.toContain('no-sb');
    const resize = (w: number) => {
      act(() => {
        window.innerWidth = w;
        window.dispatchEvent(new Event('resize'));
      });
    };
    const before = window.innerWidth;
    resize(800);
    expect(app()?.className).toContain('no-sb');
    resize(1200);
    expect(app()?.className).not.toContain('no-sb');
    resize(before);
  });

  it('sends a signed-out visitor to the sign-in page and back', async () => {
    db.signedIn = false;
    const user = userEvent.setup();
    const { router } = renderApp('/settings/profile');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login');
    });
    expect(router.state.location.search).toBe('?next=%2Fsettings%2Fprofile');
    server.use(
      http.post(`${API}/auth/login`, () => {
        db.signedIn = true;
        return HttpResponse.json(db.user);
      }),
    );
    // the address changes a moment before the sign-in page has rendered (a race under load)
    await user.type(await screen.findByRole('textbox', { name: 'Email' }), 'alex@example.com');
    await user.type(screen.getByLabelText('Password'), 'correct horse battery');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeTruthy();
    expect(router.state.location.pathname).toBe('/settings/profile');
  });

  it('the account menu has who is signed in, Profile and Sign out, nothing else', async () => {
    db.user.isAdmin = true;
    const user = userEvent.setup();
    const { router } = renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Account menu' }));
    const menu = screen.getByRole('menu', { name: 'Account' });
    expect(within(menu).getByText(db.user.email)).toBeTruthy();
    expect(
      within(menu)
        .getAllByRole('menuitem')
        .map((m) => m.textContent),
    ).toEqual(['Profile', 'Sign out']);
    await user.click(within(menu).getByRole('menuitem', { name: 'Profile' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/profile');
    });
  });

  it('signs out from the account menu', async () => {
    const user = userEvent.setup();
    const { router } = renderApp('/');
    await user.click(await screen.findByRole('button', { name: 'Account menu' }));
    await user.click(screen.getByRole('menuitem', { name: 'Sign out' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login');
    });
    await waitFor(() => {
      expect(db.signedIn).toBe(false);
    });
  });
});

describe('sidebar import history', () => {
  type Summary = import('../api/client').Schemas['ImportSummary'];
  const imp = (over: Partial<Summary>, withAccount = true): Summary => ({
    id: '00000000-0000-4000-8000-0000000000c1',
    ...(withAccount ? { accountId: '00000000-0000-4000-8000-0000000000a1' } : {}),
    filename: 'styczen.csv',
    sizeBytes: 100,
    status: 'committed' as const,
    mappingSource: 'detected' as const,
    createdAt: '2026-10-20T10:00:00Z',
    committedAt: '2026-10-20T10:00:05Z',
    stats: { rows: 12, new: 10, duplicates: 2 },
    ...over,
  });

  it('folds out an account’s imports with counts, labels and a menu', async () => {
    db.prefs = { version: 1, prefs: { sbOpen: true } };
    db.accounts = [account({ txCount: 10 })];
    db.imports = [
      imp({ profileLabel: 'b2' }),
      imp({
        id: '00000000-0000-4000-8000-0000000000c2',
        filename: 'luty.csv',
        status: 'failed',
        error: 'Database error',
        stats: { rows: 3 },
      }),
      imp(
        { id: '00000000-0000-4000-8000-0000000000c3', filename: 'draft.csv', status: 'previewed', stats: { rows: 4 } },
        false,
      ),
    ];
    let deleted = '';
    server.use(
      http.delete(`${API}/imports/:id`, ({ params }) => {
        deleted = String(params['id']);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/');
    const nav = await screen.findByRole('navigation', { name: 'Explorer' });
    // previews without an account are listed on their own
    expect(await within(nav).findByText('Not imported yet')).toBeTruthy();
    expect(within(nav).getByText('draft.csv')).toBeTruthy();

    await user.click(within(nav).getByRole('button', { name: 'Show imports' }));
    const row = within(nav).getByText('styczen.csv').closest('.fi') as HTMLElement;
    expect(within(row).getByText('b2').className).toBe('tag src');
    expect(within(row).getByText('+10 · 2 dup')).toBeTruthy();
    expect(within(nav).getByText('luty.csv').closest('.fi')?.className).toContain('err');

    await user.click(within(row).getByRole('button', { name: 'Actions for styczen.csv' }));
    const menu = await screen.findByRole('menu', { name: 'Actions for styczen.csv' });
    expect(within(menu).getByRole('menuitem', { name: 'Re-map and reprocess' })).toBeTruthy();
    await user.click(within(menu).getByRole('menuitem', { name: 'Delete…' }));
    const dlg = await screen.findByRole('dialog', { name: 'Delete import' });
    expect(dlg.textContent).toContain('another import of this account also contains');
    await user.click(within(dlg).getByRole('button', { name: 'Delete import' }));
    await waitFor(() => {
      expect(deleted).toBe('00000000-0000-4000-8000-0000000000c1');
    });
    expect(await screen.findByText('Deleted the import of styczen.csv')).toBeTruthy();
  });
});

describe('import settings', () => {
  it('turns the fast path off on Settings → General and saves it', async () => {
    const user = userEvent.setup();
    renderApp('/settings/general');
    const box = await screen.findByRole('checkbox', { name: 'Import known layouts without review' });
    expect((box as HTMLInputElement).checked).toBe(true);
    await user.click(box);
    await waitFor(
      () => {
        expect(db.puts.at(-1)?.prefs).toMatchObject({ importFastPath: false });
      },
      { timeout: 2000 },
    );
  });
});
