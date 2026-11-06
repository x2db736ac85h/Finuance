// The assistant against the mock model server of the E2E stack (PLAN task 13.8, legacy §29.6):
// a tool call streamed in pieces is assembled and run on the server, its result goes back to the
// model, the answer's markdown table renders, Show in table filters the list, auto mode falls back
// to plan mode for a model without tools, Stop ends an answer, and the browser talks to nobody but
// its own origin. The mock (testutil/llmmock) turns a "quoted" word into a search for it.
import { expect, test, type Page } from '@playwright/test';

import { API_HEADERS, importViaApi, signUp } from './helpers';

/** Every request the page makes goes to its own origin; page errors fail the test. */
function watch(page: Page, base: string): { foreign: string[]; errors: string[] } {
  const out = { foreign: [] as string[], errors: [] as string[] };
  page.on('request', (r) => {
    const url = r.url();
    if (!url.startsWith(base) && !url.startsWith('data:') && !url.startsWith('blob:')) out.foreign.push(url);
  });
  page.on('pageerror', (e) => out.errors.push(e.message));
  return out;
}

async function setup(page: Page) {
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  await page.keyboard.press('ControlOrMeta+j');
  const panel = page.getByRole('complementary', { name: 'Assistant' });
  await expect(panel.getByRole('heading', { name: 'Ask about your money' })).toBeVisible();
  await expect(panel.getByText('mock-tools')).toBeVisible();
  return panel;
}

async function ask(page: Page, text: string) {
  const box = page.getByRole('textbox', { name: 'Question' });
  await box.fill(text);
  await box.press('Enter');
}

test('answers with a lookup streamed in pieces and shows it in the table', async ({ page, baseURL }) => {
  const seen = watch(page, baseURL ?? '');
  const panel = await setup(page);
  await ask(page, 'What did I pay at "kino"?');

  // the mock streams search_transactions {"query":"kino","limit":5} in three-character pieces
  await expect(panel.getByText('Searched transactions')).toBeVisible();
  await expect(panel.getByText('query: kino → 10 matches')).toBeVisible();
  // the answer is built from the tool result the server sent back: 10 matches, 5 rows listed
  await expect(panel.getByText(/Found 10 matching transactions/)).toBeVisible();
  const table = panel.locator('.mdt table');
  await expect(table.locator('tbody tr')).toHaveCount(5);
  await expect(table.locator('tbody tr').first()).toContainText('KINO POD GWIAZDAMI');
  await expect(panel.locator('.meta')).toContainText('1 lookup · mock-tools');

  await panel.getByRole('button', { name: 'Show in table' }).click();
  await expect(page.locator('.chip', { hasText: '✦' })).toContainText('query: kino');
  await expect(page.locator('.vt-meta')).toHaveText(/^10 ×/);
  await expect(page.locator('tr[data-i]')).toHaveCount(10);

  // the chat is kept: after a reload it is in the recent chats
  await page.reload();
  await expect(page.getByRole('complementary', { name: 'Assistant' })).toBeVisible();
  await page.locator('.chat-ttl').click();
  await expect(page.locator('.cm-s', { hasText: 'What did I pay at "kino"?' })).toBeVisible();

  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});

test('falls back to plan mode for a model without tools', async ({ page }) => {
  const panel = await setup(page);
  await panel.getByRole('button', { name: 'Assistant settings' }).click();
  await panel
    .locator('label', { hasText: /^Model/ })
    .locator('select')
    .selectOption('mock-no-tools');
  await panel.getByRole('button', { name: 'Done' }).click();
  await ask(page, 'How often did I take a "taxi"?');
  await expect(panel.getByText(/Found 40 matching transactions/)).toBeVisible();
  await expect(panel.locator('.meta')).toContainText('plan mode');
  await expect(panel.getByText('Searched transactions')).toBeVisible();
});

test('stops an answer being written', async ({ page }) => {
  const panel = await setup(page);
  await ask(page, 'Tell me something slow');
  await expect(panel.getByText(/Still thinking/)).toBeVisible();
  await panel.getByRole('button', { name: 'Stop' }).click();
  await expect(panel.getByText('Stopped.')).toBeVisible();
  await expect(panel.locator('.meta')).toContainText('stopped');
  await expect(panel.getByRole('button', { name: 'Send (Enter)' })).toBeVisible();
});

test('says so when no model is configured', async ({ page }) => {
  await page.route('**/api/v1/assistant/status', (route) =>
    route.fulfill({ json: { enabled: false, connections: [], maxRowsLimit: 50 } }),
  );
  await signUp(page);
  await page.keyboard.press('ControlOrMeta+j');
  const panel = page.getByRole('complementary', { name: 'Assistant' });
  await expect(panel.getByRole('heading', { name: 'The assistant isn’t set up' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Question' })).toBeHidden();
});

test('a proposal changes nothing until Apply; Apply categorises the rows, Undo puts them back (PLAN 15.7, ADR 0030)', async ({
  page,
  baseURL,
}) => {
  const seen = watch(page, baseURL ?? '');
  const panel = await setup(page);
  const count = async (cond: string) => {
    const r = await page.request.post('/api/v1/transactions/query', {
      headers: API_HEADERS,
      data: { context: { filters: [{ column: 'category', cond }] }, limit: 1 },
    });
    return ((await r.json()) as { total: number }).total;
  };
  await ask(page, 'put "OPERATOR INTERNETU" in Internet');
  const card = panel.getByRole('group', { name: 'Proposed change' });
  await expect(card).toContainText('Set the category “Internet” for 14 transactions');
  await expect(card).toContainText('The category is new and is created first.');
  await expect(panel.getByText(/Nothing has changed yet/)).toBeVisible();
  expect(await count('=Internet')).toBe(0);

  await card.getByRole('button', { name: 'Apply' }).click();
  await expect(page.getByText('Applied to 14 transactions')).toBeVisible();
  await expect(card).toContainText('✓ Applied');
  await expect.poll(() => count('=Internet')).toBe(14);
  // the table shows the new category without a reload
  await card.getByRole('button', { name: 'Show in table' }).click();
  await expect(page.locator('tr[data-i]')).toHaveCount(14);
  await expect(page.locator('tr[data-i]').first()).toContainText('Internet');
  // the activity log remembers it: after a reload the reopened chat shows the card as applied
  await page.reload();
  const again = page.getByRole('complementary', { name: 'Assistant' });
  await again.getByTitle('Recent chats').click();
  await again.getByTitle('put "OPERATOR INTERNETU" in Internet').click();
  const card2 = again.getByRole('group', { name: 'Proposed change' });
  await expect(card2).toContainText('✓ Applied');
  // only the rows the assistant changed, each marked; Undo puts the bank's categories back (ADR 0030)
  await page.getByRole('button', { name: 'Columns' }).click();
  await page.getByRole('checkbox', { name: 'Only what the assistant changed' }).check();
  await page.keyboard.press('Escape');
  await expect(page.locator('tr[data-i]')).toHaveCount(14);
  await expect(page.getByTitle(/Changed by the assistant/)).toHaveCount(14);
  await card2.getByRole('button', { name: 'Undo' }).click();
  await expect(card2).toContainText('↺ Undone');
  await expect.poll(() => count('=Internet')).toBe(0);
  await expect(page.locator('tr[data-i]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Remove ✦ AI assisted' }).click();
  await page.goto('/settings/activity');
  await expect(page.getByRole('list', { name: 'Assistant changes' })).toContainText('undone');
  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});

test('budget and cash-expense proposals apply and undo (ADR 0030)', async ({ page, baseURL }) => {
  const seen = watch(page, baseURL ?? '');
  // the cash account exists before the question: the proposal names it
  const panel = await setup(page);
  expect(
    (
      await page.request.post('/api/v1/accounts', { headers: API_HEADERS, data: { name: 'Portfel', kind: 'cash' } })
    ).ok(),
  ).toBe(true);
  const budget = async () => {
    const r = await page.request.get('/api/v1/budgets');
    const list = ((await r.json()) as { budgets: { category: { name: string }; amountMinor: number }[] }).budgets;
    return list.find((b) => b.category.name === 'Rachunki')?.amountMinor ?? null;
  };
  const added = async () => {
    const r = await page.request.post('/api/v1/transactions/query', {
      headers: API_HEADERS,
      data: { context: { filters: [{ column: 'source', cond: '=Assistant' }] }, limit: 1 },
    });
    return ((await r.json()) as { total: number }).total;
  };

  await ask(page, 'budget "Rachunki" 800');
  const card = panel.getByRole('group', { name: 'Proposed change' }).last();
  await expect(card).toContainText('Set the monthly budget of “Rachunki”');
  await card.getByRole('button', { name: 'Apply' }).click();
  await expect(card).toContainText('✓ Applied');
  await expect.poll(budget).toBe(80000);
  await card.getByRole('button', { name: 'Undo' }).click();
  await expect(card).toContainText('↺ Undone');
  await expect.poll(budget).toBeNull();

  await ask(page, 'spent 25.50 at "Kawiarnia Ziarno" on 2025-03-10');
  const card2 = panel.getByRole('group', { name: 'Proposed change' }).last();
  await expect(card2).toContainText('paid to “Kawiarnia Ziarno” on 10.03.2025 to Portfel');
  await card2.getByRole('button', { name: 'Apply' }).click();
  await expect(card2).toContainText('✓ Applied');
  await expect.poll(added).toBe(1);
  await card2.getByRole('button', { name: 'Undo' }).click();
  await expect(card2).toContainText('↺ Undone');
  await expect.poll(added).toBe(0);
  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});

test('a goal proposal applies, shows on the Overview, and undoes (ADR 0031)', async ({ page, baseURL }) => {
  const seen = watch(page, baseURL ?? '');
  const panel = await setup(page);
  const goals = async () => {
    const r = await page.request.get('/api/v1/goals');
    return ((await r.json()) as { goals: { name: string; ai: boolean }[] }).goals;
  };

  await ask(page, 'spend 20% less on "Artykuły spożywcze" by 2030-04-30');
  const card = panel.getByRole('group', { name: 'Proposed change' }).last();
  await expect(card).toContainText('New goal “Less on Artykuły spożywcze”: spend 20 % less on Artykuły spożywcze');
  expect(await goals()).toEqual([]); // nothing changes before Apply
  await card.getByRole('button', { name: 'Apply' }).click();
  await expect(card).toContainText('✓ Applied');
  await expect
    .poll(async () => (await goals()).map((g) => [g.name, g.ai]))
    .toEqual([['Less on Artykuły spożywcze', true]]);

  // the Overview's Goals card shows it, marked as the assistant's
  await page.getByRole('button', { name: 'Personal finance overview' }).click(); // in the app: the panel stays open
  const goalsCard = page.getByRole('region', { name: 'Goals' });
  await expect(goalsCard).toContainText('Less on Artykuły spożywcze');
  await expect(goalsCard.getByTitle(/Set by the assistant/)).toBeVisible();

  await card.getByRole('button', { name: 'Undo' }).click();
  await expect(card).toContainText('↺ Undone');
  await expect(goalsCard).toContainText('No goals yet');
  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});

test('a receipt photo goes with its question; attach_receipt writes the note and undoes (ADR 0032)', async ({
  page,
  baseURL,
}) => {
  const seen = watch(page, baseURL ?? '');
  const panel = await setup(page);
  // the mock's model that reads images
  await panel.getByRole('button', { name: 'Assistant settings' }).click();
  await panel
    .locator('label', { hasText: /^Model/ })
    .locator('select')
    .selectOption('mock-vl');
  await panel.getByRole('button', { name: 'Done' }).click();
  const withReceipt = async () => {
    const r = await page.request.post('/api/v1/transactions/query', {
      headers: API_HEADERS,
      // the search box covers the bank's text, not notes: filter the note column
      data: { context: { filters: [{ column: 'note', cond: 'Piekarnia Ziarno' }] }, limit: 1 },
    });
    return ((await r.json()) as { total: number }).total;
  };

  await panel
    .getByLabel('Photo of a receipt', { exact: true })
    .setInputFiles('../evals/assistant/receipts/pl-sklep.jpg');
  await expect(panel.getByAltText('Photo to send')).toBeVisible();
  await ask(page, 'receipt 98.07 "Piekarnia Ziarno" on 2025-03-17');
  await expect(panel.getByAltText('Attached photo')).toBeVisible();
  const card = panel.getByRole('group', { name: 'Proposed change' }).last();
  await expect(card).toContainText('Add the receipt to “PIEKARNIA ZIARNO WARSZAWA” (17.03.2025');
  await expect(card).toContainText('Piekarnia Ziarno · bread, butter');
  expect(await withReceipt()).toBe(0);
  await card.getByRole('button', { name: 'Apply' }).click();
  await expect(card).toContainText('✓ Applied');
  await expect.poll(withReceipt).toBe(1);
  await card.getByRole('button', { name: 'Undo' }).click();
  await expect(card).toContainText('↺ Undone');
  await expect.poll(withReceipt).toBe(0);
  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});

test('a digest asked for now is written by the worker and opens from the bell (ADR 0033)', async ({ page }) => {
  const panel = await setup(page);
  await panel.getByTitle('Assistant settings').click();
  await panel.getByLabel('How often').selectOption('daily');
  await expect(panel.getByText(/Next one .*, with mock-tools\./)).toBeVisible();
  await panel.getByRole('button', { name: 'Write one now' }).click();
  await expect(page.getByText(/A digest is being written/)).toBeVisible();
  await panel.getByRole('button', { name: 'Done' }).click();

  // the worker answers the digest's question with the mock (spending by category); the bell asks
  // every minute and when the page loads
  const bell = page.getByRole('button', { name: 'Notifications, 1 unread' });
  await expect(async () => {
    await page.reload();
    await expect(bell).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  await bell.click();
  await page.getByText('Your daily digest is ready').click();
  const open = page.getByRole('complementary', { name: 'Assistant' });
  await expect(open.getByText(/^Write my daily digest for today/)).toBeVisible();
  await expect(open.locator('.mdt table')).toBeVisible();
  await expect(open.locator('.chat-ttl')).toContainText('Daily digest ·');
});
