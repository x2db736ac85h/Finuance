// Settings → Activity (PLAN 17.6): what the account did, recorded by the server as it happens —
// sign-out, a failed and a successful sign-in, an import committed by the worker, an export —
// newest first, with the address and browser of each.
import { expect, test } from '@playwright/test';

import { API_HEADERS, importViaApi, PASSWORD, signUp } from './helpers';

test('sign-ins, an import and an export show up on the Activity page', async ({ page }) => {
  const email = await signUp(page);
  await page.getByRole('button', { name: 'Account menu' }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.getByLabel('Password').fill('not the password at all');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByText(/invalid email or password/i)).toBeVisible();
  await page.getByLabel('Password').fill(PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();

  await importViaApi(page, 'Demo', 'synthetic/overlap/A_jan-mar.csv');
  const exp = await page.request.get('/api/v1/me/export', { headers: API_HEADERS });
  expect(exp.status()).toBe(200);

  await page.goto('/settings/activity');
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
  const items = page.getByRole('list', { name: 'Activity' }).getByRole('listitem');
  await expect(items).toHaveCount(5);
  await expect(items.nth(0)).toContainText('Downloaded all data');
  await expect(items.nth(1)).toContainText(/Imported A_jan-mar\.csv: \d+ new transactions/);
  await expect(items.nth(1)).toContainText('in the background');
  await expect(items.nth(2)).toContainText('Signed in');
  await expect(items.nth(2)).toContainText(/Chrome on \w+/);
  await expect(items.nth(3)).toContainText('Failed sign-in: wrong password');
  await expect(items.nth(3)).toContainText('attention');
  await expect(items.nth(4)).toContainText('Signed out');
});
