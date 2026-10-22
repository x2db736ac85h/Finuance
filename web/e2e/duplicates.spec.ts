// The duplicates window on the duplicate-versions fixture set (PLAN Phase 10 duplicates E2E): two
// overlapping exports of one account where the bank re-labelled four transactions.
import { expect, test } from '@playwright/test';

import { importViaApi, signUp } from './helpers';

test('status-bar link, Use this version, Keep both / Merge again, filter', async ({ page }) => {
  await signUp(page);
  const { accountId } = await importViaApi(
    page,
    'Konto',
    'synthetic/dup-versions/v1_jan-feb.csv',
    'synthetic/dup-versions/v2_jan-mar.csv',
  );
  await page.goto(`/t/${accountId}`);
  const status = page.locator('footer.statusbar');
  await expect(status).toContainText('7 of 7 rows');
  const alfa = page.locator('tr[data-i]', { hasText: 'Zakupy luty' });
  await expect(alfa).toContainText('Bez kategorii'); // the first import's version

  await status.getByRole('button', { name: '4 duplicates skipped' }).click();
  const dlg = page.getByRole('dialog', { name: 'Duplicates' });
  await expect(dlg.locator('section.dg')).toHaveCount(4);
  await expect(dlg.getByText('Duplicates are found per account', { exact: false })).toBeVisible();

  // filter
  await dlg.getByRole('textbox', { name: 'Filter duplicates' }).fill('kwiaciarnia');
  await expect(dlg.locator('section.dg')).toHaveCount(1);
  await dlg.getByRole('textbox', { name: 'Filter duplicates' }).fill('');
  await expect(dlg.locator('section.dg')).toHaveCount(4);

  // Use this version: the newer export's category shows in the table.
  const g = dlg.getByRole('region', { name: /28\.02\.2025 SKLEP ALFA/ });
  await expect(g.locator('td.df')).toHaveText(['Artykuły spożywcze']);
  await g.getByRole('button', { name: 'Use this version' }).click();
  await expect(g.locator('tr.k')).toContainText('v2_jan-mar.csv');
  await expect(alfa).toContainText('Artykuły spożywcze');

  // Keep both adds the rows, Merge again removes them.
  const k = dlg.getByRole('region', { name: /KWIACIARNIA/ });
  await k.getByRole('button', { name: 'Keep both' }).click();
  await expect(k.getByText('Kept as separate transactions')).toBeVisible();
  await expect(status).toContainText('8 of 8 rows');
  await expect(status).toContainText('3 duplicates skipped'); // the window hides the page from role queries
  await expect(dlg.getByText('3 rows skipped')).toBeVisible();
  await k.getByRole('button', { name: 'Merge again' }).click();
  await expect(status).toContainText('7 of 7 rows');
  await expect(status).toContainText('4 duplicates skipped');
  await page.keyboard.press('Escape');
  await expect(dlg).toBeHidden();
});
