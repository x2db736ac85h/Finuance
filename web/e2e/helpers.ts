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
