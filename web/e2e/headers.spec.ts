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
