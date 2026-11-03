// Administration end to end (PLAN Phase 19, 19.3 and 19.5): an administrator (created with
// `finuance admin create-user -admin` by make test-e2e) creates an account with a one-time
// password, limits it, changes where new users start and approves a colour theme; the new user
// changes the password, starts from those defaults, hits the limit and picks the theme. A non-admin
// can't see /admin. This project runs after every other spec (playwright.config.ts) and puts the
// instance settings back.

import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

import { check } from '../axe';
import { API_HEADERS } from '../helpers';

const ADMIN_EMAIL = process.env['E2E_ADMIN_EMAIL'] ?? 'admin@example.test';
const ADMIN_PASSWORD = process.env['E2E_ADMIN_PASSWORD'] ?? 'e2e admin password long enough';

test.describe.configure({ mode: 'serial' });

async function signIn(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

/** Puts the instance settings back to "everything at its default" and retires the test theme. */
async function restore(request: APIRequestContext): Promise<void> {
  const login = await request.post('/api/v1/auth/login', {
    headers: API_HEADERS,
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  });
  expect(login.ok()).toBe(true);
  const settings = (await (await request.get('/api/v1/admin/settings')).json()) as { version: number };
  await request.put('/api/v1/admin/settings', {
    headers: API_HEADERS,
    data: { version: settings.version, defaults: {}, features: {}, limits: {} },
  });
  const themes = (await (await request.get('/api/v1/admin/themes')).json()) as {
    id: string;
    version: number;
    status: string;
  }[];
  for (const t of themes.filter((x) => x.status === 'approved')) {
    await request.put(`/api/v1/admin/themes/${t.id}/status`, {
      headers: API_HEADERS,
      data: { version: t.version, status: 'retired' },
    });
  }
}

test.afterAll(async ({ request }) => {
  await restore(request);
});

test('an administrator runs the instance; a new user follows it', async ({ page, browser }) => {
  const userEmail = `e2e-admin-${String(Date.now())}@example.test`;
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Account menu' }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();

  // a new account with a one-time password, limited to one account
  await page.getByRole('button', { name: 'Create account' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('E-mail').fill(userEmail);
  await dialog.getByRole('button', { name: 'Create account' }).click();
  const oneTime = (await dialog.locator('code').textContent()) ?? '';
  expect(oneTime).toMatch(/^[a-z2-9]{4}(-[a-z2-9]{4}){3}$/);
  await dialog.getByRole('button', { name: 'Done' }).click();
  const detail = page.getByLabel(`Account ${userEmail}`);
  await detail.getByLabel(/Accounts/).fill('1');
  await detail.getByRole('button', { name: 'Save limits' }).click();
  await expect(page.getByText(`Saved the limits of ${userEmail}`)).toBeVisible();

  // new users start without the daily spending chart
  await page.getByRole('link', { name: 'Defaults and features' }).click();
  await page.getByRole('checkbox', { name: 'Daily spending' }).uncheck();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('Saved the instance settings')).toBeVisible();

  // a colour theme from Fjord, approved for everyone
  await page.getByRole('link', { name: 'Colour themes' }).click();
  await page.getByLabel('Start from').selectOption('fjord');
  await page.getByLabel('Name').fill(`E2E Sea ${String(Date.now())}`);
  await page.getByRole('button', { name: 'Create draft' }).click();
  await page.getByRole('button', { name: 'Approve' }).click();
  await expect(page.getByText(/everyone can pick it now/)).toBeVisible();

  // the new user: password first, then the instance's defaults
  const ctx = await browser.newContext();
  const user = await ctx.newPage();
  await signIn(user, userEmail, oneTime);
  await expect(user.getByRole('heading', { name: 'Choose a new password' })).toBeVisible();
  await user.getByLabel('One-time password').fill(oneTime);
  await user.getByLabel('New password', { exact: true }).fill('my own password now');
  await user.getByLabel('Repeat the new password').fill('my own password now');
  await user.getByRole('button', { name: 'Save and continue' }).click();
  await user.getByRole('button', { name: 'Load demo data' }).click();
  await expect(user.getByRole('region', { name: 'Summary' })).toBeVisible();
  await expect(user.getByRole('region', { name: 'Daily spending' })).toHaveCount(0);

  // the demo account was the one account allowed
  const second = await user.request.post('/api/v1/accounts', {
    headers: API_HEADERS,
    data: { name: 'Second', currency: 'PLN' },
  });
  expect(second.status()).toBe(422);
  expect(((await second.json()) as { code: string }).code).toBe('limit_reached');

  // the approved theme is offered and applies its colours
  await user.goto('/settings/appearance');
  const themes = user.getByRole('radiogroup', { name: 'Colour theme' });
  await themes.getByRole('radio', { name: /E2E Sea/ }).click();
  await expect.poll(() => user.evaluate(() => document.documentElement.style.getPropertyValue('--bg'))).not.toBe('');

  // and a non-admin has no administration
  expect((await user.request.get('/api/v1/admin/users')).status()).toBe(404);
  await user.goto('/admin/users');
  await expect(user).not.toHaveURL(/\/admin/);
  await ctx.close();
});

test('the administration pages are accessible in both modes', async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  for (const [path, heading] of [
    ['/admin/users', 'Users'],
    ['/admin/instance', 'Defaults and features'],
    ['/admin/themes', 'Colour themes'],
    ['/admin/usage', 'Usage'],
  ] as const) {
    await page.goto(path);
    await expect(page.getByRole('heading', { name: heading })).toBeVisible();
    await check(page, `admin ${heading}`);
  }
});
