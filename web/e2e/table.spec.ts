// The table view against the legacy demo file (PLAN Phase 10 E2E, a port of the legacy §26
// checks). Expected counts come from the legacy oracle (testdata/oracle/filters/demo.json).
import { readFileSync } from 'node:fs';

import { expect, test, type Page } from '@playwright/test';

import { importViaApi, signUp } from './helpers';

const TOTAL = 917;

async function setup(page: Page): Promise<string> {
  await signUp(page);
  const { accountId } = await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto(`/t/${accountId}`);
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  return accountId;
}

const shown = (page: Page) => page.locator('footer.statusbar .st').first();

/** Sets a column condition through its menu (Enter applies and closes). */
async function condition(page: Page, column: string, cond: string) {
  await page.getByRole('button', { name: `Filter and sort ${column}` }).click({ force: true });
  const box = page.getByRole('textbox', { name: `Condition for ${column}` });
  await box.fill(cond);
  await box.press('Enter');
}

const amountOf = (text: string) => Number(text.replace(/[^\d.-]/g, '')); // en-US grouping in the test browser

test('search: count, accent-insensitive, highlighted', async ({ page }) => {
  await setup(page);
  await expect(shown(page)).toHaveText(`${String(TOTAL)} of ${String(TOTAL)} rows`);
  const search = page.getByRole('textbox', { name: 'Search all columns' });
  await search.fill('WSPÓLNOTA');
  await expect(shown(page)).toHaveText(`15 of ${String(TOTAL)} rows`);
  await search.fill('wspolnota');
  await expect(shown(page)).toHaveText(`15 of ${String(TOTAL)} rows`);
  await expect(page.locator('tr[data-i] mark').first()).toHaveText('WSPÓLNOTA');

  await search.fill('PŁATNOŚĆ');
  await expect(shown(page)).not.toHaveText(`15 of ${String(TOTAL)} rows`);
  const accented = await shown(page).textContent();
  await search.fill('platnosc');
  await expect(shown(page)).toHaveText(accented ?? '');
  await expect(page.locator('.chips')).toContainText('platnosc');
});

test('sort cycles asc → desc → off', async ({ page }) => {
  await setup(page);
  const amounts = async () => (await page.locator('tr[data-i] td.number').allTextContents()).slice(0, 8).map(amountOf);
  const header = page.getByRole('columnheader', { name: /Amount/ });
  await header.click();
  await expect(header).toHaveAttribute('aria-sort', 'ascending');
  await expect.poll(async () => (await amounts())[0]).toBe(-2850);
  const asc = await amounts();
  expect(asc).toEqual([...asc].sort((a, b) => a - b));
  await header.click();
  await expect(header).toHaveAttribute('aria-sort', 'descending');
  await expect.poll(async () => (await amounts())[0]).toBeGreaterThan(9000);
  const desc = await amounts();
  expect(desc).toEqual([...desc].sort((a, b) => b - a));
  await header.click();
  await expect(header).toHaveAttribute('aria-sort', 'none');
  await expect(page.locator('tr[data-i] td.date').first()).toHaveText('17.03.2025'); // newest first again
});

test('each filter operator family matches the legacy counts', async ({ page }) => {
  await setup(page);
  const cases: [string, string, number][] = [
    ['Amount', '>100', 15],
    ['Amount', '-100..-50', 279],
    ['Amount', '!-100..-50', 638],
    ['Amount', '-79,99', 14],
    ['Date', '2025-03', 34],
    ['Date', '01.2025..03.2025', 155],
    ['Date', '<2024-06', 334],
    ['Payee', 'kino', 10],
    ['Payee', '!kino', 907],
    ['Payee', 'lipą', 139],
    ['Payee', '=', 0],
    ['Payee', '!=', TOTAL],
    ['Category', '=rachunki', 29],
  ];
  for (const [column, cond, count] of cases) {
    await condition(page, column, cond);
    await expect(shown(page), `${column} ${cond}`).toHaveText(`${String(count)} of ${String(TOTAL)} rows`);
    await page.getByRole('button', { name: new RegExp(`^Remove ${column} `) }).click();
    await expect(shown(page)).toHaveText(`${String(TOTAL)} of ${String(TOTAL)} rows`);
  }
});

test('value checklist excludes values', async ({ page }) => {
  await setup(page);
  await page.getByRole('button', { name: 'Filter and sort Category' }).click({ force: true });
  const menu = page.getByRole('dialog', { name: 'Category column' });
  await menu.getByRole('checkbox', { name: /^Rachunki/ }).uncheck();
  await expect(shown(page)).toHaveText(`888 of ${String(TOTAL)} rows`);
  await page.keyboard.press('Escape');
  await expect(page.locator('.chips')).toContainText('−1 value');
});

test('hidden columns and widths survive a reload', async ({ page }) => {
  await setup(page);
  await page.getByRole('button', { name: 'Filter and sort Description' }).click({ force: true });
  await page.getByRole('dialog', { name: 'Description column' }).getByRole('button', { name: 'Hide column' }).click();
  await expect(page.getByRole('columnheader', { name: /Description/ })).toHaveCount(0);

  const payee = page.getByRole('columnheader', { name: /Payee/ });
  const before = (await payee.boundingBox())?.width ?? 0;
  const handle = payee.locator('.rz');
  const box = await handle.boundingBox();
  if (!box) throw new Error('no resize handle');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 80, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
  await expect.poll(async () => (await payee.boundingBox())?.width ?? 0).toBeGreaterThan(before + 60);

  // the debounced preferences save lands, then a reload restores both
  await expect
    .poll(async () => JSON.stringify(await (await page.request.get('/api/v1/preferences')).json()))
    .toContain('"description"');
  await page.reload();
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(page.getByRole('columnheader', { name: /Description/ })).toHaveCount(0);
  expect((await payee.boundingBox())?.width ?? 0).toBeGreaterThan(before + 60);
});

test('inspector: Copy as JSON', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await setup(page);
  await condition(page, 'Payee', 'kino');
  await expect(shown(page)).toHaveText(`10 of ${String(TOTAL)} rows`);
  await page.locator('tr[data-i="0"]').click();
  const ins = page.getByRole('complementary', { name: 'Transaction details' });
  await expect(ins.getByText('Original row')).toBeVisible();
  await ins.getByRole('button', { name: 'Copy as JSON' }).click();
  await expect(page.getByText('Copied the transaction as JSON')).toBeVisible();
  const json = JSON.parse(await page.evaluate(() => navigator.clipboard.readText())) as Record<string, unknown>;
  expect(json['Payee']).toMatch(/^KINO POD GWIAZDAMI/);
  expect(json['Original row']).toMatchObject({ 'Nadawca / Odbiorca': expect.stringMatching(/^KINO/) });
});

test('export: the rows shown, as CSV', async ({ page }) => {
  await setup(page);
  await condition(page, 'Payee', 'kino');
  await expect(shown(page)).toHaveText(`10 of ${String(TOTAL)} rows`);
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('button', { name: 'Export' }).click(),
  ]);
  expect(download.suggestedFilename()).toMatch(/^finuance-demo-\d{4}-\d{2}-\d{2}\.csv$/);
  const text = readFileSync(await download.path(), 'utf8');
  const lines = text.replace(/^\uFEFF/, '').split('\r\n');
  expect(text.startsWith('\uFEFF')).toBe(true);
  expect(lines[0]).toBe('Date;Payee;Description;Amount;Category');
  expect(lines).toHaveLength(11);
  expect(lines.slice(1).every((l) => l.includes(';KINO POD GWIAZDAMI'))).toBe(true);
  await expect(page.getByText('Exported 10 rows')).toBeVisible();
});

test('keyboard moves the selection across a window boundary', async ({ page }) => {
  await setup(page);
  const wrap = page.locator('.grid-wrap');
  const rowH = (await page.locator('tr[data-i="0"]').boundingBox())?.height ?? 30;
  await wrap.evaluate((el, y) => {
    el.scrollTop = y;
  }, 190 * rowH);
  await page.locator('tr[data-i="198"]').click();
  await expect(page.locator('footer.statusbar')).toContainText('Row 199');
  for (let i = 0; i < 4; i++) await page.keyboard.press('ArrowDown'); // into the second window (200+)
  await expect(page.locator('footer.statusbar')).toContainText('Row 203');
  await expect(page.locator('tr[data-i="202"]')).toHaveClass(/\bsel\b/);
  await page.keyboard.press('k');
  await expect(page.locator('footer.statusbar')).toContainText('Row 202');
  await page.keyboard.press('PageDown');
  await expect(page.locator('footer.statusbar')).not.toContainText('Row 202');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('complementary', { name: 'Transaction details' })).toBeHidden();
});
