#!/usr/bin/env node
// npm audit with the same accepted-findings file as Trivy (.trivyignore): `npm audit` has no way
// to accept one advisory, so a single finding without a fix would switch the whole gate off.
// Reads `npm audit --json` on stdin and fails on any HIGH or CRITICAL advisory that is not listed
// in the file given as the first argument, or whose `exp:YYYY-MM-DD` has passed.
//   npm audit --json | node tools/npm-audit.mjs .trivyignore
//   node tools/npm-audit.mjs --self-test
import { readFileSync } from 'node:fs';

/** Lines `ID exp:YYYY-MM-DD`; comments and blank lines are skipped. An entry without an expiry is not accepted. */
export function accepted(text, today) {
  const ids = new Map();
  for (const raw of text.split('\n')) {
    const line = raw.replace(/#.*/, '').trim();
    if (!line) continue;
    const [id, ...rest] = line.split(/\s+/);
    const exp = rest.map((r) => /^exp:(\d{4}-\d{2}-\d{2})$/.exec(r)?.[1]).find(Boolean);
    ids.set(id.toUpperCase(), Boolean(exp) && exp >= today);
  }
  return ids;
}

/** The HIGH and CRITICAL advisories of an `npm audit --json` report, one per advisory id. */
export function advisories(report) {
  const found = new Map();
  for (const v of Object.values(report.vulnerabilities ?? {})) {
    for (const via of v.via ?? []) {
      if (typeof via === 'string') continue; // a dependent of a vulnerable package, not an advisory
      if (via.severity !== 'high' && via.severity !== 'critical') continue;
      const id = (/GHSA-[0-9a-z-]+/i.exec(via.url ?? '')?.[0] ?? String(via.source)).toUpperCase();
      found.set(id, { id, name: via.name, severity: via.severity, title: via.title });
    }
  }
  return [...found.values()];
}

export function check(report, ignoreText, today) {
  const ok = accepted(ignoreText, today);
  const open = advisories(report).filter((a) => ok.get(a.id) !== true);
  const expired = open.filter((a) => ok.has(a.id));
  return { open, expired };
}

function selfTest() {
  const report = {
    vulnerabilities: {
      a: { via: [{ source: 1, name: 'a', severity: 'high', title: 't', url: 'https://github.com/advisories/GHSA-aaaa-bbbb-cccc' }] },
      b: { via: ['a'] },
      c: { via: [{ source: 2, name: 'c', severity: 'moderate', url: 'https://github.com/advisories/GHSA-dddd-eeee-ffff' }] },
    },
  };
  const cases = [
    ['an unlisted high advisory fails', '', 1, 0],
    ['an accepted one passes', 'GHSA-aaaa-bbbb-cccc exp:2030-01-01', 0, 0],
    ['an expired acceptance fails', 'GHSA-aaaa-bbbb-cccc exp:2020-01-01', 1, 1],
    ['an acceptance without an expiry fails', 'GHSA-aaaa-bbbb-cccc', 1, 1],
    ['a comment is not an entry', '# GHSA-aaaa-bbbb-cccc exp:2030-01-01', 1, 0],
    ['other ids do not help', 'CVE-2026-1 exp:2030-01-01', 1, 0],
  ];
  for (const [name, text, open, expired] of cases) {
    const r = check(report, text, '2026-11-18');
    if (r.open.length !== open || r.expired.length !== expired) {
      console.error(`npm-audit self-test: ${name}: got ${r.open.length} open, ${r.expired.length} expired`);
      process.exit(1);
    }
  }
  console.log(`npm-audit self-test: ok (${cases.length} cases)`);
}

if (process.argv[2] === '--self-test') {
  selfTest();
} else {
  const ignoreFile = process.argv[2];
  if (!ignoreFile) {
    console.error('usage: npm audit --json | node tools/npm-audit.mjs <accepted-file>');
    process.exit(2);
  }
  const report = JSON.parse(readFileSync(0, 'utf8'));
  const today = new Date().toISOString().slice(0, 10);
  const { open, expired } = check(report, readFileSync(ignoreFile, 'utf8'), today);
  for (const a of open) {
    const why = expired.includes(a) ? ' (its acceptance has expired or has no expiry)' : '';
    console.error(`npm audit: ${a.severity.toUpperCase()} ${a.id} in ${a.name}: ${a.title}${why}`);
  }
  if (open.length) process.exit(1);
  console.log(`npm audit: no HIGH or CRITICAL advisory outside ${ignoreFile}`);
}
