// Cash accounts (Phase 19, ADR 0029): a wallet created in the app, an expense typed into it shows
// in the table, can be changed and deleted, and files dropped while looking at it don't go in.
import { expect, test } from '@playwright/test';

import { signUp } from './helpers';

test('a cash account: add an expense, change it, delete it', async ({ page }) => {
  await signUp(page);
  await page.getByRole('button', { name: 'New cash account' }).first().click();
  const create = page.getByRole('dialog', { name: 'New cash account' });
  await create.getByLabel('Name').fill('Portfel');
  await create.getByRole('button', { name: 'Create' }).click();
  await expect(page.locator('.toolbar .vt-title')).toHaveText('Portfel');

  await page.keyboard.press('n');
  const add = page.getByRole('dialog', { name: 'Add a transaction' });
  await add.getByLabel('Amount (PLN)').fill('24,50');
  await add.getByLabel('Paid to').fill('Piekarnia Pod Lipą');
  await add.getByLabel('Category').fill('Jedzenie');
  await add.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByText('Added Piekarnia Pod Lipą')).toBeVisible();

  const row = page.locator('tr[data-i]').filter({ hasText: 'Piekarnia Pod Lipą' });
  await expect(row).toContainText('Jedzenie');
  await expect(row).toContainText(/24[.,]50/);
  // search finds it like an imported row
  await page.getByRole('textbox', { name: 'Search all columns' }).fill('lipa');
  await expect(page.locator('tr[data-i]')).toHaveCount(1);
  await page.getByRole('textbox', { name: 'Search all columns' }).fill('');

  const ins = page.getByRole('complementary', { name: 'Transaction details' });
  await expect(ins.getByText('Added by hand')).toBeVisible();
  await ins.getByRole('button', { name: 'Edit' }).click();
  const edit = page.getByRole('dialog', { name: 'Change the transaction' });
  await edit.getByLabel('Amount (PLN)').fill('30');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(row).toContainText(/30[.,]00/);

  await ins.getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('dialog', { name: 'Delete this transaction?' }).getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('Deleted Piekarnia Pod Lipą')).toBeVisible();
  await expect(page.locator('tr[data-i]')).toHaveCount(0);

  const acts = await page.request.get('/api/v1/me/activity');
  const text = await acts.text();
  for (const a of ['transaction.added', 'transaction.edited', 'transaction.deleted']) expect(text).toContain(a);
});
