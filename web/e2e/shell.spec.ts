// First end-to-end flow (PLAN task 8.13): sign up → load demo data → the sidebar shows the demo
// account with its count; settings persist across a reload; sign out and back in.
import { expect, test } from '@playwright/test';

import { PASSWORD as password, signUp } from './helpers';

test('sign up, load demo data, see the demo account in the sidebar', async ({ page }) => {
  await signUp(page);
  await page.getByRole('button', { name: 'Load demo data' }).click();
  await expect(page.getByRole('status')).toContainText(/Loaded demo data: [\d\s,.  ]+ transactions/);
  await expect(page).toHaveURL(/\/t\/[0-9a-f-]{36}$/);

  // The sidebar is hidden by default (v7); the toggle shows it.
  await page.getByRole('button', { name: 'Toggle sidebar' }).click();
  const explorer = page.getByRole('navigation', { name: 'Explorer' });
  const demo = explorer.getByRole('link', { name: /Demo account/ });
  await expect(demo).toBeVisible();
  await expect(demo.locator('xpath=..')).toHaveClass(/\bon\b/);
  await expect(demo.locator('.tag')).toHaveText('demo');
  await expect(demo.locator('.fc')).toHaveText(/^[1-9][\d\s,.  ]*$/);
});

test('theme, size and sidebar persist across a reload', async ({ page }) => {
  await signUp(page);
  await page.getByRole('button', { name: 'Settings' }).click();
  await page.getByRole('button', { name: 'Light' }).click();
  // the size slider one step right of Auto: 10 % above the auto size (1.12 at a pixel ratio of 1)
  await page.getByRole('slider', { name: 'Interface size' }).focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Toggle sidebar' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  // the debounced save has to land before the reload
  await expect
    .poll(async () => (await (await page.request.get('/api/v1/preferences')).json()) as unknown, { timeout: 5000 })
    .toMatchObject({ prefs: { theme: 'light', ui: 'auto', uiStep: 1, sbOpen: true } });
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('html')).toHaveAttribute('style', /--ui-scale: 1\.23/);
  await expect(page.getByRole('navigation', { name: 'Explorer' })).toBeVisible();
});

test('sign out and sign back in', async ({ page }) => {
  const email = await signUp(page);
  await page.getByRole('button', { name: 'Account menu' }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto('/settings/sessions');
  await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fsessions$/);
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible();
  await expect(page.getByText('this browser')).toBeVisible();
});

test('keyboard shortcuts window opens with ? and closes with Esc', async ({ page }) => {
  await signUp(page);
  await page.keyboard.press('?');
  const dlg = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(dlg).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dlg).toBeHidden();
});

test('no CSP violations on the main screens', async ({ page }) => {
  const violations: string[] = [];
  page.on('console', (m) => {
    if (/Content Security Policy/i.test(m.text())) violations.push(m.text());
  });
  await signUp(page);
  await page.getByRole('button', { name: 'Settings' }).click();
  await page.keyboard.press('Escape');
  await page.goto('/settings/appearance');
  await expect(page.getByRole('heading', { name: 'Appearance' })).toBeVisible();
  expect(violations).toEqual([]);
});
