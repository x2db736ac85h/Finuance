// The insights pane against the legacy demo file (PLAN Phase 11 E2E). Expected numbers are the
// legacy charts' table twins (testdata/oracle/charts/demo.json, case "all-time"), in the test
// browser's en-US formats.
import { expect, test, type Page } from '@playwright/test';

import type { components } from '../src/api/schema';

import { API_HEADERS, importViaApi, signUp } from './helpers';

type Schemas = components['schemas'];

const TOTAL = 917;

async function setup(page: Page): Promise<string> {
  await signUp(page);
  const { accountId } = await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto(`/t/${accountId}`);
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  return accountId;
}

const shown = (page: Page) => page.locator('footer.statusbar .st').first();
const card = (page: Page, title: string) => page.getByRole('region', { name: title });

test('the cards show the legacy numbers for the demo year', async ({ page }) => {
  await setup(page);
  const sum = card(page, 'Summary');
  await expect(sum.locator('.kpi .n').first()).toHaveText('97,014.46');
  await expect(sum.locator('.kpi .d')).toHaveText([
    'all time',
    '917 operations',
    'saved 31% of income',
    'over 442 days',
    'WSPÓLNOTA MIESZKANIOWA · 02.03.2025',
  ]);
  await expect(card(page, 'Spending by category').locator('.card-h .sub')).toHaveText('11 categories');
  await expect(card(page, 'Spending by category').locator('.lg').first()).toContainText('Mieszkanie42,75044%');
  await expect(card(page, 'Top payees').locator('.card-h .sub')).toHaveText('top 23 of 23');
});

test('a category click filters the list; the count matches the API', async ({ page }) => {
  const accountId = await setup(page);
  const expected = (await (
    await page.request.post('/api/v1/transactions/query', {
      headers: API_HEADERS,
      data: { context: { accountIds: [accountId], filters: [{ column: 'category', cond: '=Mieszkanie' }] }, limit: 1 },
    })
  ).json()) as Schemas['TransactionPage'];
  await card(page, 'Spending by category').locator('.lg', { hasText: 'Mieszkanie' }).click();
  await expect(page.getByRole('button', { name: /^Remove Category/ })).toBeVisible();
  await expect(shown(page)).toHaveText(`${String(expected.total ?? 0)} of ${String(TOTAL)} rows`);
  // the other charts follow the filter
  await expect(card(page, 'Spending by category').locator('.card-h .sub')).toHaveText('1 category');
  await expect(card(page, 'Top payees').locator('.hb')).toHaveCount(1);
  // the same click again clears it
  await card(page, 'Spending by category').locator('.lg', { hasText: 'Mieszkanie' }).click();
  await expect(shown(page)).toHaveText(`${String(TOTAL)} of ${String(TOTAL)} rows`);
});

test('a month click sets the period; a second click shows all time again', async ({ page }) => {
  await setup(page);
  const period = page.getByRole('combobox', { name: 'Period' });
  await card(page, 'Cash flow by month').locator('rect[data-month="2025-01"]').click();
  await expect(period).toHaveValue('m:2025-01');
  await expect(shown(page)).toHaveText(`71 of ${String(TOTAL)} rows`);
  await expect(card(page, 'Summary').locator('.card-h .sub')).toHaveText('Jan 2025');
  await expect(card(page, 'Month pace').locator('.ch-x')).toContainText('Jan');
  await card(page, 'Cash flow by month').locator('rect[data-month="2025-01"]').click();
  await expect(period).toHaveValue('all');
  await expect(shown(page)).toHaveText(`${String(TOTAL)} of ${String(TOTAL)} rows`);
});

test('the window shows the table twin with the same numbers as the API', async ({ page }) => {
  const accountId = await setup(page);
  const api = (await (
    await page.request.post('/api/v1/insights/cashflow', {
      headers: API_HEADERS,
      data: { context: { accountIds: [accountId] } },
    })
  ).json()) as Schemas['InsightCashflow'];
  await card(page, 'Cash flow by month')
    .getByRole('button', { name: /Open larger/ })
    .click();
  const dlg = page.getByRole('dialog', { name: 'Cash flow by month' });
  await dlg.getByRole('combobox', { name: 'Range' }).selectOption('all');
  await dlg.getByRole('button', { name: 'Table' }).click();
  const rows = dlg.locator('table.mt tbody tr');
  await expect(rows).toHaveCount(api.months.length);
  // newest first, as the legacy twin: March 2025 | 9,323.00 PLN | -5,151.82 PLN | 4,171.18 PLN | 34
  await expect(rows.first().locator('td')).toHaveText([
    'March 2025',
    '9,323.00 PLN',
    '-5,151.82 PLN',
    '4,171.18 PLN',
    '34',
  ]);
  const last = api.months.at(-1);
  expect(last).toMatchObject({ month: '2025-03', incomeMinor: 932300, expenseMinor: 515182, count: 34 });
  await expect(rows.last().locator('td').first()).toHaveText('January 2024');

  // ← / → step through the cards; Esc closes
  await dlg.getByText('Cash flow by month').click();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('dialog', { name: 'Month pace' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
});
