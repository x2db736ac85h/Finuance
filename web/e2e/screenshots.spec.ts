// The dark/light screenshot set (PLAN task 14.2): the main screens on the demo data, written to
// web/screenshots/ and kept by CI as the "screenshots" artifact of every run.
import { mkdirSync } from 'node:fs';

import { expect, test, type Page } from '@playwright/test';

import { fixture, importViaApi, sharedGroupViaApi, signUp } from './helpers';

const DIR = 'screenshots';

async function shoot(page: Page, name: string) {
  for (const scheme of ['dark', 'light'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expect(page.locator('html')).toHaveAttribute('data-theme', scheme);
    await page.screenshot({ path: `${DIR}/${name}-${scheme}.png` });
  }
}

test('screenshots of the main screens in both themes', async ({ page }) => {
  test.setTimeout(120_000);
  mkdirSync(DIR, { recursive: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await signUp(page);
  await shoot(page, 'empty');
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(page.locator('.card').first()).toBeVisible();
  await shoot(page, 'table');

  await page.goto('/o/all');
  await expect(page.getByText('Recurring payments').first()).toBeVisible();
  await shoot(page, 'overview');

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.locator('input[type=file]').first().setInputFiles(fixture('synthetic/banks/mbank-like.csv'));
  await expect(page.getByRole('dialog')).toBeVisible();
  await shoot(page, 'import');
  await page.keyboard.press('Escape');

  await page.goto('/settings/data');
  await page.getByLabel('Backup file').setInputFiles(fixture('legacy-backup/backup-plain.json'));
  await expect(page.getByText(/3 files · 2 groups/)).toBeVisible();
  await shoot(page, 'migration');
  await page.keyboard.press('Escape');

  await page.goto('/settings/appearance');
  await shoot(page, 'settings');

  await page.goto(await sharedGroupViaApi(page));
  await expect(page.getByRole('region', { name: 'Paid vs share' })).toBeVisible();
  await shoot(page, 'shared-group');
  await page.goto('/s');
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('button', { name: 'Mazury' })).toBeVisible();
  await shoot(page, 'shared');
});

// Phase 19: the table in each built-in colour theme, both modes (theme-<id>-dark/light.png).
test('screenshots of every colour theme', async ({ page }) => {
  test.setTimeout(120_000);
  mkdirSync(DIR, { recursive: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  for (const theme of ['Indigo', 'Fjord', 'Grove', 'Ember']) {
    await page.goto('/settings/appearance');
    await page
      .getByRole('radiogroup', { name: 'Colour theme' })
      .getByRole('radio', { name: new RegExp(theme) })
      .click();
    await page.goto('/t/all');
    await expect(page.locator('tr[data-i]').first()).toBeVisible();
    await expect(page.locator('.card').first()).toBeVisible();
    await shoot(page, `theme-${theme.toLowerCase()}`);
  }
});
