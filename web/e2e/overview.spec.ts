// The overview against the legacy demo file (PLAN Phase 12 E2E). Expected numbers are the legacy
// cards' table twins and highlight sentences (testdata/oracle/overview/demo.json, case
// "latest-partial": the latest month, 12 months, no budgets), in the test browser's en-US formats.
import { readFileSync } from 'node:fs';

import { expect, test, type Locator, type Page } from '@playwright/test';

import type { components } from '../src/api/schema';

import { API_HEADERS, importViaApi, signUp } from './helpers';

type Schemas = components['schemas'];
type Twin = { cols: string[]; rows: string[][] };
type OracleCard = { sub: string; table?: Twin | null; lines?: { kind: string; text: string }[] };
type Oracle = { cases: { name: string; cards: Record<string, { card: OracleCard; big: OracleCard }> }[] };

const oracle = (JSON.parse(readFileSync('../testdata/oracle/overview/demo.json', 'utf8')) as Oracle).cases.find(
  (c) => c.name === 'latest-partial',
);

const TITLES: Record<string, string> = {
  o_flow: 'Cash flow',
  o_budget: 'Budgets',
  o_recur: 'Recurring payments',
  o_sankey: 'Money flow',
  o_top: 'Largest expenses',
  o_savings: 'Savings rate',
  o_week: 'Spending by weekday',
  o_balance: 'Account balance',
};

async function setup(page: Page): Promise<string> {
  await signUp(page);
  const { accountId } = await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto(`/o/${accountId}`);
  await expect(page.getByRole('img', { name: 'Account balance over time' })).toBeVisible();
  return accountId;
}

const card = (page: Page, title: string): Locator => page.getByRole('region', { name: title });

async function tableOf(scope: Locator): Promise<string[][]> {
  return scope
    .locator('table.mt tbody tr')
    .evaluateAll((trs) => trs.map((tr) => [...tr.querySelectorAll('td')].map((td) => td.textContent)));
}

/**
 * ALLOWED_DIFFS row 13: a half-cent tie in a weekday average shows one cent lower in the legacy
 * app. Accepts exactly that, nothing else.
 */
function halfCentTie(legacy: string, ours: string): boolean {
  const n = (s: string) => Math.round(Number(s.replace(/[^\d.-]/g, '')) * 100);
  return n(ours) - n(legacy) === 1;
}

test('every card shows the legacy numbers for the latest month', async ({ page }) => {
  test.setTimeout(60_000);
  expect(oracle).toBeDefined();
  if (!oracle) return;
  await setup(page);
  await expect(page.getByRole('group', { name: 'Projected spend · PLN' })).toContainText('9,394.50');
  await expect(page.getByText('Month in progress — data until Mar 17, 2025')).toBeVisible();
  await expect(card(page, 'Highlights').locator('.hl-t')).toHaveText(
    (oracle.cards['o_high']?.card.lines ?? []).map((l) => l.text),
  );
  for (const [key, title] of Object.entries(TITLES)) {
    const want = oracle.cards[key]?.big;
    await expect(card(page, title).locator('.card-h .sub')).toHaveText(oracle.cards[key]?.card.sub ?? '');
    await card(page, title)
      .getByTitle(/^Open larger/)
      .click();
    const dialog = page.getByRole('dialog', { name: title });
    await expect(dialog).toBeVisible();
    await dialog.getByRole('button', { name: 'Table' }).click();
    await expect(dialog.locator('table.mt thead th')).toHaveText(want?.table?.cols ?? []);
    const got = await tableOf(dialog);
    const rows = want?.table?.rows ?? [];
    expect(got.length, title).toBe(rows.length);
    for (const [i, row] of rows.entries()) {
      for (const [j, cell] of row.entries()) {
        const ours = got[i]?.[j] ?? '';
        if (ours !== cell && key === 'o_week' && j === 1 && halfCentTie(cell, ours)) continue;
        expect(ours, `${title} row ${String(i)} column ${String(j)}`).toBe(cell);
      }
    }
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  }
});

test('a budget change updates the status and survives a reload', async ({ page }) => {
  await setup(page);
  const budgets = card(page, 'Budgets');
  const input = budgets.getByRole('textbox', { name: 'Budget for Transport' });
  const row = budgets.locator('.br', { hasText: 'Transport' });
  await input.fill('1');
  await input.press('Enter');
  await expect(row.locator('.bst')).toContainText('over');
  await expect(input).toHaveValue('1');
  await page.reload();
  await expect(card(page, 'Budgets').getByRole('textbox', { name: 'Budget for Transport' })).toHaveValue('1');
  await expect(card(page, 'Budgets').locator('.br', { hasText: 'Transport' }).locator('.bst')).toContainText('over');
  // emptying it goes back to the default
  const again = card(page, 'Budgets').getByRole('textbox', { name: 'Budget for Transport' });
  await again.fill('');
  await again.press('Enter');
  await expect
    .poll(async () => ((await (await page.request.get('/api/v1/budgets')).json()) as Schemas['BudgetList']).budgets)
    .toEqual([]);
  // the default comes back in the field
  await expect(again).toHaveValue(/^\d+$/);
  await expect(again).not.toHaveValue('1');
});

test('a Sankey category lands in the filtered table', async ({ page }) => {
  const accountId = await setup(page);
  const expected = (await (
    await page.request.post('/api/v1/transactions/query', {
      headers: API_HEADERS,
      data: { context: { accountIds: [accountId], filters: [{ column: 'category', cond: '=Transport' }] }, limit: 1 },
    })
  ).json()) as Schemas['TransactionPage'];
  await card(page, 'Money flow').locator('rect[data-cat="Transport"]').click();
  await expect(page).toHaveURL(new RegExp(`/t/${accountId}$`));
  await expect(page.getByRole('button', { name: /^Remove Category/ })).toBeVisible();
  await expect(page.locator('footer.statusbar .st').first()).toHaveText(`${String(expected.total ?? 0)} of 917 rows`);
});

test('a largest expense opens the table at its row; a bar picks the month', async ({ page }) => {
  await setup(page);
  await card(page, 'Cash flow').locator('rect[data-month="2025-01"]').click();
  await expect(page.getByRole('combobox', { name: 'Month' })).toHaveValue('2025-01');
  await expect(card(page, 'Largest expenses').locator('.card-h .sub')).toHaveText('January 2025');
  await card(page, 'Largest expenses').locator('tbody tr').first().click();
  await expect(page.locator('tr[data-i].sel')).toBeVisible();
  await expect(page.locator('.inspector')).toContainText('WSPÓLNOTA MIESZKANIOWA');
});
