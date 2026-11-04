// Shared E2E steps.
import { readFileSync } from 'node:fs';

import { expect, type Page } from '@playwright/test';

export const PASSWORD = 'e2e password long enough';

/** Signs up a fresh user and waits for the empty app. Returns the email. */
export async function signUp(page: Page): Promise<string> {
  const email = `e2e-${String(Date.now())}-${String(Math.floor(Math.random() * 1e6))}@example.test`;
  await page.goto('/signup');
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.getByLabel('Password').fill(PASSWORD);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();
  return email;
}

/** A fixture path, relative to web/ (where Playwright runs). */
export const fixture = (rel: string) => `../testdata/fixtures/${rel}`;

/** Opens files the way ⌘O and Browse do (the app's hidden file input). */
export async function openFiles(page: Page, ...rel: string[]): Promise<void> {
  await page.locator('input[type=file]').setInputFiles(rel.map(fixture));
}

export const API_HEADERS = { 'X-Finuance-Client': 'web' };

/**
 * Creates an account and imports fixture files into it through the API (faster than the import
 * window, which import.spec.ts covers). Waits until every import is committed.
 */
export async function importViaApi(
  page: Page,
  account: string,
  ...rel: string[]
): Promise<{ accountId: string; importIds: string[] }> {
  const acc = await page.request.post('/api/v1/accounts', { headers: API_HEADERS, data: { name: account } });
  expect(acc.status()).toBe(201);
  const accountId = ((await acc.json()) as { id: string }).id;
  return { accountId, importIds: await importInto(page, accountId, ...rel) };
}

/** A shared group with a few typed expenses (the signed-in user alone), for screenshots and axe. */
export async function sharedGroupViaApi(page: Page): Promise<string> {
  // sharing needs a display name (the members see it)
  const me = await page.request.patch('/api/v1/me', { headers: API_HEADERS, data: { displayName: 'Alex' } });
  expect(me.status()).toBe(200);
  const g = await page.request.post('/api/v1/shared-groups', {
    headers: API_HEADERS,
    data: { name: 'Mazury', kind: 'trip', startsOn: '2026-07-01', endsOn: '2026-07-05', budgetMinor: 300000 },
  });
  expect(g.status()).toBe(201);
  const id = ((await g.json()) as { id: string }).id;
  const rows: [string, number, string, string][] = [
    ['2026-07-01', 48000, 'Przystań Sztynort', 'Nocleg'],
    ['2026-07-02', 12450, 'Tawerna', 'Jedzenie'],
    ['2026-07-03', 6000, 'Wypożyczalnia kajaków', 'Atrakcje'],
    ['2026-07-04', 9800, 'Stacja paliw', 'Transport'],
  ];
  for (const [bookedOn, amountMinor, payee, category] of rows) {
    const r = await page.request.post(`/api/v1/shared-groups/${id}/expenses`, {
      headers: API_HEADERS,
      data: { bookedOn, amountMinor, payee, category },
    });
    expect(r.status()).toBe(201);
  }
  return `/s/${id}`;
}

/** Imports fixture files into an existing account and waits until they are committed. */
export async function importInto(page: Page, accountId: string, ...rel: string[]): Promise<string[]> {
  const importIds: string[] = [];
  for (const f of rel) {
    const up = await page.request.post('/api/v1/imports', {
      headers: API_HEADERS,
      multipart: {
        accountId,
        file: { name: f.split('/').at(-1) ?? f, mimeType: 'text/csv', buffer: readFileSync(fixture(f)) },
      },
    });
    expect(up.status()).toBe(201);
    const id = ((await up.json()) as { import: { id: string } }).import.id;
    expect((await page.request.post(`/api/v1/imports/${id}/commit`, { headers: API_HEADERS, data: {} })).status()).toBe(
      202,
    );
    await expect
      .poll(async () => ((await (await page.request.get(`/api/v1/imports/${id}`)).json()) as { status: string }).status)
      .toBe('committed');
    importIds.push(id);
  }
  return importIds;
}
