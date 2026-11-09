#!/usr/bin/env node
// Legacy oracle: runs legacy/finuance.html in headless Chromium and records its behaviour as JSON
// expectations in testdata/oracle/ (plus the legacy demo CSV and backups in testdata/fixtures/).
// Deterministic: seeded Math.random and crypto.getRandomValues, frozen clock, fixed locale/time zone.
// Usage: node tools/legacy-oracle/oracle.mjs   (or `make oracle`)

import {createServer} from 'node:http';
import {readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync, statSync, existsSync} from 'node:fs';
import {dirname, join, relative} from 'node:path';
import {fileURLToPath} from 'node:url';
import {chromium} from 'playwright';

import * as corpus from './cases/corpus.mjs';
import filterCases from './cases/filters.mjs';
import toolCases from './cases/tools.mjs';
import cfgCases from './cases/cfg.mjs';
import portfolios from './cases/investments.mjs';
import chartCases from './cases/charts.mjs';
import overviewCases from './cases/overview.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, '..', '..');
const LEGACY = join(ROOT, 'legacy', 'finuance.html');
const FIX = join(ROOT, 'testdata', 'fixtures');
const OUT = join(ROOT, 'testdata', 'oracle');
const FROZEN_NOW = '2025-03-20T10:00:00Z';   // a Thursday, three days after the demo data ends
const BACKUP_PASSPHRASE = 'oracle-test-passphrase';
const GENERATED = ['ingest', 'ingest-cfg', 'corpus', 'dedupe', 'txkeys', 'filters', 'recurring-periods', 'assistant', 'investments', 'charts', 'overview'];

// Names exposed from the legacy IIFE. The first block is the list in docs/PLAN.md Phase 1 task 1.4; the
// second is what the overview, duplicates and backup outputs additionally need.
const NAMES = [
  'decode', 'detectDelim', 'parseCSV', 'buildTable', 'fileOpts', 'applyMapping', 'parseEntry', 'parseNum', 'parseDate', 'looseDate',
  'analyzeCol', 'makeDataset', 'detectRoles', 'norm', 'payeeName', 'compileCond', 'getDataset', 'dupChanged', 'txKeys', 'applyGroupsAll',
  'cardOrder', 'exportBackup', 'ser', 'deser', 'findRecurring', 'resolvePeriod', 'roundNice', 'demoCSV', 'aiData', 'aiFilter', 'execTool',
  'sysPrompt', 'S',
  'CHARTS', 'ovCtx', 'drawChart', 'view', 'setActive', 'addFiles', 'setPassphrase', 'savePrivate', 'saveChatsNow', 'INV0', 'compute', 'vizCtx'
];

/* ---------- deterministic JSON ---------- */
const sortKeys = v => Array.isArray(v) ? v.map(sortKeys)
  : v && typeof v === 'object' ? Object.fromEntries(Object.keys(v).sort().map(k => [k, sortKeys(v[k])])) : v;
const stable = v => JSON.stringify(sortKeys(v), null, 2) + '\n';
function put(path, data){
  mkdirSync(dirname(path), {recursive: true});
  writeFileSync(path, typeof data === 'string' || Buffer.isBuffer(data) ? data : stable(data));
}

/* ---------- legacy page with the injection ---------- */
function injectedHtml(){
  const src = readFileSync(LEGACY, 'utf8');
  const anchor = /^updateToggles\(\);$/gm;
  const n = (src.match(anchor) || []).length;
  if (n !== 1) throw new Error(`legacy-oracle: expected the anchor "updateToggles();" exactly once at the start of a line, found ${n}`);
  return src.replace(anchor, `window.__legacy = {${NAMES.join(', ')}};\nupdateToggles();`);
}

// Seeded xorshift32 for Math.random and crypto.getRandomValues (ids, salts, IVs become reproducible).
const INIT_SCRIPT = `(() => {
  let s = 0x9E3779B9 >>> 0;
  const next = () => { s ^= s << 13; s >>>= 0; s ^= s >>> 17; s ^= s << 5; s >>>= 0; return s; };
  Math.random = () => next() / 4294967296;
  const fill = a => { const u = new Uint8Array(a.buffer, a.byteOffset, a.byteLength); for (let i = 0; i < u.length; i++) u[i] = next() & 255; return a; };
  Object.defineProperty(crypto, 'getRandomValues', {value: fill});
})();`;

const HELPERS = readFileSync(join(HERE, 'page-helpers.js'), 'utf8');

async function main(){
  const html = injectedHtml();
  const server = createServer((req, res) => {
    if (req.url === '/' || req.url === '/index.html'){ res.writeHead(200, {'content-type': 'text/html; charset=utf-8'}); res.end(html); }
    else { res.writeHead(404); res.end(); }
  });
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const url = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({executablePath: process.env.ORACLE_CHROMIUM || undefined});
  const errors = [];

  // a fresh context (fresh IndexedDB, fresh seed) for every task
  async function withPage(label, fn){
    const ctx = await browser.newContext({bypassCSP: true, locale: 'en-US', timezoneId: 'UTC', acceptDownloads: true, viewport: {width: 1400, height: 900}});
    await ctx.addInitScript(INIT_SCRIPT);
    const page = await ctx.newPage();
    page.on('pageerror', e => errors.push(`${label}: ${e.message}`));
    await page.clock.setFixedTime(new Date(FROZEN_NOW));
    await page.goto(url);
    await page.waitForFunction(() => window.__legacy && window.__legacy.S.booted);
    await page.addScriptTag({content: HELPERS});
    try { return await fn(page); } finally { await ctx.close(); }
  }
  const call = (page, name, ...args) => page.evaluate(([n, a]) => window.__oracle[n](...a), [name, args]);

  // only the generated folders; hand-written files such as ALLOWED_DIFFS.md stay
  for (const d of GENERATED) rmSync(join(OUT, d), {recursive: true, force: true});
  rmSync(join(FIX, 'legacy-backup'), {recursive: true, force: true});
  const counts = {};
  const count = (k, n = 1) => { counts[k] = (counts[k] || 0) + n; };

  /* demo dataset (also a fixture) */
  const demo = await withPage('demo', page => page.evaluate(() => window.__legacy.demoCSV()));
  put(join(FIX, 'demo', 'demo-legacy.csv'), demo);
  const demoB64 = Buffer.from(demo, 'utf8').toString('base64');
  const csv = (name, b64, cfg) => ({name, b64, cfg});

  /* ingest: every committed fixture, default options */
  const files = ['synthetic', 'demo'].flatMap(d => walk(join(FIX, d))).filter(p => p.endsWith('.csv')).sort();
  for (const p of files){
    const rel = relative(FIX, p), b64 = readFileSync(p).toString('base64');
    const res = await withPage(rel, page => call(page, 'ingest', csv(rel.split('/').pop(), b64)));
    put(join(OUT, 'ingest', rel.replace(/\.csv$/, '.json')), {fixture: rel, options: {enc: 'auto', delim: 'auto', strip: true}, ...res});
    count('ingest');
  }

  /* per-file import settings (v7) */
  for (const [file, variant, cfg] of cfgCases){
    const b64 = readFileSync(join(FIX, 'synthetic', file)).toString('base64');
    const res = await withPage(`${file}:${variant}`, page => call(page, 'ingest', csv(file.split('/').pop(), b64, cfg)));
    put(join(OUT, 'ingest-cfg', file.replace(/\.csv$/, ''), variant + '.json'), {fixture: 'synthetic/' + file, variant, ...res});
    count('ingest-cfg');
  }

  /* value corpora */
  await withPage('corpus', async page => {
    for (const [name, inputs] of Object.entries(corpus)){
      const uniq = [...new Map(inputs.map(x => [JSON.stringify(x), x])).values()];
      put(join(OUT, 'corpus', name + '.json'), {function: name, cases: await call(page, 'corpus', name, uniq)});
      count('corpus cases', uniq.length);
    }
  });

  /* dedupe, duplicate groups and overrides */
  const fx = (...ps) => ps.map(p => csv(p.split('/').pop(), readFileSync(join(FIX, 'synthetic', p)).toString('base64')));
  const sets = [
    ['overlap-A-B', fx('overlap/A_jan-mar.csv', 'overlap/B_mar-may.csv'), true],
    ['overlap-A-C', fx('overlap/A_jan-mar.csv', 'overlap/C_q1_again.csv'), true],
    ['overlap-C-A', fx('overlap/C_q1_again.csv', 'overlap/A_jan-mar.csv'), false],
    ['overlap-A-B-C', fx('overlap/A_jan-mar.csv', 'overlap/B_mar-may.csv', 'overlap/C_q1_again.csv'), true],
    ['same-name', fx('same-name/a/export.csv', 'same-name/b/export.csv'), false],
    ['dup-versions', fx('dup-versions/v1_jan-feb.csv', 'dup-versions/v2_jan-mar.csv'), true],
    ['dup-versions-reversed', fx('dup-versions/v2_jan-mar.csv', 'dup-versions/v1_jan-feb.csv'), true],
    ['bank-layouts', fx('42-bank-a-layout.csv', '43-bank-b-layout.csv'), false]
  ];
  for (const [name, set, overrides] of sets){
    const res = await withPage(name, page => call(page, 'dedupe', set, overrides));
    put(join(OUT, 'dedupe', name + '.json'), {set: name, files: set.map(f => f.name), ...res});
    count('dedupe');
  }

  /* group keys (txKeys: the #n twin rule) */
  for (const [name, set] of sets.filter(s => s[0].startsWith('overlap') || s[0] === 'same-name')){
    put(join(OUT, 'txkeys', name + '.json'), {set: name, datasets: await withPage('txkeys ' + name, page => call(page, 'txKeysOf', set))});
    count('txkeys');
  }

  /* filters on the demo dataset */
  put(join(OUT, 'filters', 'demo.json'), {dataset: 'demo/demo-legacy.csv', columnMap: 'tools/legacy-oracle/column-map.json',
    cases: await withPage('filters', page => call(page, 'filters', demoB64, filterCases))});
  count('filter cases', filterCases.length);

  /* recurring payments and period presets */
  const presets = [['all'], ['l30'], ['l3m'], ['l12m'], ['ytd'], ['m:2024-06'], ['m:2025-03'], ['m:2023-01'], ['y:2024'], ['y:2025'],
    ['custom', ['2024-07-01T00:00:00Z', '2024-07-15T00:00:00Z']], ['custom', ['2025-03-10T00:00:00Z', '2025-03-11T00:00:00Z']], ['bogus']];
  put(join(OUT, 'recurring-periods', 'demo.json'), {dataset: 'demo/demo-legacy.csv', ...await withPage('recurring', page => call(page, 'recurringAndPeriods', demoB64, presets))});

  /* assistant tools and system prompt (today frozen) */
  const ai = await withPage('assistant', page => call(page, 'assistant', demoB64, toolCases));
  put(join(OUT, 'assistant', 'tools.json'), {dataset: 'demo/demo-legacy.csv', frozenNow: FROZEN_NOW, aiData: ai.aiData, cases: ai.results});
  put(join(OUT, 'assistant', 'system-prompt.txt'), ai.sysPrompt);
  put(join(OUT, 'assistant', 'system-prompt-no-data.txt'), ai.sysPromptNoData);
  count('assistant cases', toolCases.length);

  /* investments: Investment, Yield on cost, FI/RE table twins */
  const variants = investmentDatasets(demo);
  for (const [name, dataset, inv] of portfolios){
    const b64 = Buffer.from(variants[dataset], 'utf8').toString('base64');
    const months = await withPage('inv ' + name, page => call(page, 'investments', b64, dataset + '.csv', inv));
    put(join(OUT, 'investments', name + '.json'), {portfolio: name, dataset, inv, months});
    count('investments');
  }

  /* insight charts: table twins of the six pane charts */
  put(join(OUT, 'charts', 'demo.json'), {dataset: 'demo/demo-legacy.csv', columnMap: 'tools/legacy-oracle/column-map.json',
    cases: await withPage('charts', page => call(page, 'charts', demoB64, chartCases))});
  count('chart cases', chartCases.length);

  /* overview: context, card twins and highlight sentences */
  put(join(OUT, 'overview', 'demo.json'), {dataset: 'demo/demo-legacy.csv', columnMap: 'tools/legacy-oracle/column-map.json',
    cases: await withPage('overview', page => call(page, 'overview', demoB64, overviewCases))});
  count('overview cases', overviewCases.length);

  /* legacy backups (plain and passphrase-locked) */
  await withPage('backup', page => backups(page, demoB64));

  await browser.close();
  server.close();
  if (errors.length){ console.error('legacy-oracle: page errors:\n  ' + errors.join('\n  ')); process.exit(1); }
  console.log('legacy-oracle: ok ·', Object.entries(counts).map(([k, v]) => `${k} ${v}`).join(' · '));
}

function walk(d){
  if (!existsSync(d)) return [];
  return readdirSync(d).flatMap(n => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : [p]; });
}

// Variants of the demo CSV for the investment cards: dividends in the data (yield-on-cost fallback),
// a short history (< 12 months) and expenses only (FI not reachable).
function investmentDatasets(demo){
  const [head, ...rows] = demo.split('\n');
  const date = r => { const [d, m, y] = r.split(';')[0].split('.'); return `${y}-${m}-${d}`; };
  const amount = r => parseFloat(r.split(';')[7].replace(/\s/g, '').replace(',', '.'));
  const extra = [
    '15.06.2024;15.06.2024;DOM MAKLERSKI PRZYKŁADOWY;;00000000000000000000000000;;DYWIDENDA SPÓŁKA ALFA;640,00;PLN;DEMODIV1;PRZELEW PRZYCHODZĄCY;Inwestycje',
    '30.09.2024;30.09.2024;BANK PRZYKŁADOWY;;00000000000000000000000000;;ODSETKI OD LOKATY;125,40;PLN;DEMODIV2;PRZELEW PRZYCHODZĄCY;Inwestycje',
    '20.12.2024;20.12.2024;DOM MAKLERSKI PRZYKŁADOWY;;00000000000000000000000000;;DIVIDEND ETF;212,15;PLN;DEMODIV3;PRZELEW PRZYCHODZĄCY;Inwestycje'
  ];
  return {
    'demo': demo,
    'demo-dividends': [head, ...extra, ...rows].join('\n'),
    'demo-short': [head, ...rows.filter(r => date(r) >= '2024-11-01')].join('\n'),
    'demo-expenses-only': [head, ...rows.filter(r => amount(r) < 0)].join('\n')
  };
}

async function backups(page, demoB64){
  const fixtures = ['dup-versions/v1_jan-feb.csv', 'dup-versions/v2_jan-mar.csv'].map(p => ({name: p.split('/').pop(), b64: readFileSync(join(FIX, 'synthetic', p)).toString('base64')}));
  await page.evaluate(async ([demo, more]) => {
    const L = window.__legacy, S = L.S, O = window.__oracle;
    const file = f => { const b = O.bytes(f.b64).buffer; return {name: f.name, size: b.byteLength, arrayBuffer: async () => b}; };
    await L.addFiles([file({name: 'demo.csv', b64: demo}), ...more.map(file)]);
    for (let i = 0; i < 200 && !S.files.every(f => f.saved); i++) await new Promise(r => setTimeout(r, 25));
    if (!S.files.every(f => f.saved)) throw new Error('files were not saved to IndexedDB');
    // groups on the first rows of the merged view
    const ds = L.getDataset('merged'), keys = L.txKeys(ds);
    S.groups = [{id: 'g1', name: 'Wakacje nad morzem', color: 1, created: 0}, {id: 'g2', name: 'Remont', color: 2, created: 0}];
    keys.slice(0, 5).forEach(k => { S.tags[k] = 'g1'; }); keys.slice(5, 8).forEach(k => { S.tags[k] = 'g2'; });
    L.applyGroupsAll();
    S.budgets = {'Sport': 100, 'Rachunki': 400, 'Artykuły spożywcze': 1500};
    S.opening = 1500;
    const groups = [...ds.dup.groups.values()];
    if (groups[0]) S.dupPick[groups[0].k] = ds.dup.files[ds.dup.src[groups[0].idx[groups[0].idx.length - 1]]].id;
    if (groups[1]) S.dupKeep[groups[1].k] = 1;
    if (!groups[1]) throw new Error('backup fixture needs at least two duplicate groups');
    S.inv = {ppk: {employee: 12000, employer: 9000, state: 690, value: 25400}, positions: [{id: 'p1', name: 'ETF World', type: 'ETF', invested: 40000, value: 46500, income: 0}], swr: 4, ret: 5};
    const t0 = Date.parse('2025-03-19T08:00:00Z');
    S.chats = [
      {id: 'c1', created: t0, updated: t0 + 60000, title: 'How much did I spend on groceries last month?',
        turns: [{role: 'user', text: 'How much did I spend on groceries last month?'},
          {role: 'ai', content: 'You spent **1 234,56 PLN** on groceries in February 2025.', reasoning: '', meta: {model: 'qwen2.5-7b-instruct'}, error: '', sent: t0,
            steps: [{name: 'aggregate_transactions', args: {group_by: 'none', category: 'Artykuły spożywcze', date_from: '2025-02-01', date_to: '2025-02-28'}, summary: 'by none · 2025-02-01 → 2025-02-28', json: '{}', sel: null}]}],
        history: [{role: 'user', content: 'How much did I spend on groceries last month?'}]},
      {id: 'c2', created: t0 - 86400000, updated: t0 - 86000000, title: 'Which subscriptions do I pay?',
        turns: [{role: 'user', text: 'Which subscriptions do I pay?'}, {role: 'ai', content: 'Internet (79,99 PLN/month) and energy.', reasoning: '', meta: {}, error: '', sent: t0 - 86400000, steps: []}],
        history: [{role: 'user', content: 'Which subscriptions do I pay?'}]}
    ];
    await L.savePrivate(); await L.saveChatsNow();
  }, [demoB64, fixtures]);
  const grab = async name => {
    const [dl] = await Promise.all([page.waitForEvent('download'), page.evaluate(() => window.__legacy.exportBackup())]);
    const p = join(FIX, 'legacy-backup', name);
    mkdirSync(dirname(p), {recursive: true});
    await dl.saveAs(p);
  };
  await grab('backup-plain.json');
  await page.evaluate(p => window.__legacy.setPassphrase(p), BACKUP_PASSPHRASE);
  await grab('backup-locked.json');
  put(join(FIX, 'legacy-backup', 'README.md'), `# Legacy backups\n\nGenerated by \`make oracle\` from the legacy app (do not edit). Both contain the demo data plus\n` +
    `dup-versions/v1_jan-feb.csv and dup-versions/v2_jan-mar.csv (same layout as the demo), two groups, budgets, an opening balance, one \`dupPick\` and one \`dupKeep\`\n` +
    `override, an investment portfolio and two chats.\n\n* \`backup-plain.json\` — unencrypted.\n* \`backup-locked.json\` — passphrase lock on; passphrase \`${BACKUP_PASSPHRASE}\` (test-only).\n\n` +
    `They drive the legacy-migration tests (Phase 14).\n`);
}

main().catch(err => { console.error(err); process.exit(1); });
