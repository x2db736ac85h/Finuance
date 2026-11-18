// axe for the E2E specs (PLAN task 14.4): serious and critical WCAG 2.1 A/AA violations fail,
// in both colour modes. `within` limits a check to one element, for a small dialog whose page
// behind was checked without it: axe measures the dimmed page through the backdrop.
import AxeBuilder from '@axe-core/playwright';
import { expect, type Page } from '@playwright/test';

export async function check(page: Page, name: string, within?: string) {
  for (const scheme of ['dark', 'light'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    // The theme switch fades colours, and a fade can start a frame or two after the switch (seen in
    // CI on 2026-11-18: axe measured a toast half-way between the dark and the light colours,
    // #7f7f83 on #8c8c8e, although no animation was running when the wait returned). So wait for
    // five consecutive frames without any animation, not for one instant without one.
    await page.evaluate(
      () =>
        new Promise<void>((resolve) => {
          let quiet = 0;
          const tick = () => {
            quiet = document.getAnimations().length === 0 ? quiet + 1 : 0;
            if (quiet >= 5) resolve();
            else requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
    );
    let axe = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
    if (within) axe = axe.include(within);
    const r = await axe.analyze();
    const serious = r.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
    const bad = [];
    for (const v of serious) {
      const nodes = v.nodes.slice(0, 5);
      // what the page looked like for each failing element, read right after the analysis: a
      // failure then says whether the element was half-way through a fade or in the wrong theme
      const seen = await page.evaluate(
        (selectors) =>
          selectors.map((sel) => {
            const el = document.querySelector(sel);
            const cs = el ? getComputedStyle(el) : undefined;
            return {
              class: el?.getAttribute('class') ?? null,
              text: el?.textContent.trim().slice(0, 60) ?? null,
              opacity: cs?.opacity ?? null,
              color: cs?.color ?? null,
              background: cs?.backgroundColor ?? null,
              theme: document.documentElement.dataset['theme'] ?? null,
              themeId: document.documentElement.dataset['themeId'] ?? null,
              animationsRunning: document.getAnimations().length,
            };
          }),
        nodes.map((n) => n.target.join(' ')),
      );
      bad.push({
        id: v.id,
        impact: v.impact,
        help: v.help,
        nodes: nodes.map((n) => n.target.join(' ')),
        // what axe measured (colour-contrast: foreground, background, ratio, required ratio)
        measured: nodes.map((n) => [...n.any, ...n.all, ...n.none].map((c) => c.data as unknown).filter(Boolean)),
        seen,
      });
    }
    expect(bad, `${name} (${scheme})`).toEqual([]);
  }
}
