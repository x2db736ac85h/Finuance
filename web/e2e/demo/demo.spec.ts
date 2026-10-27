// Demo mode (PLAN 19.1), against compose.ci.yaml + compose.demo.yaml (make test-e2e-demo, weekly
// workflow): a visitor tries the demo from the sign-in page and gets a demo year of their own;
// sign-up, the migration and big uploads are refused. Uses 2 of the 5 demo users an address may
// start per hour.
import { expect, test } from '@playwright/test';

import { API_HEADERS } from '../helpers';

test('a visitor tries the demo and gets their own demo year', async ({ page, browser }) => {
  await page.goto('/login');
  await expect(page.getByRole('link', { name: 'Create an account' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Try the demo' }).click();
  await expect(page).toHaveURL(/\/t\/all$/);
  await expect(page.getByText('Demo — your data is deleted after 24 hours.')).toBeVisible();
  await page.getByRole('button', { name: 'Toggle sidebar' }).click();
  const explorer = page.getByRole('navigation', { name: 'Explorer' });
  await expect(explorer.getByRole('link', { name: /Demo account/ })).toBeVisible();
  await expect(page.getByRole('row').nth(5)).toBeVisible();

  const me = (await (await page.request.get('/api/v1/me', { headers: API_HEADERS })).json()) as { isDemo: boolean };
  expect(me.isDemo).toBe(true);
  const mine = (await (await page.request.get('/api/v1/accounts', { headers: API_HEADERS })).json()) as {
    id: string;
  }[];

  // what the demo instance refuses
  const signup = await page.request.post('/api/v1/auth/signup', {
    headers: API_HEADERS,
    data: { email: 'someone@example.com', password: 'a long enough password' },
  });
  expect(signup.status()).toBe(403);
  const big = await page.request.post('/api/v1/imports', {
    headers: API_HEADERS,
    multipart: { file: { name: 'big.csv', mimeType: 'text/csv', buffer: Buffer.alloc(3 << 20, 'a;b\n') } },
  });
  expect(big.status()).toBe(413);
  const legacy = await page.request.post('/api/v1/legacy-imports', {
    headers: API_HEADERS,
    multipart: {
      manifest: '{"version":1,"files":[]}',
      file: { name: 'a.csv', mimeType: 'text/csv', buffer: Buffer.from('a;b\n') },
    },
  });
  expect(legacy.status()).toBe(403);
  await page.goto('/signup');
  await expect(page.getByRole('heading', { name: 'Sign-up is turned off' })).toBeVisible();

  // a second visitor (another browser) gets a separate demo year
  const other = await browser.newContext();
  const p2 = await other.newPage();
  await p2.goto('/login');
  await p2.getByRole('button', { name: 'Try the demo' }).click();
  await expect(p2.getByText('Demo — your data is deleted after 24 hours.')).toBeVisible();
  const theirs = (await (await p2.request.get('/api/v1/accounts', { headers: API_HEADERS })).json()) as {
    id: string;
  }[];
  expect(theirs).toHaveLength(1);
  expect(theirs[0]?.id).not.toBe(mine[0]?.id);
  await other.close();
});
