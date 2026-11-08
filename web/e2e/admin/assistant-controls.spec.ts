// Administration → Assistant, the controls (Phase 20 follow-ups): an administrator loads and
// unloads the mock's models (it answers like Ollama), sets its model for photos, decides that the
// administrator picks the model, and switches the connection off; a person sees the result in the
// assistant panel. The connection, its controls and who picks are put back at the end.
import { expect, test, type Page } from '@playwright/test';

import { ADMIN_EMAIL, ADMIN_PASSWORD, setModelChoice, signUp } from '../helpers';

async function signInAdmin(page: Page): Promise<void> {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Email' }).fill(ADMIN_EMAIL);
  await page.getByLabel('Password').fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

async function openSettings(user: Page) {
  await user.goto('/t/all');
  await expect(user.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();
  const panel = user.getByRole('complementary', { name: 'Assistant' });
  if (!(await panel.isVisible())) await user.locator('.aibtn').click();
  await panel.getByRole('button', { name: 'Assistant settings' }).click();
  return panel;
}

test('administrators load and unload models, pick the model and switch a connection off', async ({
  page,
  browser,
  playwright,
}) => {
  const base = test.info().project.use.baseURL ?? '';
  await signInAdmin(page);
  await page.goto('/admin/assistant');
  const row = page.locator('.set-list li').filter({ hasText: 'ai-mock' }).first();
  await row.getByRole('button', { name: 'Models' }).click();
  const form = page.getByRole('form', { name: 'Models and use of ai-mock' });
  await expect(form.getByText(/Connected to Ollama · 4 models/)).toBeVisible();

  // load and unload a model on the server
  const vl = form.locator('li').filter({ hasText: 'mock-vl' });
  const unload = vl.getByRole('button', { name: 'Unload', exact: true });
  // the assistant spec's receipt question may have left it loaded (its unload time moves after a run)
  if (await unload.isVisible()) {
    await unload.click();
    await expect(vl.getByText('not loaded')).toBeVisible();
  }
  await vl.getByRole('button', { name: 'Load', exact: true }).click();
  await expect(vl.getByText('loaded', { exact: true })).toBeVisible();
  await unload.click();
  await expect(vl.getByText('not loaded')).toBeVisible();

  // a model for photos, and the administrator picks
  await form.getByLabel('Model for photos').selectOption('mock-vl');
  await form.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('Saved ai-mock')).toBeVisible();
  const was = await setModelChoice(playwright, base, (a) => ({ ...a, modelChoice: 'admin', defaultConnection: '' }));

  // a person: the connection and model are set; Test says it is reachable; photos are offered
  const ctx = await browser.newContext();
  const user = await ctx.newPage();
  await signUp(user);
  let panel = await openSettings(user);
  await expect(panel.getByText('The connection and model are set by your administrator.')).toBeVisible();
  await expect(panel.getByLabel('Model', { exact: true })).toBeHidden();
  await panel.getByRole('button', { name: 'Test', exact: true }).click();
  await expect(panel.getByText(/Connected to ai-mock · 4 models/)).toBeVisible();
  await panel.getByRole('button', { name: 'Done' }).click();
  await expect(panel.getByRole('button', { name: 'Attach a photo of a receipt' })).toBeVisible();

  // switched off: the assistant isn't available for people any more
  await page.reload();
  await page
    .locator('.set-list li')
    .filter({ hasText: 'ai-mock' })
    .first()
    .getByRole('button', { name: 'Models' })
    .click();
  const again = page.getByRole('form', { name: 'Models and use of ai-mock' });
  await again.getByRole('checkbox', { name: /^On/ }).uncheck();
  await again.getByRole('button', { name: 'Save' }).click();
  await expect(
    page.locator('.set-list li').filter({ hasText: 'ai-mock' }).getByText('off', { exact: true }),
  ).toBeVisible();
  await user.reload();
  await expect(user.getByRole('heading', { name: 'Drop CSV files here' })).toBeVisible();
  panel = user.getByRole('complementary', { name: 'Assistant' });
  if (!(await panel.isVisible())) await user.locator('.aibtn').click();
  await expect(panel.getByRole('heading', { name: 'The assistant isn’t available' })).toBeVisible();

  // put everything back: on, no model for photos, who picks as it was
  await page.reload();
  await page
    .locator('.set-list li')
    .filter({ hasText: 'ai-mock' })
    .first()
    .getByRole('button', { name: 'Models' })
    .click();
  const back = page.getByRole('form', { name: 'Models and use of ai-mock' });
  await back.getByRole('checkbox', { name: /^On/ }).check();
  await back.getByLabel('Model for photos').selectOption('');
  await back.getByRole('button', { name: 'Save' }).click();
  await expect(
    page.locator('.set-list li').filter({ hasText: 'ai-mock' }).getByText('off', { exact: true }),
  ).toBeHidden();
  await setModelChoice(playwright, base, () => was);
  await ctx.close();
});
