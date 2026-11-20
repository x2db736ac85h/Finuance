// Security headers (PLAN §8.1): every page and API response carries each of them exactly once,
// and the server's own redirects don't leak the container's port.
// nginx adds them and the API sets some too; a header sent twice with different values is
// combined or ignored differently by each browser (found by the Phase 17 ZAP run).
import { expect, test } from '@playwright/test';

const ONCE = [
  'content-security-policy',
  'x-content-type-options',
  'referrer-policy',
  'permissions-policy',
  'cross-origin-opener-policy',
  'cross-origin-resource-policy',
  'cross-origin-embedder-policy',
];

test('each security header is sent once, on pages and on API responses', async ({ request }) => {
  for (const path of ['/t/all', '/api/v1/meta', '/api/v1/nowhere']) {
    const res = await request.get(path);
    const names = res.headersArray().map((h) => h.name.toLowerCase());
    for (const h of ONCE) {
      expect(
        names.filter((n) => n === h),
        `${h} on ${path}`,
      ).toHaveLength(1);
    }
  }
});

test('redirects stay on the public address', async ({ request }) => {
  const redirects: [string, string][] = [
    ['/api', '/api/'],
    ['/api/v1', '/api/v1/'],
  ];
  for (const [path, to] of redirects) {
    const res = await request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).toBeGreaterThanOrEqual(300);
    expect(res.headers()['location'], path).toBe(to);
  }
});

// One id per request over both hops (PLAN 23.4): the web server gives every API request its own
// id and passes it on, so a client can't choose the id that the logs are joined on.
test("the request id is the web server's own", async ({ request }) => {
  const first = await request.get('/api/v1/meta', { headers: { 'X-Request-ID': 'chosen-by-the-client' } });
  const second = await request.get('/api/v1/meta');
  for (const res of [first, second]) {
    expect(res.headers()['x-request-id']).toMatch(/^[0-9a-f]{32}$/);
  }
  expect(first.headers()['x-request-id']).not.toBe(second.headers()['x-request-id']);
});

// The policy names what the app needs and closes the rest (register WEB-3): no plugins, no
// frames, workers from here only, and Trusted Types for anything that turns text into script.
test('the content security policy closes plugins, frames and DOM script sinks', async ({ request }) => {
  const csp = (await request.get('/t/all')).headers()['content-security-policy'] ?? '';
  const directives = csp.split(';').map((d) => d.trim());
  for (const d of [
    "default-src 'self'",
    "script-src 'self'",
    "object-src 'none'",
    "worker-src 'self'",
    "frame-src 'none'",
    "frame-ancestors 'none'",
    "base-uri 'none'",
    "require-trusted-types-for 'script'",
  ]) {
    expect(directives, d).toContain(d);
  }
  expect((await request.get('/t/all')).headers()['permissions-policy']).not.toContain('interest-cohort');
});

// The browser's errors reach the server (PLAN 24.8): the policy names where violations are reported,
// that route takes a browser's report (no client header, the browser's own content type) through
// the web server, and the web app's own reports are accepted without a session.
test('policy violations and the app’s errors have somewhere to go', async ({ request }) => {
  const page = await request.get('/t/all');
  const csp = page.headers()['content-security-policy'] ?? '';
  expect(csp).toContain('report-uri /api/v1/csp-reports');
  expect(csp).toContain('report-to csp');
  expect(page.headers()['reporting-endpoints']).toBe('csp="/api/v1/csp-reports"');

  const report = await request.post('/api/v1/csp-reports', {
    headers: { 'Content-Type': 'application/csp-report' },
    data: JSON.stringify({ 'csp-report': { 'violated-directive': 'script-src', 'blocked-uri': 'inline' } }),
  });
  expect(report.status()).toBe(204);

  const event = await request.post('/api/v1/client-events', {
    headers: { 'X-Finuance-Client': 'web' },
    data: { kind: 'error', name: 'TypeError', route: '/t/:id' },
  });
  expect(event.status()).toBe(204);
  const withText = await request.post('/api/v1/client-events', {
    headers: { 'X-Finuance-Client': 'web' },
    data: { kind: 'error', name: 'what the person typed', route: '/t/all?q=private' },
  });
  expect(withText.status(), 'free text is refused').toBeGreaterThanOrEqual(400);
});

// Body sizes at the web server (register OPS-7): the migration from the single-file app may be
// larger than one upload; every other route keeps the upload limit. Nobody is signed in, so a
// body the web server lets through is answered by the api (401), not stored.
test('the web server lets a large migration through and nothing else', async ({ request }) => {
  const big = Buffer.alloc(30 * 1024 * 1024, 'a');
  const headers = { 'Content-Type': 'application/octet-stream', 'X-Finuance-Client': 'web' };
  const migration = await request.post('/api/v1/legacy-imports', { data: big, headers });
  expect(migration.status(), 'a 30 MiB migration').not.toBe(413);
  const upload = await request.post('/api/v1/imports', { data: big, headers });
  expect(upload.status(), 'a 30 MiB upload').toBe(413);
});
