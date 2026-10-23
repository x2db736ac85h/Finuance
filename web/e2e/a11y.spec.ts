// Accessibility (PLAN task 14.4): axe on the main screens, in both themes. Serious and critical
// violations fail the test; the keyboard-only checklist is in docs/a11y.md.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

import { fixture, importViaApi, signUp } from './helpers';

async function check(page: Page, name: string) {
  for (const scheme of ['dark', 'light'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    // the theme switch fades colours; measure once every transition has ended
    await page.waitForFunction(() => document.getAnimations().length === 0);
    const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    const bad = r.violations
      .filter((v) => v.impact === 'serious' || v.impact === 'critical')
      .map((v) => ({
        id: v.id,
        impact: v.impact,
        help: v.help,
        nodes: v.nodes.slice(0, 5).map((n) => n.target.join(' ')),
      }));
    expect(bad, `${name} (${scheme})`).toEqual([]);
  }
}

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

  for (const s of ['appearance', 'profile', 'sessions', 'data']) {
    await page.goto(`/settings/${s}`);
    await expect(page.locator('.set-body')).toBeVisible();
    await check(page, `settings ${s}`);
  }

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.keyboard.press('ControlOrMeta+j');
  await expect(page.getByRole('complementary', { name: 'Assistant' })).toBeVisible();
  await check(page, 'assistant');
});
