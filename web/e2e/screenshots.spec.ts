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

  await page.goto('/transactions/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(page.locator('.card').first()).toBeVisible();
  await shoot(page, 'table');

  await page.goto('/overview/all');
  await expect(page.getByText('Recurring payments').first()).toBeVisible();
  await shoot(page, 'overview');

  await page.goto('/transactions/all');
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

  await page.goto('/settings/general');
  await shoot(page, 'settings');

  await page.goto(await sharedGroupViaApi(page));
  await expect(page.getByRole('region', { name: 'Paid vs share' })).toBeVisible();
  await shoot(page, 'shared-group');
  await page.goto('/shared');
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('button', { name: 'Mazury' })).toBeVisible();
  await shoot(page, 'shared');
});

// The README's pictures (Phase 27, F3): the scenes of docs/screenshots/, taken here so that they
// can be renewed whenever the look changes. CI keeps them in the "screenshots" artifact under
// readme/; copying them to docs/screenshots/ is a deliberate step of whoever changes the look.
test('the pictures of the README in both themes', async ({ page }) => {
  test.setTimeout(180_000);
  mkdirSync(`${DIR}/readme`, { recursive: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await signUp(page);
  await shoot(page, 'readme/welcome');
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');

  await page.goto('/transactions/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(page.locator('.card').first()).toBeVisible();
  await shoot(page, 'readme/table');

  // filters: a search and a condition, with their chips
  await page.getByRole('textbox', { name: 'Search all columns' }).fill('warszawa');
  await page.getByRole('button', { name: 'Filter and sort Amount' }).click({ force: true });
  const cond = page.getByRole('textbox', { name: 'Condition for Amount' });
  await cond.fill('<-100');
  await cond.press('Enter');
  await expect(page.locator('.chip').first()).toBeVisible();
  await shoot(page, 'readme/filters');
  await page.getByRole('button', { name: 'Clear all' }).click();
  await page.getByRole('textbox', { name: 'Search all columns' }).fill('');

  // groups: six rows into a new group, then selected again so that the quick bar shows
  const row = (i: number) => page.locator(`tr[data-i="${String(i)}"] td`).nth(2);
  await row(1).click();
  await row(6).click({ modifiers: ['Shift'] });
  const qbar = page.getByRole('toolbar', { name: 'Selected transactions' });
  await expect(qbar).toContainText('6 selected');
  await qbar.getByRole('button', { name: 'New group' }).click();
  await page.getByRole('textbox', { name: 'Group name' }).fill('Italy trip');
  await page.keyboard.press('Enter');
  await expect(page.getByText('6 transactions added to “Italy trip”')).toBeVisible();
  // the same six again: from another row first, so that the first click starts a new selection
  // (a click on the row that is still the selected one would only take the selection away)
  await page.keyboard.press('Escape');
  await row(9).click();
  await row(1).click();
  await row(6).click({ modifiers: ['Shift'] });
  await expect(qbar).toContainText('6 selected');
  await expect(page.locator('.toast.on')).toHaveCount(0); // the "added" message has gone
  await shoot(page, 'readme/groups');
  await page.keyboard.press('Escape');

  await page.goto('/overview/all');
  await expect(page.getByText('Recurring payments').first()).toBeVisible();
  await shoot(page, 'readme/overview');

  await page.goto('/transactions/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.locator('input[type=file]').first().setInputFiles(fixture('synthetic/banks/mbank-like.csv'));
  await expect(page.getByRole('dialog')).toBeVisible();
  await shoot(page, 'readme/import');
  await page.keyboard.press('Escape');

  // the assistant: a lookup, what was sent to the model, and the answer
  await page.goto('/transactions/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.keyboard.press('ControlOrMeta+j');
  const panel = page.getByRole('complementary', { name: 'Assistant' });
  await expect(panel.getByRole('heading', { name: 'Ask about your money' })).toBeVisible();
  const box = page.getByRole('textbox', { name: 'Question' });
  await box.fill('What did I pay at "kino"?');
  await box.press('Enter');
  await expect(panel.locator('.meta')).toContainText('1 lookup');
  // the lookup unfolded, so the picture shows what the model received
  await panel.locator('details.step > summary').first().click();
  await shoot(page, 'readme/assistant');
  await page.keyboard.press('ControlOrMeta+j');

  // duplicates: two overlapping statements of one account
  const { accountId } = await importViaApi(
    page,
    'Konto',
    'synthetic/dup-versions/v1_jan-feb.csv',
    'synthetic/dup-versions/v2_jan-mar.csv',
  );
  await page.goto(`/transactions/${accountId}`);
  await page.locator('footer.statusbar').getByRole('button', { name: '4 duplicates skipped' }).click();
  await expect(page.getByRole('dialog', { name: 'Duplicates' })).toBeVisible();
  await shoot(page, 'readme/duplicates');
});

// Phase 19: the table in each built-in colour theme, both modes (theme-<id>-dark/light.png).
test('screenshots of every colour theme', async ({ page }) => {
  test.setTimeout(120_000);
  mkdirSync(DIR, { recursive: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  for (const theme of ['Indigo', 'Fjord', 'Grove', 'Ember']) {
    await page.goto('/settings/general');
    await page
      .getByRole('radiogroup', { name: 'Colour theme' })
      .getByRole('radio', { name: new RegExp(theme) })
      .click();
    await page.goto('/transactions/all');
    await expect(page.locator('tr[data-i]').first()).toBeVisible();
    await expect(page.locator('.card').first()).toBeVisible();
    await shoot(page, `theme-${theme.toLowerCase()}`);
  }
});
