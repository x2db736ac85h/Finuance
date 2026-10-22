// Grid performance with a 100k-row user (PLAN task 10.14, docs/perf.md "Table view").
//
//   make up && node tools/perf/grid.mjs [--email existing@example.test --password …] [--trace out.zip]
//
// Without --email it signs up a new user and imports ~100k transactions: the legacy demo file
// copied 110 times, each copy one month earlier with its own reference numbers (so nothing is
// de-duplicated), in 10 files. Then it measures, in headless Chromium at 1440×900:
//   - wheel scrolling (120 steps of 500 px) at the top and in the middle: frame intervals from
//     requestAnimationFrame, and Chrome's script / layout / style time;
//   - jumps to the bottom, the middle and the top: time until no skeleton row is left;
//   - filter-to-paint for five filter shapes (6 runs, the first discarded);
//   - the same wheel steps over a static table without script (the machine's floor).
// It uses the web/ workspace's Playwright (run `npm ci` in web/ first). The password of a new
// user is "perf password long enough".

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const { chromium } = await import(join(root, 'web/node_modules/playwright/index.mjs'));
const { values: args } = parseArgs({ options: { email: { type: 'string' }, password: { type: 'string' }, trace: { type: 'string' }, base: { type: 'string' } } });
const BASE = args.base ?? 'http://localhost:8000';
const PASSWORD = args.password ?? 'perf password long enough';
const H = { 'X-Finuance-Client': 'web' };

function files() {
  const lines = readFileSync(join(root, 'testdata/fixtures/demo/demo-legacy.csv'), 'utf8').split('\n');
  const header = lines[0];
  const rows = lines.slice(1).filter((l) => l.trim());
  const cols = header.split(';');
  const ref = cols.indexOf('Numer referencyjny');
  const dates = [cols.indexOf('Data księgowania'), cols.indexOf('Data waluty')];
  const shift = (d, months) => {
    const [dd, mm, yy] = d.split('.').map(Number);
    const m = yy * 12 + (mm - 1) - months;
    return `${String(Math.min(dd, 28)).padStart(2, '0')}.${String((m % 12) + 1).padStart(2, '0')}.${String(Math.floor(m / 12))}`;
  };
  const out = [];
  for (let f = 0; f < 10; f++) {
    const body = [header];
    for (let c = 0; c < 11; c++) {
      const k = f * 11 + c;
      for (const r of rows) {
        const p = r.split(';');
        for (const i of dates) if (p[i]) p[i] = shift(p[i], k);
        p[ref] += `X${String(k).padStart(3, '0')}`;
        body.push(p.join(';'));
      }
    }
    out.push({ name: `perf-${String(f).padStart(2, '0')}.csv`, buffer: Buffer.from(body.join('\n') + '\n') });
  }
  return out;
}

const stats = (f) => {
  const s = [...f].sort((a, b) => a - b);
  const q = (x) => s[Math.min(s.length - 1, Math.floor(x * s.length))] ?? 0;
  return { frames: f.length, p50: +q(0.5).toFixed(1), p95: +q(0.95).toFixed(1), max: +(s.at(-1) ?? 0).toFixed(1), overBudget: f.filter((x) => x > 17.5).length };
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, baseURL: BASE });
const p = await ctx.newPage();
const result = {};

if (args.email) {
  await p.goto('/login');
  await p.getByRole('textbox', { name: 'Email' }).fill(args.email);
  await p.getByLabel('Password').fill(PASSWORD);
  await p.getByRole('button', { name: 'Sign in' }).click();
  await p.waitForURL((u) => !u.pathname.startsWith('/login'), { waitUntil: 'commit' });
} else {
  const email = `perf-${String(Date.now())}@example.test`;
  await p.goto('/signup');
  await p.getByRole('textbox', { name: 'Email' }).fill(email);
  await p.getByLabel('Password').fill(PASSWORD);
  await p.getByRole('button', { name: 'Create account' }).click();
  await p.getByRole('heading', { name: 'Drop CSV files here' }).waitFor();
  const acc = await (await p.request.post('/api/v1/accounts', { headers: H, data: { name: 'Perf' } })).json();
  const t0 = Date.now();
  for (const file of files()) {
    const up = await p.request.post('/api/v1/imports', { headers: H, timeout: 120_000, multipart: { accountId: acc.id, file: { ...file, mimeType: 'text/csv' } } });
    if (up.status() !== 201) throw new Error(`${file.name}: ${String(up.status())} ${await up.text()}`);
    const id = (await up.json()).import.id;
    await p.request.post(`/api/v1/imports/${id}/commit`, { headers: H, data: {} });
    for (;;) {
      const s = (await (await p.request.get(`/api/v1/imports/${id}`)).json()).status;
      if (s === 'committed') break;
      if (s === 'failed') throw new Error(`${file.name} failed`);
      await p.waitForTimeout(500);
    }
  }
  result.user = email;
  result.importSeconds = Math.round((Date.now() - t0) / 1000);
  console.log(`imported as ${email} in ${String(result.importSeconds)} s`);
}

if (args.trace) await ctx.tracing.start({ screenshots: true, snapshots: false });
let t0 = Date.now();
await p.goto('/t/all');
await p.locator('tr[data-i]').first().waitFor();
result.firstPaintMs = Date.now() - t0;
result.rows = await p.locator('.vt-meta').innerText();

const cdp = await ctx.newCDPSession(p);
await cdp.send('Performance.enable');
const metrics = async () => Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map((m) => [m.name, m.value]));
await p.evaluate(() => {
  window.__frames = [];
  let last = performance.now();
  const tick = (t) => {
    window.__frames.push(t - last);
    last = t;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
});
const frames = () => p.evaluate(() => window.__frames.splice(0));
const wrap = p.locator('.grid-wrap');
const box = await wrap.boundingBox();
await p.mouse.move(box.x + 400, box.y + 300);
const scrollTo = (frac) => wrap.evaluate((el, f) => { el.scrollTop = (el.scrollHeight - el.clientHeight) * f; }, frac);
const loaded = () => p.waitForFunction(() => !document.querySelector('tr.skel') && document.querySelector('tr[data-i]'), null, { polling: 'raf', timeout: 20_000 });

await frames();
await p.waitForTimeout(1000);
result.idle = stats(await frames());
for (const [name, at] of [['wheelTop', 0], ['wheelMiddle', 0.5]]) {
  await scrollTo(at);
  await loaded();
  await p.waitForTimeout(1000);
  await frames();
  const m0 = await metrics();
  for (let i = 0; i < 120; i++) {
    await p.mouse.wheel(0, 500);
    await p.waitForTimeout(16);
  }
  await p.waitForTimeout(300);
  const m1 = await metrics();
  const d = (k) => Math.round((m1[k] - m0[k]) * 1000);
  result[name] = { ...stats(await frames()), scriptMs: d('ScriptDuration'), layoutMs: d('LayoutDuration'), styleMs: d('RecalcStyleDuration'), mainThreadMs: d('TaskDuration') };
}
for (const [name, at] of [['jumpBottomMs', 1], ['jumpMiddleMs', 0.5], ['jumpTopMs', 0]]) {
  t0 = Date.now();
  await scrollTo(at);
  await loaded();
  result[name] = Date.now() - t0;
}

const status = p.locator('footer.statusbar .st').first();
const settle = async (before) => {
  await p.waitForFunction((b) => {
    const t = document.querySelector('footer.statusbar .st')?.textContent;
    return t && t !== b && !document.querySelector('tr.skel');
  }, before, { polling: 'raf', timeout: 20_000 });
  await p.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
};
const condition = async (col, c) => {
  await p.getByRole('button', { name: `Filter and sort ${col}` }).click({ force: true });
  const input = p.getByRole('textbox', { name: `Condition for ${col}` });
  await input.fill(c);
  return () => input.press('Enter');
};
const removeChip = (col) => p.getByRole('button', { name: new RegExp(`^Remove ${col} `) }).click();
const search = p.getByRole('textbox', { name: 'Search all columns' });
const cases = [
  ['search "blik warszawa"', async () => { await search.fill('blik warszawa'); }, () => search.fill('')],
  ['payee contains "kawiarnia"', () => condition('Payee', 'kawiarnia'), () => removeChip('Payee')],
  ['amount < -100', () => condition('Amount', '<-100'), () => removeChip('Amount')],
  ['date 2024', () => condition('Date', '2024'), () => removeChip('Date')],
  ['category =Transport', () => condition('Category', '=Transport'), () => removeChip('Category')],
];
result.filterToPaintMs = {};
for (const [name, apply, undo] of cases) {
  const ms = [];
  for (let i = 0; i < 6; i++) {
    let before = await status.textContent();
    const go = await apply();
    t0 = Date.now();
    if (go) await go();
    await settle(before);
    ms.push(Date.now() - t0);
    before = await status.textContent();
    await undo();
    await settle(before);
  }
  ms.shift();
  result.filterToPaintMs[name] = ms;
}
if (args.trace) await ctx.tracing.stop({ path: args.trace });

// Baseline: the same wheel steps over a static 5,000-row table without any script, in the same
// browser. Headless Chromium paints in software, so this is the floor for this machine.
const plain = await ctx.newPage();
const cells = '<td>21.10.2026</td><td>MARKET NA ROGU WARSZAWA</td><td>DEMO 82471841</td><td>-96.28</td><td>Artykuły spożywcze</td>';
await plain.setContent(
  '<style>body{margin:0;font:13px sans-serif}div{height:850px;overflow:auto}table{border-collapse:collapse;table-layout:fixed;width:1260px}td{padding:6px 10px;border-bottom:1px solid #ddd;white-space:nowrap;overflow:hidden}</style>' +
    `<div><table>${Array.from({ length: 5000 }, (_, i) => `<tr><td>${String(i + 1)}</td>${cells}</tr>`).join('')}</table></div>`,
);
await plain.evaluate(() => {
  window.__frames = [];
  let last = performance.now();
  const tick = (t) => {
    window.__frames.push(t - last);
    last = t;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
});
await plain.mouse.move(400, 300);
await plain.waitForTimeout(500);
await plain.evaluate(() => window.__frames.splice(0));
for (let i = 0; i < 120; i++) {
  await plain.mouse.wheel(0, 500);
  await plain.waitForTimeout(16);
}
await plain.waitForTimeout(300);
result.staticTableBaseline = stats(await plain.evaluate(() => window.__frames.splice(0)));
await browser.close();
console.log(JSON.stringify(result, null, 2));
writeFileSync('perf-grid.json', JSON.stringify(result, null, 2));
