// Shared E2E steps.
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
