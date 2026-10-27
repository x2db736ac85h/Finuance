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
