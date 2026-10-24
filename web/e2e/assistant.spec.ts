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

test('a proposal changes nothing until Apply; Apply categorises the rows (PLAN 15.7)', async ({ page, baseURL }) => {
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
  expect(seen.foreign).toEqual([]);
  expect(seen.errors).toEqual([]);
});
