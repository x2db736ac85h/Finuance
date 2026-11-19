/* global URL, console, Buffer */
// Writes src/lib/qr.fixtures.json: QR codes made by libqrencode (`qrencode`, byte mode, level M)
// for texts of several lengths, as rows of 0 and 1. src/lib/qr.test.ts compares our encoder with
// them module for module. Run by hand when the list changes: node scripts/qr-fixtures.mjs
// (needs qrencode 4.x on the PATH; the result is committed, so tests and CI don't need it).
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';

const secret = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ';
const uri = (issuer, account) =>
  `otpauth://totp/${encodeURIComponent(issuer)}:${encodeURIComponent(account)}?algorithm=SHA1&digits=6&issuer=${encodeURIComponent(issuer)}&period=30&secret=${secret}`;
const texts = [
  'A',
  'Finuance',
  'https://example.com/a',
  'zażółć gęślą jaźń — 2026',
  'otpauth://totp/F:a@example.com?secret=' + secret,
  uri('Finuance', 'alex@example.com'),
  uri('Finuance (finance.example.com)', 'alexandra.longer-name+finuance@mail.example.org'),
  uri('Finuance (finuance.example.ts.net)', 'a.very.long.address.for.a.person@department.university.example.com') +
    '&x=' +
    'y'.repeat(60),
  'x'.repeat(330),
  'y'.repeat(412),
  // with the texts above, libqrencode uses each of the eight masks at least once
  ...[30, 0, 6, 5, 35, 59, 3].map((i) => `mask probe ${i}`),
];
const fixtures = texts.map((text) => {
  const out = execFileSync('qrencode', ['-l', 'M', '-8', '-m', '0', '-t', 'ASCII', text], { encoding: 'utf8' });
  const rows = out
    .split('\n')
    .filter((l) => l.length > 0)
    .map((l) => l.replace(/(..)/g, (_, pair) => (pair === '##' ? '1' : '0')));
  return { text, version: (rows.length - 17) / 4, rows };
});
writeFileSync(new URL('../src/lib/qr.fixtures.json', import.meta.url), JSON.stringify(fixtures, null, 2) + '\n');
console.log(fixtures.map((f) => `v${f.version} (${Buffer.byteLength(f.text)} bytes)`).join(', '));
