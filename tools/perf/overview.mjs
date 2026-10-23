// Overview performance with a 100k-row user (PLAN Phase 12, docs/perf.md "Overview").
//
//   make up && node tools/perf/overview.mjs --email perf-…@example.test [--password …]
//
// Use the user tools/perf/grid.mjs creates (it prints the email; the password of a new user is
// "perf password long enough"). In headless Chromium at 1440×900 it measures:
//   - POST /overview from the page (20 runs after one warm-up): the server through nginx;
//   - table → overview (6 runs, the first discarded): from the click on the title bar's overview
//     button until the balance card is drawn, with when the overview request started;
//   - month step (6 runs): from a click on "Previous month" until the page is idle again, and the
//     part of it spent drawing after the response.
// It uses the web/ workspace's Playwright (run `npm ci` in web/ first). The result is printed and
// written to ./perf-overview.json.

import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const { chromium } = await import(join(root, 'web/node_modules/playwright/index.mjs'));
const { values: args } = parseArgs({
  options: { email: { type: 'string' }, password: { type: 'string' }, base: { type: 'string' } },
});
if (!args.email) {
  console.error('--email is required (a user with ~100k transactions, see tools/perf/grid.mjs)');
  process.exit(2);
}
const BASE = args.base ?? 'http://localhost:8000';

const stats = (f) => {
  const s = [...f].sort((a, b) => a - b);
  const q = (p) => s[Math.min(s.length - 1, Math.floor(p * s.length))];
  return { n: s.length, p50: q(0.5), p95: q(0.95), max: s.at(-1) };
};
const frames = (page) =>
  page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(performance.now())))));
const lastOverview = (page) =>
  page.evaluate(() => {
    const e = performance.getEntriesByType('resource').filter((x) => x.name.includes('/api/v1/overview')).at(-1);
    return e ? { start: e.startTime, end: e.responseEnd } : null;
  });

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, baseURL: BASE });
await page.goto('/login');
await page.getByRole('textbox', { name: 'Email' }).fill(args.email);
await page.getByLabel('Password').fill(args.password ?? 'perf password long enough');
await page.getByRole('button', { name: 'Sign in' }).click();
await page.waitForURL(/\/(t|o)\//);

const api = await page.evaluate(async () => {
  const out = [];
  for (let i = 0; i < 21; i++) {
    const t0 = performance.now();
    const r = await fetch('/api/v1/overview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Finuance-Client': 'web' },
      body: JSON.stringify({ context: {}, months: 12 }),
    });
    await r.json();
    if (i) out.push(performance.now() - t0);
  }
  return out;
});

const toOverview = [];
const requestStart = [];
const step = [];
const draw = [];
for (let i = 0; i < 6; i++) {
  await page.goto('/t/all');
  await page.waitForSelector('tr[data-i]');
  await page.waitForTimeout(2000); // the pane's requests are done, the overview chunk is loaded
  const t0 = await page.evaluate(() => performance.now());
  await page.getByRole('button', { name: 'Personal finance overview' }).click();
  await page.waitForFunction(() => document.querySelector('svg[aria-label="Account balance over time"]'), null, {
    polling: 'raf',
    timeout: 30_000,
  });
  const t1 = await frames(page);
  const req = await lastOverview(page);
  if (i) {
    toOverview.push(t1 - t0);
    if (req) requestStart.push(req.start - t0);
  }
  const s0 = await page.evaluate(() => performance.now());
  await page.getByRole('button', { name: 'Previous month' }).click();
  await page.waitForFunction(() => document.querySelector('section.overview')?.getAttribute('aria-busy') === 'false', null, {
    polling: 'raf',
  });
  const s1 = await frames(page);
  const r2 = await lastOverview(page);
  step.push(s1 - s0);
  if (r2) draw.push(s1 - r2.end);
}
await browser.close();

const result = {
  apiOverview: stats(api),
  tableToOverview: stats(toOverview),
  requestStartAfterClick: stats(requestStart),
  monthStep: stats(step),
  drawAfterResponse: stats(draw),
};
const round = (o) => JSON.parse(JSON.stringify(o, (_, v) => (typeof v === 'number' ? Math.round(v) : v)));
console.log(JSON.stringify(round(result), null, 2));
writeFileSync('perf-overview.json', JSON.stringify(round(result), null, 2));
