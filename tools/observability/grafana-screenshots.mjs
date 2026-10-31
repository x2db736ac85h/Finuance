// Screenshots of the Finuance dashboards in a running Grafana (Phase 18.6 walkthrough; the gate's
// "Grafana walkthrough"). Uses the web app's Playwright (run `npm ci` in web/ first).
//
//   kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80 &
//   GRAFANA_PASSWORD=… node tools/observability/grafana-screenshots.mjs [out-dir]
//
// GRAFANA_URL (default http://localhost:3000), GRAFANA_USER (admin), GRAFANA_PASSWORD (required; on
// the homelab: kubectl -n monitoring get secret grafana-admin -o jsonpath='{.data.admin-password}' |
// base64 -d), FROM (default now-30m), THEME (dark), CHROMIUM_PATH (a Chromium binary when
// Playwright's own build isn't installed, e.g. ~/.cache/ms-playwright/chromium-<n>/chrome-linux64/chrome).
// Writes <out-dir>/<dashboard uid>.png, default docs/screenshots/grafana/homelab.
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const { chromium } = createRequire(join(root, 'web/package.json'))('@playwright/test');

const url = process.env.GRAFANA_URL || 'http://localhost:3000';
const user = process.env.GRAFANA_USER || 'admin';
const password = process.env.GRAFANA_PASSWORD;
if (!password) {
  console.error('grafana-screenshots: set GRAFANA_PASSWORD');
  process.exit(2);
}
const from = process.env.FROM || 'now-30m';
const theme = process.env.THEME || 'dark';
const out = resolve(process.argv[2] || join(root, 'docs/screenshots/grafana/homelab'));
mkdirSync(out, { recursive: true });

// The dashboards of deploy/k8s/observability (tools/observability/dashboards.mjs), in walkthrough order.
const dashboards = ['finuance-slo', 'finuance-api', 'finuance-jobs', 'finuance-runtime', 'finuance-postgres', 'finuance-assistant'];

const browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {});
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1 });
  await page.goto(`${url}/login`);
  await page.getByTestId('data-testid Username input field').fill(user);
  await page.getByTestId('data-testid Password input field').fill(password);
  await page.getByTestId('data-testid Login button').click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'));

  for (const uid of dashboards) {
    await page.goto(`${url}/d/${uid}?orgId=1&from=${from}&to=now&theme=${theme}&kiosk`);
    // Panels load lazily and query on their own: wait until no panel shows a loading bar any more.
    await page.waitForLoadState('networkidle');
    await page.waitForFunction(() => !document.querySelector('[aria-label="Panel loading bar"]'), null, { timeout: 30000 });
    // A viewport as tall as the dashboard instead of a scrolling full-page capture: kiosk mode's
    // sticky footer would otherwise land in the middle, and panels below the fold load only when seen.
    const height = await page.evaluate(() =>
      Math.max(...[...document.querySelectorAll('*')].map((e) => e.scrollHeight)),
    );
    await page.setViewportSize({ width: 1600, height: Math.min(height, 6000) });
    await page.waitForLoadState('networkidle');
    await page.waitForFunction(() => !document.querySelector('[aria-label="Panel loading bar"]'), null, { timeout: 30000 });
    await page.waitForTimeout(1500);
    const file = join(out, `${uid}.png`);
    await page.screenshot({ path: file });
    await page.setViewportSize({ width: 1600, height: 1000 });
    console.log(`grafana-screenshots: ${file}`);
  }
} finally {
  await browser.close();
}
