// The parity harness (PLAN task 14.3). The legacy single-file app runs in Chromium next to the new
// app; both get the same data, and their numbers are compared in minor units:
//
//   1. directly: the legacy demo CSV loaded into the legacy page, and imported into a fresh user;
//   2. through the migration: the oracle's demo backup (demo + two duplicate versions, groups,
//      budgets, overrides, opening balance, investments) restored in the legacy page, and migrated
//      into another fresh user. This adds group totals, budgets, the duplicates count and the
//      investment inputs.
//
// The legacy numbers are computed live from the legacy internals (window.__legacy, injected as the
// oracle does). Every comparison goes into docs/parity-report.md; the test fails on a difference
// that ALLOWED_DIFFS.md doesn't list.
import { readFileSync, writeFileSync } from 'node:fs';

import { expect, test, type Browser, type Page } from '@playwright/test';

import type { Schemas } from '../src/api/client';
import { buildManifest, openBackup, parseBackup, type Target } from '../src/legacy/backup';
import { API_HEADERS, fixture, importViaApi, signUp } from './helpers';

test.describe.configure({ mode: 'serial' });

const LEGACY_URL = 'http://legacy.test/';
const EXPOSED = [
  'S',
  'parseEntry',
  'getDataset',
  'setActive',
  'view',
  'compute',
  'findRecurring',
  'ovCtx',
  'groupStats',
  'applyGroupsAll',
];

function legacyHtml(): string {
  const src = readFileSync('../legacy/finuance.html', 'utf8');
  const anchor = /^updateToggles\(\);$/gm;
  if ((src.match(anchor) ?? []).length !== 1)
    throw new Error('legacy anchor "updateToggles();" not found exactly once');
  return src.replace(anchor, `window.__legacy = {${EXPOSED.join(', ')}};\nupdateToggles();`);
}

type LegacyFile = { id: string; name: string; added: number; b64: string; cfg: unknown };
type Private = Record<string, unknown>;

/** The numbers the harness compares, in minor units. */
type Facts = {
  sum: number;
  summary: Record<string, { spent: number; income: number; count: number }>;
  categories: Record<string, number>;
  cashflow: Record<string, { income: number; expense: number; count: number }>;
  recurring: { payee: string; median: number; count: number; last: string; next: string }[];
  months: Record<string, { income: number; expense: number; count: number }>;
  balance: number;
  groups?: Record<string, { count: number; spent: number }>;
  duplicates?: number;
  budgets?: Record<string, number>;
  investments?: { ppkValue: number; positions: { name: string; invested: number; value: number }[] };
};

const PERIODS = ['all', 'l3m', 'm:2025-03', 'm:2025-02', 'm:2025-01'];
const MONTHS = ['2025-03', '2025-02', '2025-01'];

/** Opens the legacy app, loads the files (and the private data of a backup) and computes its facts. */
async function legacyFacts(browser: Browser, files: LegacyFile[], priv: Private | null): Promise<Facts> {
  const ctx = await browser.newContext({ bypassCSP: true, locale: 'en-US', timezoneId: 'UTC' });
  const page = await ctx.newPage();
  const html = legacyHtml();
  await page.route(LEGACY_URL, (r) => r.fulfill({ contentType: 'text/html; charset=utf-8', body: html }));
  await page.goto(LEGACY_URL);
  await page.waitForFunction(() => (window as unknown as { __legacy?: { S: { booted: boolean } } }).__legacy?.S.booted);
  const facts = await page.evaluate(computeLegacy, { files, priv, periods: PERIODS, months: MONTHS });
  await ctx.close();
  return facts;
}

/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment, @typescript-eslint/no-unsafe-member-access, @typescript-eslint/no-unsafe-call, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-return, @typescript-eslint/restrict-plus-operands -- runs in the legacy page, over its untyped internals */
function computeLegacy(a: { files: LegacyFile[]; priv: Private | null; periods: string[]; months: string[] }): Facts {
  const L = (window as any).__legacy;
  const S = L.S;
  const bytes = (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
  S.files = a.files.map((f) => {
    const e: any = { id: f.id, name: f.name, size: 0, buf: bytes(f.b64).buffer, hash: f.id, added: f.added };
    if (f.cfg) e.cfg = f.cfg;
    L.parseEntry(e);
    return e;
  });
  const p: any = a.priv ?? {};
  Object.assign(S, {
    cache: {},
    views: {},
    dupPick: p.dupPick ?? {},
    dupKeep: p.dupKeep ?? {},
    groups: p.groups ?? [],
    tags: p.tags ?? {},
    budgets: p.budgets ?? {},
    opening: Number(p.opening ?? 0),
  });
  const id = S.files.length > 1 ? 'merged' : S.files[0].id;
  L.setActive(id);
  L.applyGroupsAll();
  const ds = L.getDataset(id);
  const col = (n: string) => ds.cols.find((c: any) => c.name === n);
  const dc = col(ds.roles.date);
  const ac = col(ds.roles.amount);
  const cc = col(ds.roles.category);
  const pc = col(ds.roles.payee);
  const m = (x: number) => Math.round(x * 100);
  const iso = (t: number) => new Date(t).toISOString().slice(0, 10);
  const view = (period: string) => {
    const v = L.view();
    Object.assign(v, { period, custom: null, search: '', filters: {} });
    L.compute();
    return [...S.order] as number[];
  };
  const all = view('all');
  const out: Facts = { sum: 0, summary: {}, categories: {}, cashflow: {}, recurring: [], months: {}, balance: 0 };
  let sum = 0;
  for (const i of all) sum += ac.num[i] || 0;
  out.sum = m(sum);
  for (const period of a.periods) {
    let spent = 0;
    let income = 0;
    const rows = view(period);
    for (const i of rows) {
      const x = ac.num[i];
      if (x < 0) spent -= x;
      else if (x > 0) income += x;
    }
    out.summary[period] = { spent: m(spent), income: m(income), count: rows.length };
  }
  const cats: Record<string, number> = {};
  const flow: Record<string, { income: number; expense: number; count: number }> = {};
  for (const i of all) {
    const x = ac.num[i];
    const month = iso(dc.date[i]).slice(0, 7);
    const f = (flow[month] ??= { income: 0, expense: 0, count: 0 });
    f.count++;
    if (x < 0) {
      f.expense -= x;
      const k = (cc && ds.rows[i][cc.ci]) || '';
      cats[k] = (cats[k] ?? 0) - x;
    } else if (x > 0) f.income += x;
  }
  for (const [k, v] of Object.entries(cats)) out.categories[k] = m(v);
  for (const [k, v] of Object.entries(flow))
    out.cashflow[k] = { income: m(v.income), expense: m(v.expense), count: v.count };
  let dmax = -Infinity;
  for (const t of dc.date) if (t > dmax) dmax = t;
  out.recurring = L.findRecurring(ds, dc, ac, cc, pc, all, dmax).map((r: any) => ({
    payee: r.name,
    median: m(r.med),
    count: r.count,
    last: iso(r.last),
    next: iso(r.next),
  }));
  for (const month of a.months) {
    view('all');
    L.view().ovMonth = Date.parse(`${month}-01T00:00:00Z`);
    const c = L.ovCtx();
    out.months[month] = { income: m(c.cur.inc), expense: m(c.cur.exp), count: c.cur.n };
  }
  out.balance = m(S.opening + sum);
  if (a.priv) {
    view('all');
    const g = L.groupStats();
    out.groups = Object.fromEntries(
      Object.entries(g).map(([k, v]: [string, any]) => [k, { count: v.n, spent: m(v.spent) }]),
    );
    out.duplicates = ds.dupes;
    out.budgets = Object.fromEntries(Object.entries(S.budgets).map(([k, v]) => [k, m(Number(v))]));
    const inv = p.inv;
    out.investments = {
      ppkValue: m(inv.ppk.value),
      positions: inv.positions.map((x: any) => ({ name: x.name, invested: m(x.invested), value: m(x.value) })),
    };
  }
  return out;
}
/* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment, @typescript-eslint/no-unsafe-member-access, @typescript-eslint/no-unsafe-call, @typescript-eslint/no-unsafe-argument, @typescript-eslint/no-unsafe-return, @typescript-eslint/restrict-plus-operands */

async function post<T>(page: Page, path: string, data: unknown): Promise<T> {
  const r = await page.request.post(`/api/v1${path}`, { headers: API_HEADERS, data });
  expect(r.status(), `${path}: ${await r.text()}`).toBeLessThan(300);
  return (await r.json()) as T;
}
async function get<T>(page: Page, path: string): Promise<T> {
  const r = await page.request.get(`/api/v1${path}`, { headers: API_HEADERS });
  expect(r.status(), path).toBe(200);
  return (await r.json()) as T;
}

function periodOf(p: string): Schemas['Period'] | undefined {
  if (p === 'all') return undefined;
  if (p.startsWith('m:')) return { month: p.slice(2) };
  return { preset: p as 'l3m' };
}

/** The same facts from the new app's API, for the signed-in user (every account). */
async function apiFacts(page: Page, withMigration: boolean): Promise<Facts> {
  // The legacy app has no transfers: compare with transfers included, so a pair the new app finds
  // between the migrated accounts can't make the numbers differ (Phase 15 plan).
  const ctx = (period?: string) => ({
    includeTransfers: true,
    ...(period && period !== 'all' ? { period: periodOf(period) } : {}),
  });
  const q = await post<Schemas['TransactionPage']>(page, '/transactions/query', { context: ctx(), limit: 1 });
  const out: Facts = {
    sum: q.stats?.sum ?? NaN,
    summary: {},
    categories: {},
    cashflow: {},
    recurring: [],
    months: {},
    balance: 0,
  };
  for (const p of PERIODS) {
    const s = await post<Schemas['InsightSummary']>(page, '/insights/summary', { context: ctx(p) });
    out.summary[p] = { spent: s.spentMinor, income: s.incomeMinor, count: s.count };
  }
  const cats = await post<Schemas['InsightCategories']>(page, '/insights/categories', { context: ctx() });
  for (const c of cats.categories) out.categories[c.category?.name ?? ''] = c.spentMinor;
  const flow = await post<Schemas['InsightCashflow']>(page, '/insights/cashflow', { context: ctx() });
  for (const f of flow.months)
    out.cashflow[f.month] = { income: f.incomeMinor, expense: f.expenseMinor, count: f.count };
  for (const month of MONTHS) {
    const o = await post<Schemas['Overview']>(page, '/overview', { context: ctx(), month, months: 12 });
    const d = o.data;
    if (!d) throw new Error('no overview data');
    out.months[month] = { income: d.current.incomeMinor, expense: d.current.expenseMinor, count: d.current.count };
    if (month === MONTHS[0]) {
      // the legacy app finds monthly payments only (weekly and yearly are new in Phase 15)
      out.recurring = d.recurring
        .filter((r) => r.cadence === 'monthly')
        .map((r) => ({
          payee: r.payee,
          median: r.medianMinor,
          count: r.count,
          last: r.last,
          next: r.next,
        }));
      out.balance = d.balance.nowMinor;
    }
  }
  if (withMigration) {
    const groups = await get<Schemas['Group'][]>(page, '/groups');
    out.groups = Object.fromEntries(groups.map((g) => [g.name, { count: g.count, spent: g.spent.amountMinor }]));
    out.duplicates = q.duplicatesSkipped ?? NaN;
    const budgets = await get<Schemas['BudgetList']>(page, '/budgets');
    out.budgets = Object.fromEntries(budgets.budgets.map((b) => [b.category.name, b.amountMinor]));
    const inv = await get<Schemas['Investments']>(page, '/investments');
    out.investments = {
      ppkValue: inv.ppk.valueMinor,
      positions: inv.positions.map((x) => ({ name: x.name, invested: x.investedMinor, value: x.valueMinor })),
    };
  }
  return out;
}

/** One line of the report. */
type Row = { path: string; check: string; legacy: string; next: string; result: 'equal' | 'DIFFERENT' };
const report: Row[] = [];

function compare(path: string, legacy: Facts, next: Facts): Row[] {
  const rows: Row[] = [];
  const add = (check: string, a: unknown, b: unknown) => {
    const x = JSON.stringify(a);
    const y = JSON.stringify(b);
    rows.push({ path, check, legacy: x, next: y, result: x === y ? 'equal' : 'DIFFERENT' });
  };
  const sorted = <T>(o: Record<string, T>) =>
    Object.fromEntries(Object.entries(o).sort(([p], [q]) => (p < q ? -1 : 1)));
  add('All-time Σ', legacy.sum, next.sum);
  for (const p of PERIODS) add(`Summary ${p}`, legacy.summary[p], next.summary[p]);
  add('Category totals', sorted(legacy.categories), sorted(next.categories));
  add('Cash flow per month', sorted(legacy.cashflow), sorted(next.cashflow));
  add('Recurring payments', legacy.recurring, next.recurring);
  for (const m of MONTHS) add(`Overview ${m}`, legacy.months[m], next.months[m]);
  add('Final balance', legacy.balance, next.balance);
  if (legacy.groups) {
    add('Group totals', sorted(legacy.groups), sorted(next.groups ?? {}));
    add('Duplicates skipped', legacy.duplicates, next.duplicates);
    add('Budgets', sorted(legacy.budgets ?? {}), sorted(next.budgets ?? {}));
    add('Investments', legacy.investments, next.investments);
  }
  report.push(...rows);
  return rows;
}

test.afterAll(() => {
  if (report.length === 0) return;
  const cell = (s: string) => (s.length > 120 ? `${s.slice(0, 117)}…` : s).replace(/\|/g, '\\|');
  const lines = [
    '# Parity report',
    '',
    'Written by `web/e2e/parity.spec.ts` (PLAN task 14.3): the legacy single-file app and the new app on the same',
    'data, compared in minor units. Differences listed in `testdata/oracle/ALLOWED_DIFFS.md` would be marked',
    'there; any other difference fails the test.',
    '',
    `Result: ${String(report.filter((r) => r.result === 'equal').length)} of ${String(report.length)} checks equal.`,
    '',
    '| Path | Check | Legacy | New | Result |',
    '|---|---|---|---|---|',
    ...report.map((r) => `| ${r.path} | ${r.check} | ${cell(r.legacy)} | ${cell(r.next)} | ${r.result} |`),
    '',
  ];
  writeFileSync('../docs/parity-report.md', lines.join('\n'));
});

const b64 = (p: string) => readFileSync(fixture(p)).toString('base64');

test('direct: the legacy demo CSV in both apps', async ({ page, browser }) => {
  const legacy = await legacyFacts(
    browser,
    [{ id: 'f1', name: 'demo.csv', added: 0, b64: b64('demo/demo-legacy.csv'), cfg: null }],
    null,
  );
  await signUp(page);
  await importViaApi(page, 'Demo', 'demo/demo-legacy.csv');
  const rows = compare('direct', legacy, await apiFacts(page, false));
  expect(rows.filter((r) => r.result !== 'equal')).toEqual([]);
});

test('migration: the demo backup restored in the legacy app and migrated', async ({ page, browser }) => {
  const opened = await openBackup(parseBackup(readFileSync(fixture('legacy-backup/backup-plain.json'), 'utf8')));
  const files: LegacyFile[] = opened.files.map((f) => ({
    id: f.id,
    name: f.name,
    added: f.added,
    b64: Buffer.from(f.data).toString('base64'),
    cfg: f.cfg,
  }));
  const legacy = await legacyFacts(browser, files, opened.private);

  // one account, as the legacy merged every file into one view
  await signUp(page);
  const konto: Target = { new: 'Konto' };
  const { manifest } = buildManifest(opened, {
    targets: Object.fromEntries(opened.files.map((f) => [f.id, konto])),
    opening: konto,
    includeChats: false,
  });
  // sent from the page, as the wizard does (session cookie, one "file" part per legacy file)
  const started = await page.evaluate(
    async (body) => {
      const fd = new FormData();
      fd.append('manifest', body.manifest);
      body.files.forEach((f, i) => {
        fd.append('file', new Blob([Uint8Array.from(atob(f), (c) => c.charCodeAt(0))]), `f${String(i)}.csv`);
      });
      const res = await fetch('/api/v1/legacy-imports', {
        method: 'POST',
        body: fd,
        headers: { 'X-Finuance-Client': 'web' },
      });
      return { status: res.status, body: (await res.json()) as { id: string } };
    },
    { manifest: JSON.stringify(manifest), files: files.map((f) => f.b64) },
  );
  expect(started.status).toBe(202);
  await expect
    .poll(async () => (await get<Schemas['LegacyImport']>(page, `/legacy-imports/${started.body.id}`)).status, {
      timeout: 60_000,
    })
    .toBe('done');

  const rows = compare('migration', legacy, await apiFacts(page, true));
  expect(rows.filter((x) => x.result !== 'equal')).toEqual([]);
});
