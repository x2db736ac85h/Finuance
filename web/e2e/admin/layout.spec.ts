// One look for every form (Phase 20, 20.15; found at the gate): on every Settings and Administration
// page, and in their forms, inputs and selects are one height, the fields of a row line up on their
// bottom edge, the buttons of an action row are one height, and both sections put their content in
// the same column. Runs as the administrator (in the instance project) and changes nothing.

import { expect, test, type Page } from '@playwright/test';

const ADMIN_EMAIL = process.env['E2E_ADMIN_EMAIL'] ?? 'admin@example.test';
const ADMIN_PASSWORD = process.env['E2E_ADMIN_PASSWORD'] ?? 'e2e admin password long enough';

const SETTINGS = [
  'general',
  'appearance',
  'profile',
  'sessions',
  'tokens',
  'activity',
  'friends',
  'categories',
  'rules',
  'data',
];
const ADMIN = ['users', 'instance', 'themes', 'assistant', 'usage', 'developer'];

type Layout = { body: { x: number; width: number }; fields: number; rows: number; problems: string[] };

/** Measures the page; every problem names the element so a failure says where to look. */
async function measure(page: Page): Promise<Layout> {
  return page.evaluate(() => {
    const shown = (e: Element) => {
      const r = e.getBoundingClientRect();
      return r.width > 0 && r.height > 0;
    };
    const name = (e: Element) =>
      (e.getAttribute('aria-label') ?? e.closest('label')?.textContent ?? e.textContent).trim().slice(0, 40);
    const problems: string[] = [];
    const scope = '.set-body, .modal-card';
    // every input and select: one height (text areas grow with their text)
    const fields = [...document.querySelectorAll(`:is(${scope}) :is(.inp, .sel)`)].filter(
      (e) => shown(e) && e.tagName !== 'TEXTAREA',
    );
    const h0 = fields[0]?.getBoundingClientRect().height ?? 0;
    for (const f of fields) {
      const h = f.getBoundingClientRect().height;
      if (Math.abs(h - h0) > 1) problems.push(`field "${name(f)}" is ${h.toFixed(1)} px high, not ${h0.toFixed(1)}`);
    }
    // a form row: the controls of its columns share their bottom edge
    for (const row of document.querySelectorAll('.form-row')) {
      if (!shown(row)) continue;
      const bottoms = [...row.children]
        .map((c) => (c.matches('.btn, .inp, .sel') ? c : [...c.querySelectorAll('.inp, .sel, .btn')].at(-1)))
        .filter((c): c is Element => !!c && shown(c))
        .map((c) => c.getBoundingClientRect().bottom);
      if (bottoms.length > 1 && Math.max(...bottoms) - Math.min(...bottoms) > 1)
        problems.push(`row "${name(row)}": bottoms ${bottoms.map((b) => b.toFixed(0)).join(', ')}`);
    }
    // an action row (not inside a list row): its buttons are one height
    for (const row of document.querySelectorAll(`:is(${scope}) .set-actions`)) {
      if (!shown(row) || row.closest('li')) continue;
      const hs = [...row.querySelectorAll(':scope > .btn')].filter(shown).map((b) => b.getBoundingClientRect().height);
      if (hs.length > 1 && Math.max(...hs) - Math.min(...hs) > 1)
        problems.push(`buttons of "${name(row)}": ${hs.map((h) => h.toFixed(1)).join(', ')} px`);
    }
    const body = document.querySelector('.set-body')?.getBoundingClientRect();
    return {
      body: { x: Math.round(body?.x ?? 0), width: Math.round(body?.width ?? 0) },
      fields: fields.length,
      rows: [...document.querySelectorAll('.form-row')].filter(shown).length,
      problems,
    };
  });
}

test('settings and administration share one layout, and their forms line up', async ({ page }) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Email' }).fill(ADMIN_EMAIL);
  await page.getByLabel('Password').fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);

  const seen: Record<string, Layout> = {};
  const look = async (where: string) => {
    seen[where] = await measure(page);
  };
  for (const s of SETTINGS) {
    await page.goto(`/settings/${s}`);
    await expect(page.locator('.set-body h1')).toBeVisible();
    await look(`settings/${s}`);
  }
  for (const s of ADMIN) {
    await page.goto(`/admin/${s}`);
    await expect(page.locator('.set-body h1')).toBeVisible();
    await look(`admin/${s}`);
  }

  // the forms: a new connection, a model's tuning and an article (Assistant), a new account (Users)
  await page.goto('/admin/assistant');
  await page.getByRole('button', { name: 'Add a connection' }).click();
  await expect(page.getByRole('form', { name: 'Connection' })).toBeVisible();
  await look('admin/assistant: new connection');
  await page.getByRole('form', { name: 'Connection' }).getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Tune a model' }).click();
  await expect(page.getByRole('form', { name: 'Tuning' })).toBeVisible();
  await look('admin/assistant: tuning');
  await page.getByRole('form', { name: 'Tuning' }).getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'New article' }).click();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toBeVisible();
  await look('admin/assistant: new article');
  await page.goto('/admin/users');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await look('admin/users: new account');

  // the measurements saw the forms (the connection form has 9 fields, 3 of them in two rows)
  expect(seen['admin/assistant: new connection']?.fields).toBeGreaterThanOrEqual(8);
  expect(seen['admin/assistant: new connection']?.rows).toBeGreaterThanOrEqual(2);
  expect(seen['admin/assistant: tuning']?.rows).toBeGreaterThanOrEqual(3);
  const problems = Object.entries(seen).flatMap(([where, l]) => l.problems.map((p) => `${where}: ${p}`));
  expect(problems).toEqual([]);
  // one content column for both sections
  const columns = new Set(Object.values(seen).map((l) => `${String(l.body.x)}/${String(l.body.width)}`));
  expect(
    [...columns],
    JSON.stringify(Object.fromEntries(Object.entries(seen).map(([w, l]) => [w, l.body]))),
  ).toHaveLength(1);
});
