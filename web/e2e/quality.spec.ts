// E2E completion (PLAN task 14.2): settings, the keyboard map, a 720 px window, the interface scale
// and every fixture family through the import pipeline.
import { readFileSync } from 'node:fs';

import { expect, test, type Page } from '@playwright/test';

import { API_HEADERS, importViaApi, signUp } from './helpers';

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

test('settings: appearance, profile, sessions, data export', async ({ page }) => {
  await signUp(page);
  await page.goto('/settings/appearance');
  await page.getByRole('button', { name: 'Light', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByRole('button', { name: 'Dark', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  // the size slider: Auto in the middle; one step larger is 10 % above the auto size (1.12 at a
  // device pixel ratio of 1, so 1.23 → 19.68 px)
  const size = page.getByRole('slider', { name: 'Interface size' });
  await expect(size).toHaveAttribute('aria-valuetext', /^Auto · /);
  await size.focus();
  await page.keyboard.press('ArrowRight');
  await expect(size).toHaveAttribute('aria-valuetext', /^Larger · 123/);
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).fontSize)).toBe('19.68px');

  await page.goto('/settings/profile');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Alex');
  await page.getByRole('button', { name: 'Save' }).first().click();
  await expect
    .poll(async () => ((await (await page.request.get('/api/v1/me')).json()) as { displayName?: string }).displayName)
    .toBe('Alex');

  await page.goto('/settings/sessions');
  await expect(page.getByText('this browser')).toBeVisible();

  await page.goto('/settings/data');
  const zip = await page.request.get('/api/v1/me/export');
  expect(zip.status()).toBe(200);
  expect(zip.headers()['content-type']).toBe('application/zip');
  expect((await zip.body()).subarray(0, 2).toString()).toBe('PK');
});

test('keyboard map: search, open, assistant, shortcuts, Esc', async ({ page }) => {
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  const search = page.getByLabel('Search all columns');

  await page.keyboard.press(`${MOD}+f`);
  await expect(search).toBeFocused();
  await page.keyboard.press('Escape');
  await page.locator('tr[data-i]').first().click();
  await page.keyboard.press('/');
  await expect(search).toBeFocused();
  await page.keyboard.press('Escape');
  await page.locator('body').click({ position: { x: 5, y: 600 } });

  const chooser = page.waitForEvent('filechooser');
  await page.keyboard.press(`${MOD}+o`);
  expect((await chooser).isMultiple()).toBe(true);

  const panel = page.getByRole('complementary', { name: 'Assistant' });
  await page.keyboard.press(`${MOD}+j`);
  await expect(panel).toBeVisible();
  await page.keyboard.press(`${MOD}+j`);
  await expect(panel).toBeHidden();

  await page.keyboard.press('?');
  const keys = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(keys.getByText('Show / hide the assistant')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(keys).toBeHidden();
});

test('a 720 px window: the sidebar folds away and popovers stay on screen', async ({ page }) => {
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/t/all');
  await expect(page.locator('tr[data-i]').first()).toBeVisible();
  const sidebar = page.getByRole('complementary', { name: 'Accounts' });
  await page.getByRole('button', { name: 'Toggle sidebar' }).click();
  await expect(sidebar).toBeVisible();

  await page.setViewportSize({ width: 720, height: 800 });
  await expect(sidebar).toBeHidden();
  const inside = (box: { x: number; y: number; width: number; height: number } | null) => {
    expect(box).not.toBeNull();
    if (!box) return;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(720);
    expect(box.y + box.height).toBeLessThanOrEqual(800);
  };
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  inside(await page.locator('.pop').first().boundingBox());
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Columns' }).click();
  inside(await page.locator('.pop').first().boundingBox());
  await page.keyboard.press('Escape');

  // wide again: the sidebar comes back as it was
  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(sidebar).toBeVisible();
});

test('interface size Auto follows the screen density', async ({ browser }) => {
  const size = async (deviceScaleFactor: number) => {
    const ctx = await browser.newContext({ deviceScaleFactor });
    const page = await ctx.newPage();
    await page.goto('/login');
    const px = await page.evaluate(() => getComputedStyle(document.documentElement).fontSize);
    await ctx.close();
    return px;
  };
  // 2× is a Retina screen at 100 % (16 px × 1.12); 2.5× is that screen zoomed to 125 % (× 0.9)
  expect(await size(2)).toBe('17.92px');
  expect(await size(2.5)).toBe('14.4px');
});

async function txCount(page: Page, accountId: string): Promise<number> {
  const all = (await (await page.request.get('/api/v1/accounts', { headers: API_HEADERS })).json()) as {
    id: string;
    txCount: number;
  }[];
  return all.find((a) => a.id === accountId)?.txCount ?? NaN;
}

/** Imports a file into a new account through the API and returns its staged stats. */
async function importFamily(page: Page, rel: string): Promise<{ rows: number; errors: number; transactions: number }> {
  const { accountId, importIds } = await importViaApi(page, rel.split('/').join(' · ').slice(0, 70), rel);
  const imp = (await (
    await page.request.get(`/api/v1/imports/${importIds[0] ?? ''}`, { headers: API_HEADERS })
  ).json()) as {
    stats: { rows: number; errors: number };
  };
  return { ...imp.stats, transactions: await txCount(page, accountId) };
}

// The bank look-alikes and the anonymised real exports; for mBank and PKO BP the legacy picked a
// wrong header row (ALLOWED_DIFFS.md row 4), so only the others are compared with its row count.
const FAMILIES: [string, boolean][] = [
  ['synthetic/banks/pekao-like.csv', true],
  ['synthetic/banks/mbank-like.csv', false],
  ['synthetic/banks/pko-bp-like.csv', false],
  ['real/pekao/pekao_example.csv', true],
  ['real/mbank/mbank_example.csv', false],
  ['real/pko-bp/pko_bp_example.csv', false],
  ['synthetic/same-name/a/export.csv', true],
  ['synthetic/42-bank-a-layout.csv', true],
  ['synthetic/28-debit-credit.csv', true],
];

test('every fixture family goes through the import pipeline', async ({ page }) => {
  await signUp(page);
  for (const [rel, comparable] of FAMILIES) {
    const got = await importFamily(page, rel);
    expect(got.rows, rel).toBeGreaterThan(0);
    expect(got.transactions, rel).toBe(got.rows - got.errors);
    if (comparable) {
      const oracle = JSON.parse(
        readFileSync(`../testdata/oracle/ingest/${rel.replace(/\.csv$/, '.json')}`, 'utf8'),
      ) as {
        rows: unknown[];
      };
      expect(got.rows, `${rel}: rows as the legacy`).toBe(oracle.rows.length);
    } else {
      expect(got.errors, `${rel}: no row errors with the fixed header rule`).toBe(0);
    }
  }
  // overlapping exports and duplicate versions in one account: the oracle's merged counts
  const { accountId } = await importViaApi(
    page,
    'Overlap',
    'synthetic/overlap/A_jan-mar.csv',
    'synthetic/overlap/B_mar-may.csv',
  );
  const merged = JSON.parse(readFileSync('../testdata/oracle/dedupe/overlap-A-B.json', 'utf8')) as {
    default: { rowCount: number };
  };
  expect(await txCount(page, accountId)).toBe(merged.default.rowCount);
});
