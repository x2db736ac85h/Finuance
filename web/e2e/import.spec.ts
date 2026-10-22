// Import UX end to end (PLAN Phase 9): the preview window against the real API and worker, with
// the counts the legacy oracle gives for the same files.
import { expect, test, type Page } from '@playwright/test';

import overlapAB from '../../testdata/oracle/dedupe/overlap-A-B.json' with { type: 'json' };
import { openFiles, signUp } from './helpers';

const A = 'synthetic/overlap/A_jan-mar.csv';
const B = 'synthetic/overlap/B_mar-may.csv';

/** The import window. */
const win = (page: Page) => page.getByRole('dialog', { name: 'Import' });

/** Creates an account from the window's account picker. */
async function newAccount(page: Page, name: string) {
  const w = win(page);
  const input = w.getByRole('textbox', { name: 'New account name' });
  if (!(await input.isVisible()))
    await w.getByRole('combobox', { name: 'Account' }).selectOption({ label: 'New account…' });
  await input.fill(name);
  await w.getByRole('button', { name: 'Create' }).click();
  await expect(w.getByRole('combobox', { name: 'Account' })).toHaveValue(/[0-9a-f-]{36}/);
}

/** Sets a column's meaning with the role menu on its name. */
async function mapColumn(page: Page, column: string, role: string) {
  await win(page)
    .getByRole('button', { name: new RegExp(column) })
    .click();
  const escaped = role.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  // the item may carry an "auto" hint
  await page.getByRole('menuitem', { name: new RegExp(`^${escaped}(\\s*auto)?$`) }).click();
}

/** The sidebar count of an account (opens the sidebar if needed). */
async function sidebarCount(page: Page, account: string) {
  const nav = page.getByRole('navigation', { name: 'Explorer' });
  if (!(await nav.isVisible())) await page.getByRole('button', { name: 'Toggle sidebar' }).click();
  return nav.getByRole('link', { name: new RegExp(account) }).locator('.fc');
}

/** Transactions of the user through the API, newest first. */
async function transactions(page: Page): Promise<{ amount: { amountMinor: number }; payee: string }[]> {
  const r = await page.request.post('/api/v1/transactions/query', {
    headers: { 'X-Finuance-Client': 'web' },
    data: { context: {}, limit: 100 },
  });
  expect(r.ok()).toBe(true);
  return ((await r.json()) as { rows: { amount: { amountMinor: number }; payee: string }[] }).rows;
}

test('overlapping files: counts match the oracle, delete, duplicate file', async ({ page }) => {
  const ab = overlapAB.default;
  await signUp(page);

  // A: first file of this layout → the window; create the account and import.
  await openFiles(page, A);
  await expect(win(page)).toBeVisible();
  await newAccount(page, 'Overlap');
  const importA = win(page).getByRole('button', { name: /^Import \d+ transactions$/ });
  await expect(importA).toBeEnabled();
  const aRows = Number((await importA.textContent())?.match(/\d+/)?.[0]);
  await importA.click();
  await expect(page.getByRole('status')).toContainText(`Imported ${String(aRows)} transactions`);
  await expect(page).toHaveURL(/\/t\/[0-9a-f-]{36}$/);

  // B: same layout, account in view → the fast path imports it at once.
  await openFiles(page, B);
  const newB = ab.rowCount - aRows;
  await expect(page.getByRole('status')).toContainText(
    `Imported ${String(newB)} transactions · ${String(ab.dupes)} duplicates skipped from B_mar-may.csv`,
  );
  await expect(win(page)).toBeHidden();
  await expect(await sidebarCount(page, 'Overlap')).toHaveText(String(ab.rowCount));

  // Delete A: what B also contains stays, so B's rows remain.
  const nav = page.getByRole('navigation', { name: 'Explorer' });
  await nav.getByRole('button', { name: 'Show imports' }).click();
  await nav.getByRole('button', { name: 'Actions for A_jan-mar.csv' }).click();
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  await page.getByRole('dialog', { name: 'Delete import' }).getByRole('button', { name: 'Delete import' }).click();
  await expect(page.getByRole('status')).toContainText('Deleted the import of A_jan-mar.csv');
  await expect(await sidebarCount(page, 'Overlap')).toHaveText(String(newB + ab.dupes));

  // The same bytes again: reported with a link, nothing imported.
  await openFiles(page, B);
  await expect(page.getByRole('status')).toContainText('B_mar-may.csv was already imported on');
});

test('Windows-1250 file shows Polish letters', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/03-windows-1250.csv');
  await expect(win(page).getByRole('cell', { name: 'ZAŻÓŁĆ GĘŚLĄ JAŹŃ' }).first()).toBeVisible();
  await expect(win(page).locator('.imp-f')).toContainText('windows-1250');
});

test('debit and credit columns commit with the right signs', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/28-debit-credit.csv');
  await expect(win(page)).toBeVisible();
  await mapColumn(page, 'Obciążenia', 'Amount — outflow / debit');
  await mapColumn(page, 'Uznania', 'Amount — inflow / credit');
  await expect(win(page).getByRole('button', { name: /Uznania/ })).toContainText('→ Credit');
  await newAccount(page, 'Debit credit');
  await win(page)
    .getByRole('button', { name: /^Import \d+ transactions$/ })
    .click();
  await expect(page.getByRole('status')).toContainText('Imported 3 transactions');
  const rows = await transactions(page);
  const byPayee = Object.fromEntries(rows.map((r) => [r.payee, r.amount.amountMinor]));
  expect(byPayee['Sklep']).toBe(-4520);
  expect(byPayee['Wynagrodzenie']).toBe(850000);
});

test('a file without a header row, with skip rows', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/37-headerless.csv');
  const w = win(page);
  await expect(w).toBeVisible();
  await w.getByRole('button', { name: 'File has no header row' }).click();
  await expect(w.getByLabel('Data starts at row')).toHaveValue('1');
  await expect(w.locator('.imp-f')).toContainText('no header row · 3 rows');
  await w.getByLabel('Data starts at row').fill('2');
  await expect(w.locator('.imp-f')).toContainText('no header row · 2 rows');
  await expect(w.getByRole('listbox', { name: 'Raw file lines' }).getByRole('option').first()).toHaveClass(/skip/);
});

test('padded preamble: the header is found automatically', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/35-padded-preamble.csv');
  const w = win(page);
  await expect(w.locator('.imp-f')).toContainText('header at row 4 · 3 rows');
  await expect(w.getByRole('option', { name: /Data;Opis;Kwota/ })).toHaveClass(/hdr/);
});

test('footer summary lines are dropped and reported', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/36-footer-summary.csv');
  const w = win(page);
  await expect(w.locator('.imp-f')).toContainText('3 rows · 2 summary rows skipped');
  await expect(w.getByRole('option', { name: /Saldo końcowe/ })).toContainText('summary');
});

test('month-first dates', async ({ page }) => {
  await signUp(page);
  await openFiles(page, 'synthetic/39-month-first-dates.csv');
  const w = win(page);
  await expect(w).toBeVisible();
  await w.getByRole('combobox', { name: 'Dates' }).selectOption('mdy');
  await mapColumn(page, 'Date', 'Date');
  await expect(w.getByRole('cell', { name: /^31\.10\.2025/ })).toBeVisible();
  await newAccount(page, 'US card');
  await w.getByRole('button', { name: /^Import \d+ transactions$/ }).click();
  await expect(page.getByRole('status')).toContainText('Imported 3 transactions');
});

test('fast path: a known layout is imported at once and Undo removes it', async ({ page }) => {
  await signUp(page);
  await openFiles(page, A);
  await newAccount(page, 'Fast');
  await win(page)
    .getByRole('button', { name: /^Import \d+ transactions$/ })
    .click();
  await expect(page).toHaveURL(/\/t\/[0-9a-f-]{36}$/);
  const before = await (await sidebarCount(page, 'Fast')).textContent();

  await openFiles(page, B);
  const toast = page.getByRole('status');
  await expect(toast).toContainText('from B_mar-may.csv');
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect(toast).toContainText('Removed the import of B_mar-may.csv');
  await expect(await sidebarCount(page, 'Fast')).toHaveText(before ?? '');
});
