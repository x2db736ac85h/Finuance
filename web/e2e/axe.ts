// axe for the E2E specs (PLAN task 14.4): serious and critical WCAG 2.1 A/AA violations fail,
// in both colour modes. `within` limits a check to one element, for a small dialog whose page
// behind was checked without it: axe measures the dimmed page through the backdrop.
import AxeBuilder from '@axe-core/playwright';
import { expect, type Page } from '@playwright/test';

export async function check(page: Page, name: string, within?: string) {
  for (const scheme of ['dark', 'light'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    // the theme switch fades colours; measure once every transition has ended
    await page.waitForFunction(() => document.getAnimations().length === 0);
    let axe = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
    if (within) axe = axe.include(within);
    const r = await axe.analyze();
    const bad = r.violations
      .filter((v) => v.impact === 'serious' || v.impact === 'critical')
      .map((v) => ({
        id: v.id,
        impact: v.impact,
        help: v.help,
        nodes: v.nodes.slice(0, 5).map((n) => n.target.join(' ')),
      }));
    expect(bad, `${name} (${scheme})`).toEqual([]);
  }
}
