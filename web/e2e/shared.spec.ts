// Shared groups (Phase 19, ADR 0028), two people end to end: friends by code, Alex shares a trip
// (a personal group with a cash expense), Bo joins and adds an expense, the balances say who owes
// whom, Bo settles up, and neither can see the other's accounts. Then axe on the Shared tab.
import { expect, test, type Browser, type Page } from '@playwright/test';

import { check } from './axe';
import { API_HEADERS, signUp } from './helpers';

async function person(browser: Browser, name: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await signUp(page);
  await page.goto('/settings/profile');
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('button', { name: 'Save name' }).click();
  await expect(page.getByText('Saved your name')).toBeVisible();
  return page;
}

async function befriend(a: Page, b: Page) {
  await a.goto('/settings/friends');
  await a.getByRole('button', { name: 'Show my code' }).click();
  const code = (await a.getByLabel('Your friend code').textContent()) ?? '';
  await b.goto('/settings/friends');
  await b.getByLabel('Their code').fill(code);
  await b.getByRole('button', { name: 'Add friend' }).click();
  await expect(b.getByRole('list', { name: 'Friends' })).toContainText(/Alex|Bo/);
}

test('two friends share a trip, add expenses and settle up', async ({ browser }) => {
  test.setTimeout(120_000);
  const alex = await person(browser, 'Alex');
  const bo = await person(browser, 'Bo');
  await befriend(alex, bo);

  // Alex's trip: a personal group with a cash expense in it
  const grp = await alex.request.post('/api/v1/groups', { headers: API_HEADERS, data: { name: 'Kraków' } });
  expect(grp.status()).toBe(201);
  const groupId = ((await grp.json()) as { id: string }).id;
  const wallet = await alex.request.post('/api/v1/accounts', {
    headers: API_HEADERS,
    data: { name: 'Portfel Alexa', kind: 'cash' },
  });
  expect(wallet.status()).toBe(201);
  const walletId = ((await wallet.json()) as { id: string }).id;
  const tx = await alex.request.post('/api/v1/transactions', {
    headers: API_HEADERS,
    data: { accountId: walletId, bookedOn: '2026-06-02', amountMinor: -6000, payee: 'Obwarzanki', groupId },
  });
  expect(tx.status()).toBe(201);

  // Share… from the group menu, inviting Bo
  await alex.goto('/t/all');
  await alex.getByRole('button', { name: 'Groups', exact: true }).click();
  await alex.getByRole('button', { name: 'Share Kraków…' }).click();
  const share = alex.getByRole('dialog', { name: 'Share Kraków' });
  await share.getByRole('checkbox', { name: 'Bo' }).check();
  await share.getByRole('button', { name: 'Share' }).click();
  await expect(alex.getByRole('heading', { name: /Kraków/ })).toBeVisible();
  const url = alex.url();
  expect(url).toMatch(/\/s\/[0-9a-f-]{36}$/);
  await expect(alex.getByRole('region', { name: 'Expenses', exact: true })).toContainText('Obwarzanki');

  // Bo: the bell, then the invitation in the Shared tab
  await bo.goto('/t/all');
  await bo.getByRole('button', { name: /^Notifications, 1 unread/ }).click();
  await bo
    .getByRole('dialog', { name: 'Notifications' })
    .getByText(/Alex invited you to Kraków/)
    .click();
  const inv = bo.getByRole('region', { name: 'Invitation' });
  await expect(inv).toContainText('Alex invited you to Kraków');
  await expect(bo.getByRole('region', { name: 'Expenses', exact: true })).toHaveCount(0);
  await inv.getByRole('button', { name: 'Join' }).click();
  await expect(bo.getByRole('region', { name: 'Balances' })).toBeVisible();

  // Bo adds an expense, split equally
  await bo.getByRole('button', { name: 'Add expense' }).click();
  const add = bo.getByRole('dialog', { name: 'Add an expense' });
  await add.getByLabel(/Amount/).fill('30');
  await add.getByLabel('What for').fill('Muzeum');
  await add.getByLabel('Category').fill('Kultura');
  await add.getByRole('button', { name: 'Add' }).click();
  await expect(bo.getByRole('region', { name: 'Expenses', exact: true })).toContainText('Muzeum');

  // Alex paid 60, Bo 30, both split equally: Bo owes Alex 15
  const bal = bo.getByRole('region', { name: 'Balances' });
  await expect(bal.getByRole('list', { name: 'Who owes whom' })).toContainText(/You → Alex\s*15[.,]00/);
  await bal.getByRole('button', { name: 'Settle up' }).click();
  const settle = bo.getByRole('dialog', { name: 'Settle up' });
  await expect(settle.getByLabel(/Amount/)).toHaveValue(/^15[.,]00$/);
  await settle.getByRole('button', { name: 'Record payment' }).click();
  await expect(bal).toContainText('Everyone is settled up.');

  // Alex sees it too, and gets the notifications
  await alex.goto(url);
  await expect(alex.getByRole('region', { name: 'Balances' })).toContainText('Everyone is settled up.');
  await expect(alex.getByRole('region', { name: 'Expenses', exact: true })).toContainText('Muzeum');

  // neither sees the other's accounts: Bo's sidebar and API show only Bo's own
  await bo.goto('/t/all');
  await expect(bo.getByText('Portfel Alexa')).toHaveCount(0);
  const boAccounts = (await (await bo.request.get('/api/v1/accounts')).json()) as { name: string }[];
  expect(boAccounts.map((a) => a.name)).not.toContain('Portfel Alexa');
  const detail = await (await bo.request.get(`/api/v1/shared-groups/${url.split('/').at(-1) ?? ''}`)).text();
  expect(detail).not.toContain(walletId);
  expect(detail).not.toContain('@');

  // the timeline lists the trip once, as the shared group
  await alex.goto('/s');
  const timeline = alex.getByRole('region', { name: 'Timeline' });
  await expect(timeline.getByRole('button', { name: 'Kraków' })).toBeVisible();
  await expect(timeline.locator('tbody tr')).toHaveCount(1);

  // accessibility of the tab in both modes (the timeline, then the group page)
  await check(alex, 'shared timeline');
  await alex.goto(url);
  await expect(alex.getByRole('region', { name: 'Paid vs share' })).toBeVisible();
  await check(alex, 'shared group');
});
