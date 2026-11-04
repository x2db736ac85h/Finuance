// Friends (Phase 19, ADR 0027): one person shows a code, the other enters it; both see each other
// by display name only, the code's owner is notified, and either can remove the friendship.
import { expect, test, type Browser, type Page } from '@playwright/test';

import { signUp } from './helpers';

async function person(browser: Browser, name: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  const email = await signUp(page);
  await page.goto('/settings/profile');
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('button', { name: 'Save name' }).click();
  await expect(page.getByText('Saved your name')).toBeVisible();
  expect(email).toContain('@');
  return page;
}

test('two people become friends by code, and one removes the other', async ({ browser }) => {
  const alex = await person(browser, 'Alex');
  const bo = await person(browser, 'Bo');

  await alex.goto('/settings/friends');
  await alex.getByRole('button', { name: 'Show my code' }).click();
  const code = (await alex.getByLabel('Your friend code').textContent()) ?? '';
  expect(code).toMatch(/^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$/);
  await expect(alex.getByRole('timer')).toContainText('left');

  await bo.goto('/settings/friends');
  await bo.getByLabel('Their code').fill(code.toLowerCase().replace('-', ' '));
  await bo.getByRole('button', { name: 'Add friend' }).click();
  await expect(bo.getByText('You and Alex are friends now')).toBeVisible();
  await expect(bo.getByRole('list', { name: 'Friends' })).toContainText('Alex');
  // the code works once
  await bo.getByLabel('Their code').fill(code);
  await bo.getByRole('button', { name: 'Add friend' }).click();
  await expect(bo.getByRole('alert')).toContainText(/wrong or has expired/);

  // Alex's bell
  await alex.goto('/t/all');
  await alex.getByRole('button', { name: 'Notifications, 1 unread' }).click();
  await expect(alex.getByRole('dialog', { name: 'Notifications' })).toContainText('Bo entered your code');
  await alex.goto('/settings/friends');
  const list = alex.getByRole('list', { name: 'Friends' });
  await expect(list).toContainText('Bo');
  await expect(list).not.toContainText('@');

  await list.getByRole('button', { name: 'Remove' }).click();
  await alex.getByRole('dialog', { name: 'Remove Bo?' }).getByRole('button', { name: 'Remove' }).click();
  await expect(alex.getByText('No friends yet.')).toBeVisible();
  await bo.goto('/t/all');
  await expect(bo.getByRole('button', { name: 'Notifications, 1 unread' })).toBeVisible();
});
