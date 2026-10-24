// Groups and multi-selection on the legacy demo file (PLAN Phase 10 "Groups and selection E2E").
import { expect, test, type Page } from '@playwright/test';

import { API_HEADERS as H, importInto, importViaApi, signUp } from './helpers';

const TOTAL = 917;

const shown = (page: Page) => page.locator('footer.statusbar .st').first();
const qbar = (page: Page) => page.getByRole('toolbar', { name: 'Selected transactions' });
const minorOf = (text: string) => Math.round(Number(text.replace(/[^\d.-]/g, '')) * 100); // en-US in the test browser
const money = (minor: number) =>
  new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(minor / 100);

async function setup(page: Page) {
  await signUp(page);
  const r = await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto(`/t/${r.accountId}`);
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  // the drags below need the grid's full height (the 720 px test window halves it with the pane)
  await page.getByRole('button', { name: 'Hide insights' }).click();
  return r;
}

/** Drags from the payee cell of row a to row b. */
async function drag(page: Page, a: number, b: number) {
  const from = await page
    .locator(`tr[data-i="${String(a)}"] td`)
    .nth(2)
    .boundingBox();
  const to = await page
    .locator(`tr[data-i="${String(b)}"] td`)
    .nth(2)
    .boundingBox();
  if (!from || !to) throw new Error('rows not on screen');
  await page.mouse.move(from.x + 10, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + 10, from.y + from.height / 2 + 12, { steps: 2 });
  await page.mouse.move(to.x + 10, to.y + to.height / 2, { steps: 6 });
  await page.mouse.up();
}

async function amountsOf(page: Page, rows: number[]) {
  const out: number[] = [];
  for (const i of rows)
    out.push(minorOf((await page.locator(`tr[data-i="${String(i)}"] td.number`).textContent()) ?? ''));
  return out;
}

test('drag-select five rows: the quick bar sums them; a new group filters to exactly them', async ({ page }) => {
  await setup(page);
  await drag(page, 2, 6);
  await expect(qbar(page)).toContainText('5 selected');
  const sum = (await amountsOf(page, [2, 3, 4, 5, 6])).reduce((a, b) => a + b, 0);
  await expect(qbar(page)).toContainText(money(sum));
  await expect(page.locator('tr.msel')).toHaveCount(5);

  // + → new group → assigned
  await qbar(page).getByRole('button', { name: 'New group' }).click();
  await page.getByRole('textbox', { name: 'Group name' }).fill('Italy trip 2025');
  await page.keyboard.press('Enter');
  await expect(page.getByText('5 transactions added to “Italy trip 2025”')).toBeVisible();
  await expect(qbar(page)).toBeHidden();

  // its chip filter shows exactly those rows
  await page.getByRole('button', { name: /Groups/ }).click();
  await page
    .getByRole('dialog', { name: 'Groups' })
    .getByRole('button', { name: /^Italy trip 2025/ })
    .click();
  await expect(shown(page)).toHaveText(`5 of ${String(TOTAL)} rows`);
  await expect(page.locator('footer.statusbar')).toContainText(money(sum));

  // rename → the active filter follows
  await page.getByRole('button', { name: /Groups/ }).click();
  await page.getByRole('dialog', { name: 'Groups' }).getByRole('button', { name: 'Edit Italy trip 2025' }).click();
  const name = page.getByRole('textbox', { name: 'Group name' });
  await name.fill('Włochy');
  await name.press('Enter');
  await expect(page.locator('.chips')).toContainText('=Włochy');
  await expect(shown(page)).toHaveText(`5 of ${String(TOTAL)} rows`);
  await page.keyboard.press('Escape'); // the menu comes back after the form

  // delete → labels gone, transactions intact
  await page.getByRole('button', { name: /Groups/ }).click();
  await page.getByRole('dialog', { name: 'Groups' }).getByRole('button', { name: 'Delete Włochy' }).click();
  await page.getByRole('dialog', { name: 'Delete group' }).getByRole('button', { name: 'Delete group' }).click();
  await expect(page.getByText('Group “Włochy” deleted — transactions kept')).toBeVisible();
  await expect(shown(page)).toHaveText(`${String(TOTAL)} of ${String(TOTAL)} rows`);
  await expect(page.getByRole('columnheader', { name: /Group/ })).toHaveCount(0);
});

test('Shift-click across a window that is not loaded: count and Σ from the server', async ({ page }) => {
  const { accountId } = await setup(page);
  await page.locator('tr[data-i="0"] td').nth(2).click(); // anchor (and the inspector)
  const wrap = page.locator('.grid-wrap');
  const rowH = (await page.locator('tr[data-i="0"]').boundingBox())?.height ?? 30;
  await wrap.evaluate((el, y) => {
    el.scrollTop = y;
  }, 440 * rowH);
  await page
    .locator('tr[data-i="450"] td')
    .nth(2)
    .click({ modifiers: ['Shift'] });
  await expect(qbar(page)).toContainText('451 selected');
  const ids = (await (
    await page.request.post('/api/v1/transactions/ids', {
      headers: H,
      data: { context: { accountIds: [accountId] }, offset: 0, limit: 451 },
    })
  ).json()) as { amountMinor: number }[];
  await expect(qbar(page)).toContainText(money(ids.reduce((n, x) => n + x.amountMinor, 0)));
  await page.keyboard.press('Escape');
  await expect(qbar(page)).toBeHidden();
});

test('a label survives deleting and re-importing its file', async ({ page }) => {
  const { accountId, importIds } = await setup(page);
  // ⌘/Ctrl-click two rows and add them to a new group
  await page
    .locator('tr[data-i="1"] td')
    .nth(2)
    .click({ modifiers: ['ControlOrMeta'] });
  await page
    .locator('tr[data-i="3"] td')
    .nth(2)
    .click({ modifiers: ['ControlOrMeta'] });
  await expect(qbar(page)).toContainText('2 selected');
  await qbar(page).getByRole('button', { name: 'New group' }).click();
  await page.getByRole('textbox', { name: 'Group name' }).fill('Wesele');
  await page.keyboard.press('Enter');
  await expect(page.getByText('2 transactions added to “Wesele”')).toBeVisible();

  expect((await page.request.delete(`/api/v1/imports/${importIds[0] ?? ''}`, { headers: H })).status()).toBe(204);
  await importInto(page, accountId, 'demo/demo-legacy.csv');

  await page.goto(`/t/${accountId}`);
  await page.getByRole('button', { name: /Groups/ }).click();
  await page
    .getByRole('dialog', { name: 'Groups' })
    .getByRole('button', { name: /^Wesele/ })
    .click();
  await expect(shown(page)).toHaveText(`2 of ${String(TOTAL)} rows`);
});

test('the group window: totals, span, budget, CSV export and Show in table (PLAN 15.9)', async ({ page }) => {
  test.setTimeout(60_000);
  await setup(page);
  const g = await page.request.post('/api/v1/groups', { headers: H, data: { name: 'Internet', budgetMinor: 100000 } });
  expect(g.status()).toBe(201);
  const gid = ((await g.json()) as { id: string }).id;
  const q = await page.request.post('/api/v1/transactions/query', {
    headers: H,
    data: { context: { filters: [{ column: 'payee', cond: '=OPERATOR INTERNETU' }] }, limit: 50 },
  });
  const ids = ((await q.json()) as { rows: { id: string }[] }).rows.map((r) => r.id);
  expect(ids).toHaveLength(14);
  const b = await page.request.post('/api/v1/transactions/bulk', { headers: H, data: { ids, set: { groupId: gid } } });
  expect(b.status()).toBe(200);
  await page.reload();
  await expect(page.locator('tr[data-i]').first()).toBeVisible();

  await page.getByRole('button', { name: /Groups/ }).click();
  const menu = page.getByRole('dialog', { name: 'Groups' });
  // 14 × 79.99 = 1,119.86 against a 1,000.00 budget
  await expect(menu.getByText('over 120')).toBeVisible();
  await menu.getByRole('button', { name: 'Report for Internet' }).click();
  const win = page.getByRole('dialog', { name: 'Internet' });
  await expect(win.getByRole('group', { name: 'Spent' })).toContainText('1,119.86');
  await expect(win.getByRole('group', { name: 'Span' })).toContainText('398 days');
  await expect(win.getByRole('group', { name: 'Span' })).toContainText('20.01.2024 – 20.02.2025');
  await expect(win.getByRole('region', { name: 'Budget' })).toContainText('over 120');
  await expect(win.locator('rect[data-date]')).toHaveCount(14);
  await expect(
    win.getByRole('list', { name: 'Top payees' }).getByRole('listitem').filter({ hasText: 'OPERATOR INTERNETU' }),
  ).toContainText('14×');

  const [dl] = await Promise.all([
    page.waitForEvent('download'),
    win.getByRole('button', { name: 'Export CSV' }).click(),
  ]);
  const path = await dl.path();
  const fs = await import('node:fs/promises');
  const csv = (await fs.readFile(path, 'utf8')).replace(/^\ufeff/, '');
  const lines = csv.trimEnd().split('\r\n');
  expect(lines).toHaveLength(15);
  expect(lines[0]).toBe('Date;Payee;Description;Amount;Currency;Category;Account;Note');
  expect(dl.suggestedFilename()).toMatch(/^finuance-internet-\d{4}-\d{2}-\d{2}\.csv$/);

  await win.getByRole('button', { name: 'Show in table' }).click();
  await expect(shown(page)).toHaveText(`14 of ${String(TOTAL)} rows`);
});
