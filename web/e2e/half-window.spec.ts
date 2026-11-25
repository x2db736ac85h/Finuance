// Half a screen (Phase 26, U11; the maintainer, 2026-11-25): the app in a window 960 px wide (half
// of a 1920 px screen) and 720 px wide (half of 1440), in both colour modes. On every page and with
// every small surface open, nothing makes the page scroll sideways, nothing leaves the window, the
// title bar stays one row, no button's text is cut, and dialogs keep their inset. axe runs on each.
import { expect, test, type Page } from '@playwright/test';

import { check } from './axe';
import { dialogInset, importViaApi, sharedGroupViaApi, signUp } from './helpers';

/** What is wrong with the layout of what is on screen now; each problem names its element. */
async function problems(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const out: string[] = [];
    const name = (e: Element) => (e.getAttribute('aria-label') ?? (e.className || e.tagName)).slice(0, 50);
    const over = document.documentElement.scrollWidth - vw;
    if (over > 0) out.push(`the page scrolls sideways by ${String(over)} px`);
    const parts = '.modal-card, .pop, .titlebar, .toolbar, .set-nav, .set-body, .sh-page > *, .statusbar';
    for (const e of document.querySelectorAll(parts)) {
      const r = e.getBoundingClientRect();
      if (r.width === 0) continue;
      if (r.right > vw + 1 || r.left < -1)
        out.push(`${name(e)} leaves the window (${String(Math.round(r.left))}–${String(Math.round(r.right))})`);
      if (e.matches('.modal-card, .pop') && r.bottom > vh + 1) out.push(`${name(e)} goes below the window`);
    }
    const bar = document.querySelector('.titlebar');
    if (bar) {
      const r = bar.getBoundingClientRect();
      for (const c of bar.children) {
        const b = c.getBoundingClientRect();
        if (b.width === 0) continue;
        if (b.top < r.top - 1 || b.bottom > r.bottom + 1) out.push(`title bar: ${name(c)} wraps onto another row`);
        if (b.right > vw + 1) out.push(`title bar: ${name(c)} is cut off`);
      }
      if (bar.scrollWidth > bar.clientWidth + 1) out.push('the title bar is wider than the window');
    }
    for (const b of document.querySelectorAll<HTMLElement>('.btn, .mi')) {
      if (b.getBoundingClientRect().width > 0 && b.scrollWidth > b.clientWidth + 2)
        out.push(`the button "${b.textContent.trim().slice(0, 30)}" is cut`);
    }
    return out;
  });
}

const PAGES = [
  '/transactions/all',
  '/overview/all',
  '/shared',
  '/settings/general',
  '/settings/profile',
  '/settings/security',
  '/settings/activity',
  '/settings/categories',
  '/settings/rules',
  '/settings/data',
];

for (const width of [960, 720]) {
  test(`a window ${String(width)} px wide: pages, popovers and dialogs stay whole`, async ({ page }) => {
    test.setTimeout(240_000);
    await page.setViewportSize({ width, height: 800 });
    await signUp(page);
    await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
    const group = await sharedGroupViaApi(page);
    const look = async (what: string, within?: string) => {
      expect(await problems(page), `${what} at ${String(width)} px`).toEqual([]);
      await check(page, `${what} ${String(width)}`, within);
      // axe leaves the colour mode at light: the layout is measured once more in it
      expect(await problems(page), `${what} at ${String(width)} px, light`).toEqual([]);
    };

    for (const path of PAGES) {
      await page.goto(path);
      await expect(page.locator('.titlebar')).toBeVisible();
      if (path.startsWith('/transactions')) await expect(page.locator('tr[data-i]').first()).toBeVisible();
      if (path.startsWith('/overview')) await expect(page.getByText('Recurring payments').first()).toBeVisible();
      if (path === '/shared') await expect(page.getByRole('region', { name: 'Friends' })).toBeVisible();
      if (path.startsWith('/settings')) await expect(page.locator('.set-body')).toBeVisible();
      await look(path);
    }

    // the small surfaces over the table
    await page.goto('/transactions/all');
    await expect(page.locator('tr[data-i]').first()).toBeVisible();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Quick settings' })).toBeVisible();
    await look('quick settings', '.pop');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: /^Notifications/ }).click();
    await expect(page.getByRole('dialog', { name: 'Notifications' })).toBeVisible();
    await look('notifications', '.pop');
    await page.keyboard.press('Escape');
    await page.keyboard.press('?');
    await expect(page.getByRole('dialog', { name: 'Keyboard shortcuts' })).toBeVisible();
    await look('keyboard shortcuts', '.modal-card');
    await page.keyboard.press('Escape');

    // dialogs: whole, and their controls away from the edge
    const dialog = async (name: string) => {
      await expect(page.getByRole('dialog', { name })).toBeVisible();
      await look(`dialog ${name}`, '.modal-card');
      expect(await dialogInset(page), `inset of ${name} at ${String(width)} px`).toBeGreaterThanOrEqual(12);
      await page.keyboard.press('Escape');
      await expect(page.getByRole('dialog', { name })).toBeHidden();
    };
    await page.goto('/shared');
    await page.getByRole('button', { name: 'New shared group' }).click();
    await dialog('New shared group');
    await page.goto(group);
    await expect(page.getByRole('region', { name: 'Balances' })).toBeVisible();
    await look('a shared group');
    await page.getByRole('button', { name: 'Group menu' }).click();
    await page.getByRole('menuitem', { name: 'Group settings…' }).click();
    await dialog('Group settings');

    await page.goto('/settings/categories');
    await page
      .getByRole('list', { name: 'Categories' })
      .getByRole('button', { name: /^Archive / })
      .first()
      .click();
    await dialog('Archive category');
  });
}
