// The simulator's tuning page (PLAN 25.6) against a running lab: `make sim-up`, then
// `make test-e2e-sim` (E2E_SIM=1, the simulator's address and its control token). The page and
// its control API are the simulator's; the personas' requests go to the lab behind it.
import { expect, test, type Page } from '@playwright/test';

import { check } from '../axe';

const TOKEN = process.env['E2E_SIM_TOKEN'] ?? '';
const HEADERS = { Authorization: `Bearer ${TOKEN}`, 'X-Finuance-Client': 'web' };

async function open(page: Page) {
  await page.goto(`/#token=${TOKEN}`);
  await expect(page.getByRole('heading', { name: 'Simulator' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Personas' }).getByRole('article').first()).toBeVisible();
}

test.beforeEach(() => {
  test.skip(!TOKEN, 'needs E2E_SIM_TOKEN (the lab: make sim-up)');
});

test('a wrong token is refused, and the right one is kept for the tab', async ({ page }) => {
  await page.goto('/#token=not-the-control-token-of-this-lab-at-all');
  await expect(page.getByRole('alert')).toContainText('refused');
  expect(page.url()).not.toContain('token'); // the fragment is taken off the address
  await page.getByLabel('Control token').fill(TOKEN);
  await page.getByRole('button', { name: 'Open' }).click();
  await expect(page.getByRole('region', { name: 'Personas' }).getByRole('article').first()).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Simulator' })).toBeVisible();
});

test('a weight changes, and goes back with the defaults', async ({ page }) => {
  await open(page);
  const weight = page.getByLabel('Weight of insights.overview');
  await weight.fill('7');
  await expect
    .poll(async () => {
      const r = await page.request.get('/api/config', { headers: HEADERS });
      return ((await r.json()) as { config: { weights?: Record<string, number> } }).config.weights?.[
        'insights.overview'
      ];
    })
    .toBe(7);
  await page.getByRole('button', { name: 'Back to the defaults' }).click();
  await expect(weight).toHaveValue('');
});

test('pause and resume', async ({ page }) => {
  await open(page);
  await page.getByRole('button', { name: 'Pause' }).click();
  await expect(page.getByText('Paused', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Resume' }).click();
  await expect(page.getByText('Running', { exact: true })).toBeVisible();
});

test('Fox asks now, and the feed shows the request id', async ({ page }) => {
  test.setTimeout(90_000);
  await open(page);
  const act = page.getByRole('region', { name: 'Act now' });
  await act.getByRole('combobox', { name: 'Persona' }).selectOption('fox');
  await act.getByRole('combobox', { name: 'Action' }).selectOption('assistant.ask');
  await act.getByRole('button', { name: 'Act now' }).click();
  const result = page.getByLabel('Result');
  await expect(result).toContainText('fox assistant.ask', { timeout: 60_000 });
  const id = (await result.locator('code').textContent()) ?? '';
  expect(id).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  await expect(page.getByRole('region', { name: 'Feed' }).getByText(id).first()).toBeVisible();
  await expect(result.getByRole('link', { name: 'Logs and trace' })).toHaveAttribute('href', /explore/);
});

test('the page is accessible in both modes', async ({ page }) => {
  await open(page);
  await check(page, 'simulator');
});

test('load mode: a target, shown in the header, and back to realistic mode', async ({ page }) => {
  await open(page);
  const load = page.getByRole('radio', { name: /^Load: one target/ });
  await load.click(); // checked once the simulator has the new configuration
  await expect(load).toBeChecked();
  await page.getByRole('spinbutton', { name: /^Actions a second/ }).fill('5');
  await expect(page.getByLabel('Load now')).toContainText('a second', { timeout: 10_000 });
  await expect(page.getByLabel('Load now')).toContainText('until');
  await check(page, 'simulator in load mode');
  await page.getByRole('button', { name: 'Back to the defaults' }).click();
  await expect(page.getByLabel('Intensity')).toBeVisible();
});

test('history: a profile with its estimate, and a rebuild asked for', async ({ page }) => {
  await open(page);
  const history = page.getByRole('region', { name: 'History' });
  await expect(history).toContainText('transactions per animal');
  await history.getByRole('spinbutton', { name: /^Years back/ }).fill('1');
  await history.getByRole('button', { name: 'Rebuild history' }).click();
  await expect(history.getByRole('status')).toContainText('still to rebuild', { timeout: 10_000 });
  await check(page, 'simulator history');
});
