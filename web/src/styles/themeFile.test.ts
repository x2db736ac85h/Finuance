import { describe, expect, it } from 'vitest';

import { myThemeId, parseMyThemes, parseThemeFile, readTokens, themeFileName, themeFileText } from './themeFile';
import { BUILTIN_THEMES, THEME_KEYS } from './themes';

const indigo = BUILTIN_THEMES.find((t) => t.id === 'indigo') ?? {
  id: '',
  name: '',
  description: '',
  tokens: { dark: {}, light: {} },
};

describe('a theme as a file (Phase 27)', () => {
  it('writes every built-in theme and reads it back the same', () => {
    for (const t of BUILTIN_THEMES) {
      const r = parseThemeFile(themeFileText(t.name, t.tokens));
      expect(r).toEqual({ ok: true, name: t.name, tokens: t.tokens, lowContrast: 0 });
    }
    expect(themeFileName('Deep Sea 2')).toBe('deep-sea-2.finuance-theme.json');
    expect(themeFileName('★')).toBe('theme.finuance-theme.json');
  });

  it('refuses what is not a theme, saying why', () => {
    const problems = (text: string) => {
      const r = parseThemeFile(text);
      return r.ok ? [] : r.problems;
    };
    expect(problems('not json')).toEqual(['The file is not JSON.']);
    expect(problems('{"format":"other"}')[0]).toContain('not a Finuance theme file');
    expect(problems(JSON.stringify({ format: 'finuance-theme', version: 2 }))[0]).toContain('version');
    expect(
      problems(JSON.stringify({ format: 'finuance-theme', version: 1, name: ' ', tokens: indigo.tokens })),
    ).toEqual(['The theme has no name.']);
    expect(problems('x'.repeat(20_000))).toEqual(['The file is too large to be a theme.']);
    const file = (tokens: unknown) => JSON.stringify({ format: 'finuance-theme', version: 1, name: 'T', tokens });
    expect(problems(file({ dark: indigo.tokens.dark }))).toEqual(['The light variant is missing.']);
    // anything but a hex colour is refused: no other CSS can come in through a theme
    const bad = { ...indigo.tokens, dark: { ...indigo.tokens.dark, bg: 'url(https://example.invalid/x)' } };
    expect(problems(file(bad))).toEqual(['dark: “bg” must be a colour such as #13141a.']);
    const extra = { ...indigo.tokens, light: { ...indigo.tokens.light, 'font-family': '#000000' } };
    expect(problems(file(extra))).toEqual(['light: “font-family” is not a colour of a theme.']);
    const noBg = Object.fromEntries(Object.entries(indigo.tokens.dark).filter(([k]) => k !== 'bg'));
    expect(problems(file({ dark: noBg, light: indigo.tokens.light }))).toEqual(['dark: “bg” is missing.']);
  });

  it('takes upper-case colours and counts the pairs that are hard to read', () => {
    const upper = {
      ...indigo.tokens,
      dark: { ...indigo.tokens.dark, bg: (indigo.tokens.dark['bg'] ?? '').toUpperCase() },
    };
    const r = readTokens(upper);
    expect('tokens' in r && r.tokens.dark['bg']).toBe(indigo.tokens.dark['bg']);
    // grey text on a grey surface: allowed (it is the person's own theme), but said
    const dim = { ...indigo.tokens, dark: { ...indigo.tokens.dark, text: '#333333', muted: '#333333' } };
    const low = parseThemeFile(JSON.stringify({ format: 'finuance-theme', version: 1, name: 'Dim', tokens: dim }));
    expect(low.ok && low.lowContrast).toBeGreaterThan(0);
  });

  it('gives imported themes ids of their own and drops broken ones from a stored document', () => {
    expect(myThemeId('Deep Sea', [])).toBe('my-deep-sea');
    expect(myThemeId('Deep Sea', ['my-deep-sea', 'my-deep-sea-2'])).toBe('my-deep-sea-3');
    const good = { id: 'my-a', name: 'A', tokens: indigo.tokens };
    expect(
      parseMyThemes([
        good,
        { ...good }, // the same id twice
        { id: 'indigo', name: 'Not mine', tokens: indigo.tokens }, // not an imported theme's id
        { id: 'my-b', name: 'B', tokens: { dark: { bg: 'red' }, light: {} } },
        'nonsense',
      ]),
    ).toEqual([good]);
    expect(parseMyThemes('x')).toEqual([]);
    expect(parseMyThemes(Array.from({ length: 20 }, (_, i) => ({ ...good, id: `my-${String(i)}` })))).toHaveLength(8);
    expect(Object.keys(good.tokens.dark).sort()).toEqual([...THEME_KEYS].sort());
  });
});
