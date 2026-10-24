// Rules, categories and transfers end to end (PLAN Phase 15 tests): a rule made from the table's
// filters is applied by the worker and shows in the table and the overview; a transfer between two
// accounts is badged, hidden on request, left out of the charts, and unpaired from the inspector.
import { expect, test, type Page } from '@playwright/test';

import { API_HEADERS, importViaApi, signUp } from './helpers';

const shown = (page: Page) => page.locator('footer.statusbar .st').first();

async function uploadCsv(page: Page, accountId: string, name: string, csv: string): Promise<void> {
  const up = await page.request.post('/api/v1/imports', {
    headers: API_HEADERS,
    multipart: { accountId, file: { name, mimeType: 'text/csv', buffer: Buffer.from(csv) } },
  });
  expect(up.status()).toBe(201);
  const id = ((await up.json()) as { import: { id: string } }).import.id;
  expect((await page.request.post(`/api/v1/imports/${id}/commit`, { headers: API_HEADERS, data: {} })).status()).toBe(
    202,
  );
  await expect
    .poll(async () => ((await (await page.request.get(`/api/v1/imports/${id}`)).json()) as { status: string }).status)
    .toBe('committed');
}

async function total(page: Page, context: object): Promise<number> {
  const r = await page.request.post('/api/v1/transactions/query', {
    headers: API_HEADERS,
    data: { context, limit: 1 },
  });
  return ((await r.json()) as { total: number }).total;
}

test('a rule saved from the table filters categorises the table and the overview', async ({ page }) => {
  test.setTimeout(90_000);
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  const cat = await page.request.post('/api/v1/categories', { headers: API_HEADERS, data: { name: 'Internet' } });
  expect(cat.status()).toBe(201);

  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.getByRole('textbox', { name: 'Search all columns' }).fill('operator internetu');
  await expect(shown(page)).toHaveText('14 of 917 rows');
  await page.getByRole('button', { name: 'Save filters as rule…' }).click();
  const dlg = page.getByRole('dialog', { name: 'New rule from filters' });
  await expect(dlg.getByText('Matches 14 transactions')).toBeVisible();
  await dlg.getByRole('combobox', { name: 'Category to set' }).selectOption({ label: 'Internet' });
  await dlg.getByRole('button', { name: 'Create rule' }).click();
  await expect(page.getByText('Rule “operator internetu” created · applying…')).toBeVisible();

  // the worker applies it; the table refetches on its own
  await expect
    .poll(() => total(page, { filters: [{ column: 'category', cond: '=Internet' }] }), { timeout: 20_000 })
    .toBe(14);
  await expect(page.locator('tr[data-i]').first()).toContainText('Internet', { timeout: 10_000 });
  await page.locator('tr[data-i]').first().click();
  await expect(page.getByRole('complementary', { name: 'Transaction details' })).toContainText(
    'by the rule “operator internetu”',
  );

  // the overview's categories follow
  await page.goto('/o/all');
  await expect(page.getByText('Recurring payments').first()).toBeVisible();
  await expect(page.getByText('Internet').first()).toBeVisible();

  // Settings → Rules lists it; deleting it gives the bank's categories back
  await page.goto('/settings/rules');
  const list = page.getByRole('list', { name: 'Rules' });
  await expect(list.getByText('search “operator internetu” → Internet')).toBeVisible();
  await list.getByRole('button', { name: 'Delete operator internetu' }).click();
  await page.getByRole('dialog', { name: 'Delete rule' }).getByRole('button', { name: 'Delete rule' }).click();
  await expect
    .poll(() => total(page, { filters: [{ column: 'category', cond: '=Internet' }] }), { timeout: 20_000 })
    .toBe(0);
});

test('a transfer between two accounts is badged, hidden on request, left out of the charts and unpaired', async ({
  page,
}) => {
  test.setTimeout(90_000);
  await signUp(page);
  const head = 'Data operacji;Kontrahent;Tytuł;Kwota\n';
  const a = await page.request.post('/api/v1/accounts', { headers: API_HEADERS, data: { name: 'Konto' } });
  const b = await page.request.post('/api/v1/accounts', { headers: API_HEADERS, data: { name: 'Oszczędności' } });
  const [ida, idb] = [((await a.json()) as { id: string }).id, ((await b.json()) as { id: string }).id];
  await uploadCsv(
    page,
    ida,
    'konto.csv',
    `${head}02.01.2025;PRZELEW WLASNY;Na oszczędności;-500,00\n03.01.2025;ZABKA;Zakupy;-30,00\n`,
  );
  await uploadCsv(page, idb, 'oszczednosci.csv', `${head}03.01.2025;PRZELEW WLASNY;Z konta;500,00\n`);
  await expect.poll(() => total(page, {}), { timeout: 20_000 }).toBe(1); // the pair is left out by default

  await page.goto('/t/all');
  await expect(shown(page)).toHaveText('3 of 3 rows');
  await expect(page.locator('tr[data-i] .tag.xfer')).toHaveCount(2);
  // the summary card counts only the purchase
  await expect(page.getByRole('region', { name: 'Summary' }).getByText('1 operation')).toBeVisible();

  await page.getByRole('button', { name: 'Columns' }).click();
  await page.getByRole('checkbox', { name: 'Show transfers between my accounts' }).uncheck();
  await page.keyboard.press('Escape');
  await expect(shown(page)).toHaveText('1 of 3 rows');
  await page.getByRole('button', { name: 'Remove transfers hidden' }).click();
  await expect(shown(page)).toHaveText('3 of 3 rows');

  await page.locator('tr[data-i]', { hasText: '-500' }).click();
  const xfer = page.getByRole('group', { name: 'Transfer' });
  await expect(xfer).toContainText('paired with Oszczędności, 03.01.2025');
  await xfer.getByRole('button', { name: 'Not a transfer' }).click();
  await expect(xfer).toContainText('Not a transfer · decided by you');
  await expect(page.locator('tr[data-i] .tag.xfer')).toHaveCount(0);
  await expect.poll(() => total(page, {})).toBe(3);
});

test('three manual categorisations suggest a rule; creating it categorises the whole payee group (PLAN 15.10)', async ({
  page,
}) => {
  test.setTimeout(90_000);
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  const cat = await page.request.post('/api/v1/categories', { headers: API_HEADERS, data: { name: 'Internet' } });
  const catId = ((await cat.json()) as { id: string }).id;
  const q = await page.request.post('/api/v1/transactions/query', {
    headers: API_HEADERS,
    data: { context: { filters: [{ column: 'payee', cond: '=OPERATOR INTERNETU' }] }, limit: 3 },
  });
  const ids = ((await q.json()) as { rows: { id: string }[] }).rows.map((r) => r.id);
  const b = await page.request.post('/api/v1/transactions/bulk', {
    headers: API_HEADERS,
    data: { ids, category: { id: catId } },
  });
  expect(b.status()).toBe(200);

  await page.goto('/t/all');
  const card = page.getByRole('region', { name: 'Rule suggestion' });
  await expect(card).toContainText('You put 3 transactions of OPERATOR INTERNETU in Internet by hand.');
  await card.getByRole('button', { name: 'Create rule…' }).click();
  const dlg = page.getByRole('dialog', { name: 'New rule' });
  await expect(dlg.getByText('Matches 14 transactions')).toBeVisible();
  await dlg.getByRole('button', { name: 'Create rule' }).click();
  await expect(card).toHaveCount(0);
  await expect
    .poll(() => total(page, { filters: [{ column: 'category', cond: '=Internet' }] }), { timeout: 20_000 })
    .toBe(14);
  // the dismissal is kept in the preferences: a reload doesn't bring the card back
  await page.reload();
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await expect(card).toHaveCount(0);
});
