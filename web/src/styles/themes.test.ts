import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { afterEach, describe, expect, it, vi } from 'vitest';

import { resolveLook, watchTheme } from './theme';
import {
  BUILTIN_THEMES,
  THEME_KEYS,
  completeTokens,
  contrast,
  contrastReport,
  type Mode,
  type ThemeTokens,
} from './themes';

// Vitest runs with css: false, so the stylesheet is read as a file.
const css = readFileSync(resolve(process.cwd(), 'src/styles/tokens.css'), 'utf8');

/** The custom properties of the block whose selector is exactly sel. */
function block(sel: string): Record<string, string> {
  const start = css.indexOf(`${sel} {`);
  expect(start, sel).toBeGreaterThanOrEqual(0);
  const body = css.slice(start, css.indexOf('}', start));
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) out[m[1] ?? ''] = (m[2] ?? '').trim();
  return out;
}

function cssTokens(id: string, mode: Mode): Record<string, string> {
  return id === 'indigo' ? block(`[data-theme='${mode}']`) : block(`[data-theme-id='${id}'][data-theme='${mode}']`);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('built-in themes', () => {
  it('are Indigo, Fjord, Grove and Ember, each complete in both variants', () => {
    expect(BUILTIN_THEMES.map((t) => t.id)).toEqual(['indigo', 'fjord', 'grove', 'ember']);
    for (const t of BUILTIN_THEMES) expect(completeTokens(t.tokens), t.id).toBe(true);
  });

  it.each(BUILTIN_THEMES.map((t) => [t.id, t] as const))('%s reaches WCAG AA in every pair', (_, t) => {
    const fails = contrastReport(t.tokens).filter((r) => !r.ok);
    expect(fails).toEqual([]);
  });

  it.each(BUILTIN_THEMES.map((t) => [t.id, t] as const))('%s has the same values in tokens.css', (id, t) => {
    for (const mode of ['dark', 'light'] as const) {
      const fromCss = cssTokens(id, mode);
      for (const k of THEME_KEYS)
        expect(`${mode} --${k}: ${fromCss[k] ?? ''}`).toBe(`${mode} --${k}: ${t.tokens[mode][k] ?? ''}`);
    }
  });
});

describe('contrast', () => {
  it('matches known WCAG values', () => {
    expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 2);
    expect(contrast('#767676', '#ffffff')).toBeCloseTo(4.54, 2);
    expect(contrast('#6d5ce7', '#ffffff')).toBeCloseTo(4.84, 2);
  });

  it('reports a failing pair with its ratio', () => {
    const t: ThemeTokens = structuredClone(BUILTIN_THEMES[0]?.tokens ?? { dark: {}, light: {} });
    t.light['muted'] = '#b0b0b0';
    const fails = contrastReport(t).filter((r) => !r.ok);
    expect(fails.map((f) => `${f.mode} ${f.fg}/${f.bg}`)).toEqual([
      'light muted/bg',
      'light muted/panel',
      'light muted/elev',
    ]);
    expect(fails[0]?.ratio).toBeLessThan(4.5);
  });
});

describe('look', () => {
  const custom = { id: 'sea', tokens: BUILTIN_THEMES[1]?.tokens ?? { dark: {}, light: {} } };

  it('falls back from a retired theme to the instance default, then to Indigo', () => {
    expect(resolveLook('ember', [], 'fjord')).toEqual({ id: 'ember' });
    expect(resolveLook('gone', [], 'fjord')).toEqual({ id: 'fjord' });
    expect(resolveLook('gone', [], 'gone-too')).toEqual({ id: 'indigo' });
    expect(resolveLook('', [custom], 'sea')).toEqual({ id: 'sea', custom: custom.tokens });
  });

  it('sets data-theme-id, and an approved theme’s colours through the CSSOM', () => {
    vi.stubGlobal('matchMedia', () => ({
      matches: false,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
    const root = document.createElement('html');
    watchTheme('light', root, { id: 'grove' });
    expect(root.dataset['themeId']).toBe('grove');
    expect(root.style.getPropertyValue('--bg')).toBe('');
    watchTheme('dark', root, { id: 'sea', custom: custom.tokens });
    expect(root.dataset['themeId']).toBe('indigo');
    expect(root.style.getPropertyValue('--bg')).toBe(custom.tokens.dark['bg']);
    watchTheme('dark', root, { id: 'indigo' });
    expect(root.style.getPropertyValue('--bg')).toBe('');
  });
});
