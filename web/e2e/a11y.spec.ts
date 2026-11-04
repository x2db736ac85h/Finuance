// Accessibility (PLAN task 14.4): axe on the main screens, in both themes. Serious and critical
// violations fail the test; the keyboard-only checklist is in docs/a11y.md.
import { expect, test } from '@playwright/test';

import { check } from './axe';
import { fixture, importViaApi, sharedGroupViaApi, signUp } from './helpers';

test('login and signup', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
  await check(page, 'login');
  await page.goto('/signup');
  await check(page, 'signup');
});

test('table, import preview, overview, settings, assistant', async ({ page }) => {
  test.setTimeout(120_000);
  await signUp(page);
  await check(page, 'empty');
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(page.locator('.card').first()).toBeVisible();
  await check(page, 'table');

  await page.locator('input[type=file]').first().setInputFiles(fixture('synthetic/banks/pekao-like.csv'));
  await expect(page.getByRole('dialog')).toBeVisible();
  await check(page, 'import preview');
  await page.keyboard.press('Escape');

  await page.goto('/o/all');
  await expect(page.getByText('Recurring payments').first()).toBeVisible();
  await check(page, 'overview');

  for (const s of ['appearance', 'profile', 'sessions', 'activity', 'friends', 'data']) {
    await page.goto(`/settings/${s}`);
    await expect(page.locator('.set-body')).toBeVisible();
    await check(page, `settings ${s}`);
  }

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.getByRole('button', { name: /^Notifications/ }).click();
  await expect(page.getByRole('dialog', { name: 'Notifications' })).toBeVisible();
  await check(page, 'notifications');
  await page.keyboard.press('Escape');
  const sidebar = page.getByRole('button', { name: 'Toggle sidebar' });
  if ((await sidebar.getAttribute('aria-pressed')) !== 'true') await sidebar.click(); // collapsed at this width
  await page.getByRole('button', { name: 'New cash account' }).click();
  await expect(page.getByRole('dialog', { name: 'New cash account' })).toBeVisible();
  await check(page, 'new cash account', '[role=dialog]');
  await page.keyboard.press('Escape');
  await page.keyboard.press('ControlOrMeta+j');
  await expect(page.getByRole('complementary', { name: 'Assistant' })).toBeVisible();
  await check(page, 'assistant');
});

// Phase 19: the other built-in colour themes (Indigo is the run above) on the busiest screens.
test('every colour theme: table, overview, the Shared tab and appearance settings', async ({ page }) => {
  test.setTimeout(240_000);
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  const sharedUrl = await sharedGroupViaApi(page);
  for (const theme of ['Fjord', 'Grove', 'Ember']) {
    await page.goto('/settings/appearance');
    await page
      .getByRole('radiogroup', { name: 'Colour theme' })
      .getByRole('radio', { name: new RegExp(theme) })
      .click();
    await expect(page.locator('html')).toHaveAttribute('data-theme-id', theme.toLowerCase());
    await check(page, `${theme} settings`);
    await page.goto('/t/all');
    await expect(page.locator('tr[data-i]').first()).toBeVisible();
    await expect(page.locator('.card').first()).toBeVisible();
    await check(page, `${theme} table`);
    await page.goto('/o/all');
    await expect(page.getByText('Recurring payments').first()).toBeVisible();
    await check(page, `${theme} overview`);
    await page.goto(sharedUrl);
    await expect(page.getByRole('region', { name: 'Paid vs share' })).toBeVisible();
    await check(page, `${theme} shared group`);
  }
});
