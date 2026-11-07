#!/usr/bin/env node
// Renders the synthetic receipts of receipts.json to JPEG (PLAN 20.8) with the web app's pinned
// Playwright: `node evals/assistant/receipts/render.cjs` (CHROMIUM=/path/to/chromium when
// Playwright's own browser isn't installed). The images are committed; run this only after
// changing receipts.json. Each receipt is a thermal-paper slip on a table, optionally rotated,
// noisy (paper grain), blurred (out of focus) or faded (old print).
'use strict';

const fs = require('node:fs');
const path = require('node:path');

const here = __dirname;
const { chromium } = require(require.resolve('@playwright/test', { paths: [path.join(here, '..', '..', '..', 'web')] }));

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

function line(l) {
  switch (l[0]) {
    case 'c':
      return `<div class="c">${esc(l[1])}</div>`;
    case 'l':
      return `<div>${esc(l[1])}</div>`;
    case 'lr':
      return `<div class="lr"><span>${esc(l[1])}</span><span>${esc(l[2])}</span></div>`;
    case 'b':
      return `<div class="lr big"><span>${esc(l[1])}</span><span>${esc(l[2])}</span></div>`;
    case 'hr':
      return '<div class="hr"></div>';
    default:
      throw new Error(`unknown line ${JSON.stringify(l)}`);
  }
}

// paper grain: SVG turbulence, laid over the slip
const grain =
  "url(\"data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='300' height='300'>" +
  "<filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='3' seed='7'/></filter>" +
  "<rect width='100%' height='100%' filter='url(%23n)'/></svg>\")";

function html(r) {
  const s = r.style ?? {};
  return `<!doctype html><meta charset="utf-8"><style>
  body { margin: 0; background: #cfc8bc; }
  #wrap { display: inline-block; padding: 48px 56px; }
  .slip { position: relative; width: 340px; padding: 22px 18px 26px; background: #fbfaf5; color: ${s.fade ? '#77736c' : '#1f1f1f'};
    font: 14px/1.45 'DejaVu Sans Mono', 'Liberation Mono', monospace; box-shadow: 0 2px 10px rgba(0,0,0,.25);
    transform: rotate(${s.rotate ?? 0}deg); filter: blur(${s.blur ?? 0}px); }
  .slip::after { content: ''; position: absolute; inset: 0; background: ${grain}; opacity: ${s.noise ?? 0}; mix-blend-mode: multiply; }
  .c { text-align: center; }
  .lr { display: flex; justify-content: space-between; gap: 12px; }
  .lr span:last-child { white-space: nowrap; }
  .big { font-weight: bold; font-size: 16px; margin: 2px 0; }
  .hr { border-top: 1px dashed currentColor; margin: 6px 0; opacity: .7; }
  </style><div id="wrap"><div class="slip">${r.lines.map(line).join('')}</div></div>`;
}

async function main() {
  const { receipts } = JSON.parse(fs.readFileSync(path.join(here, 'receipts.json'), 'utf8'));
  const browser = await chromium.launch(process.env.CHROMIUM ? { executablePath: process.env.CHROMIUM } : {});
  const page = await browser.newPage({ deviceScaleFactor: 1.75 });
  for (const r of receipts) {
    await page.setContent(html(r));
    const file = path.join(here, `${r.id}.jpg`);
    await page.locator('#wrap').screenshot({ path: file, type: 'jpeg', quality: 82 });
    console.log(`${r.id}.jpg ${(fs.statSync(file).size / 1024).toFixed(0)} KB`);
  }
  await browser.close();
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
