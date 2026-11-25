// Two-step sign-in end to end (PLAN 23.7, ADR 0040): set it up in Settings → Security, sign out,
// sign in with the password and a code from the "authenticator app", then with a recovery code,
// and switch it off. The app's side is computed here from RFC 6238 (HMAC-SHA1, 30-second steps,
// six digits), as a real authenticator app does.
import { createHmac } from 'node:crypto';

import { expect, test, type Page } from '@playwright/test';

import { check } from './axe';
import { PASSWORD, signUp } from './helpers';

function base32Decode(text: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const ch of text.replace(/\s/g, '')) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0');
  const bytes = bits.match(/.{8}/g) ?? [];
  return Buffer.from(bytes.map((b) => Number.parseInt(b, 2)));
}

/** The code an authenticator app shows for a key, `stepsAhead` 30-second steps from now. */
function appCode(key: string, stepsAhead = 0): string {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000) + stepsAhead));
  const mac = createHmac('sha1', base32Decode(key)).update(counter).digest();
  const at = (mac[19] ?? 0) & 0x0f;
  return String((mac.readUInt32BE(at) & 0x7fffffff) % 1_000_000).padStart(6, '0');
}

async function signOut(page: Page) {
  await page.getByRole('button', { name: 'Account menu' }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
}

async function password(page: Page, email: string) {
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.getByLabel('Password').fill(PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

test('set up two-step sign-in, sign in with a code and with a recovery code, switch it off', async ({ page }) => {
  const email = await signUp(page);
  await page.goto('/settings/security');
  await page.getByRole('button', { name: 'Set up two-step sign-in' }).click();
  const form = page.getByRole('form', { name: 'Set up two-step sign-in' });
  // the QR code is there, dark on white, with room around it for a scanner
  const qr = form.getByRole('img', { name: 'QR code for your authenticator app' });
  await expect(qr).toBeVisible();
  expect(await qr.locator('rect').getAttribute('fill')).toBe('#fff');
  await check(page, 'two-step: setting up');

  const key = ((await form.getByLabel('The key for your authenticator app').textContent()) ?? '').replace(/\s/g, '');
  expect(key).toMatch(/^[A-Z2-7]{32}$/);
  // the link for the same device carries the same key
  expect(await form.getByRole('link', { name: 'Open it in your authenticator app' }).getAttribute('href')).toContain(
    `secret=${key}`,
  );
  await form.getByLabel('Code from your authenticator app').fill(appCode(key));
  await form.getByRole('button', { name: 'Switch it on' }).click();

  const codes = page.getByRole('status', { name: 'Recovery codes' });
  await expect(codes.getByRole('listitem')).toHaveCount(10);
  const recovery = (await codes.getByRole('listitem').first().textContent()) ?? '';
  expect(recovery).toMatch(/^[a-z2-7]{5}-[a-z2-7]{5}$/);
  const secondRecovery = (await codes.getByRole('listitem').nth(1).textContent()) ?? '';
  await check(page, 'two-step: recovery codes');
  await codes.getByRole('button', { name: 'I have saved them' }).click();
  await expect(page.getByText('10 recovery codes left')).toBeVisible();
  await expect(page.getByText(key)).toHaveCount(0); // the key is not shown again

  // the password alone signs nobody in
  await signOut(page);
  await password(page, email);
  await expect(page.getByRole('heading', { name: 'Enter your code' })).toBeVisible();
  await check(page, 'two-step: the code at sign-in');
  await page.goto('/transactions/all');
  await expect(page).toHaveURL(/\/login/); // still signed out
  await password(page, email);
  // a wrong code, then the app's (one step ahead: the code that confirmed the setup is spent)
  await page.getByRole('textbox', { name: 'Code' }).fill('000000');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toContainText(/code is not valid/);
  await page.getByRole('textbox', { name: 'Code' }).fill(appCode(key, 1));
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();

  // a recovery code signs in once, and the count goes down
  await signOut(page);
  await password(page, email);
  await page.getByRole('textbox', { name: 'Code' }).fill(recovery);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();
  await page.goto('/settings/security');
  await expect(page.getByText('9 recovery codes left')).toBeVisible();
  await check(page, 'two-step: on');
  await page.goto('/settings/activity');
  await expect(page.getByText('Signed in with a recovery code (9 left)')).toBeVisible();
  await expect(page.getByText('Switched two-step sign-in on')).toBeVisible();

  // switching it off needs the password and a code (here a recovery code: the app's codes for
  // this minute and the next are spent, as a code never works twice); signing in is one step again
  await page.goto('/settings/security');
  await page.getByRole('button', { name: 'Switch it off' }).click();
  const off = page.getByRole('form', { name: 'Switch two-step sign-in off' });
  await off.getByLabel('Password').fill(PASSWORD);
  await off.getByLabel('Code or recovery code').fill(secondRecovery);
  await off.getByRole('button', { name: 'Switch it off' }).click();
  await expect(page.getByRole('button', { name: 'Set up two-step sign-in' })).toBeVisible();
  await signOut(page);
  await password(page, email);
  await expect(page.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();
});
