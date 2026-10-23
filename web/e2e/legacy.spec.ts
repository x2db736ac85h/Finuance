// Migrating backups of the single-file app (PLAN task 14.1, 14.2): the oracle's plain and locked
// backups go through Settings → Data, the worker runs the migration, and the result is in the app:
// the accounts, the groups with their labelled transactions, the budgets and the chats.
import { expect, test, type Page } from '@playwright/test';

import { API_HEADERS, fixture, signUp } from './helpers';

async function startWizard(page: Page, backup: string) {
  await page.goto('/settings/data');
  await page.getByLabel('Backup file').setInputFiles(fixture(`legacy-backup/${backup}`));
  return page.getByRole('dialog', { name: 'Import from the single-file app' });
}

async function migrate(page: Page) {
  const dialog = page.getByRole('dialog', { name: 'Import from the single-file app' });
  await expect(dialog.getByText(/3 files · 2 groups · 8 labels/)).toBeVisible();
  await dialog.getByRole('textbox', { name: 'Name of new account 1' }).fill('Konto z kopii');
  await dialog.getByRole('button', { name: 'Start migration' }).click();
  await expect(dialog.getByText(/Migration finished/)).toBeVisible({ timeout: 60_000 });
  await expect(dialog.getByText('Labels: 8 of 8 found their transactions')).toBeVisible();
  await expect(dialog.getByText('Duplicate decisions: 2 of 2 applied')).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();
}

test('migrates the plain backup: accounts, labelled transactions, budgets, chats', async ({ page }) => {
  await signUp(page);
  await startWizard(page, 'backup-plain.json');
  await migrate(page);
  await expect(page.getByText(/Last migration .*: finished/)).toBeVisible();

  // the merged legacy view: 917 demo rows + the two duplicate versions (11 rows, 4 merged, 1 kept twice)
  const accounts = (await (await page.request.get('/api/v1/accounts', { headers: API_HEADERS })).json()) as {
    name: string;
    txCount: number;
    openingBalance: { amountMinor: number };
  }[];
  expect(accounts).toEqual([
    expect.objectContaining({
      name: 'Konto z kopii',
      txCount: 925,
      openingBalance: { amountMinor: 150000, currency: 'PLN' },
    }),
  ]);

  // the group filter shows the five transactions labelled in the legacy app
  await page.goto('/t/all');
  await page.getByRole('button', { name: 'Groups' }).click();
  await page.getByRole('button', { name: /^Wakacje nad morzem 5 ·/ }).click();
  await expect(page.locator('tr[data-i]')).toHaveCount(5);

  const budgets = (await (await page.request.get('/api/v1/budgets', { headers: API_HEADERS })).json()) as {
    budgets: { amountMinor: number; category: { name: string } }[];
  };
  expect(budgets.budgets.map((b) => [b.category.name, b.amountMinor])).toEqual([
    ['Artykuły spożywcze', 150000],
    ['Rachunki', 40000],
    ['Sport', 10000],
  ]);
  const chats = (await (await page.request.get('/api/v1/assistant/chats', { headers: API_HEADERS })).json()) as {
    chats: { title: string }[];
  };
  expect(chats.chats).toHaveLength(2);
});

test('the locked backup needs its passphrase; a wrong one is refused in the browser', async ({ page }) => {
  await signUp(page);
  const sent: string[] = [];
  page.on('request', (r) => {
    if (r.method() === 'POST') sent.push(r.url());
  });
  const dialog = await startWizard(page, 'backup-locked.json');
  await dialog.getByLabel('Passphrase').fill('not the passphrase');
  await dialog.getByRole('button', { name: 'Unlock' }).click();
  await expect(dialog.getByText(/The passphrase is wrong/)).toBeVisible({ timeout: 15_000 });
  // nothing was sent while unlocking
  expect(sent.filter((u) => u.includes('/legacy-imports'))).toEqual([]);
  await dialog.getByLabel('Passphrase').fill('oracle-test-passphrase');
  await dialog.getByRole('button', { name: 'Unlock' }).click();
  await migrate(page);
  // the passphrase never reached the server
  expect(sent.every((u) => !u.includes('passphrase'))).toBe(true);

  // running the same backup again creates nothing new
  await startWizard(page, 'backup-plain.json');
  const again = page.getByRole('dialog', { name: 'Import from the single-file app' });
  await again.getByRole('combobox', { name: 'Account for demo.csv' }).selectOption({ label: 'Konto z kopii' });
  await again.getByRole('combobox', { name: 'Account for v2_jan-mar.csv' }).selectOption({ label: 'Konto z kopii' });
  await again.getByRole('combobox', { name: 'Account for v1_jan-feb.csv' }).selectOption({ label: 'Konto z kopii' });
  await again.getByRole('button', { name: 'Start migration' }).click();
  await expect(again.getByText(/Migration finished.*: 0 new transactions/)).toBeVisible({ timeout: 60_000 });
  await expect(again.getByText('Already imported')).toHaveCount(3);
});
