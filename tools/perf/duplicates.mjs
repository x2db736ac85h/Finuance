// The duplicates window's first render with many groups (PLAN 25.17, docs/perf.md "Phase 25"): an
// animal whose overlapping statements left many duplicates, on the lab.
//
//   PERF_PASSWORD="$(make -s sim-password ANIMAL=squirrel | cut -f2)" \
//     node tools/perf/duplicates.mjs --email squirrel@sim.invalid --base http://localhost:8020
//
// The password comes from the environment, never the command line. In headless Chromium at
// 1440×900 it measures:
//   - GET /duplicates from the page (20 runs after one warm-up): the server through nginx;
//   - the window's first render (6 runs, the first discarded): from the click on the status bar's
//     "duplicates skipped" until the first group is drawn, and how many groups the window holds.
// It uses the web/ workspace's Playwright (run `npm ci` in web/ first). The result is printed and
// written to ./perf-duplicates.json.

import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const { chromium } = await import(join(root, 'web/node_modules/playwright/index.mjs'));
const { values: args } = parseArgs({ options: { email: { type: 'string' }, base: { type: 'string' } } });
const password = process.env.PERF_PASSWORD;
if (!args.email || !password) {
  console.error('--email and PERF_PASSWORD are required (an animal with many duplicates, e.g. the squirrel)');
  process.exit(2);
}
const BASE = args.base ?? 'http://localhost:8020';

const stats = (f) => {
  const s = [...f].sort((a, b) => a - b);
  const q = (p) => s[Math.min(s.length - 1, Math.floor(p * s.length))];
  return { n: s.length, p50: q(0.5), p95: q(0.95), max: s.at(-1) };
};
const frames = (page) =>
  page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(performance.now())))));

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, baseURL: BASE });
await page.goto('/login');
await page.getByRole('textbox', { name: 'Email' }).fill(args.email);
await page.getByLabel('Password').fill(password);
await page.getByRole('button', { name: 'Sign in' }).click();
await page.waitForURL(/\/(t|o)\//);

const api = await page.evaluate(async () => {
  const out = [];
  for (let i = 0; i < 21; i++) {
    const t0 = performance.now();
    const r = await fetch('/api/v1/duplicates', { headers: { 'X-Finuance-Client': 'web' } });
    await r.json();
    if (i) out.push(performance.now() - t0);
  }
  return out;
});

const open = [];
let groups = 0;
let skipped = '';
for (let i = 0; i < 6; i++) {
  await page.goto('/transactions/all');
  await page.waitForSelector('tr[data-i]');
  const link = page.locator('footer.statusbar').getByRole('button', { name: /duplicates? skipped/ });
  await link.waitFor({ timeout: 30_000 });
  skipped = (await link.textContent()) ?? '';
  await page.waitForTimeout(1000);
  const t0 = await page.evaluate(() => performance.now());
  await link.click();
  const dlg = page.getByRole('dialog', { name: 'Duplicates' });
  await dlg.getByRole('region').first().waitFor({ timeout: 60_000 });
  const t1 = await frames(page);
  if (i) open.push(t1 - t0);
  groups = await dlg.getByRole('region').count();
  await page.keyboard.press('Escape');
}
await browser.close();

const round = (o) => JSON.parse(JSON.stringify(o, (_, v) => (typeof v === 'number' ? Math.round(v) : v)));
const result = round({ skipped, groupsDrawn: groups, apiDuplicates: stats(api), firstRender: stats(open) });
console.log(JSON.stringify(result, null, 2));
writeFileSync('perf-duplicates.json', JSON.stringify(result, null, 2));
