// Colour themes (PLAN Phase 19, ADR 0026). Four built-in themes, each with a dark and a light
// variant; administrators can approve more (colours only). The token names and the text/surface
// pairs that must reach WCAG AA come from theme-contract.json, which the server checks too
// (backend/internal/instance/theme.go), so the editor's report and the server always agree.
// The built-in values are also in tokens.css, as CSS (themes.test.ts keeps both equal).

import contract from './theme-contract.json';

export type Mode = 'dark' | 'light';
export type ThemeColours = Record<string, string>;
export type ThemeTokens = Record<Mode, ThemeColours>;

export type BuiltinTheme = {
  id: string;
  name: string;
  description: string;
  tokens: ThemeTokens;
};

/** The tokens a theme sets (CSS custom properties without the leading --). */
export const THEME_KEYS: readonly string[] = contract.keys;
/** Text colours and the surfaces they are drawn on; "a/b" is the translucent a over b. */
export const CONTRAST_PAIRS: readonly { fg: string; bg: readonly string[] }[] = contract.pairs;
export const MIN_CONTRAST: number = contract.minRatio;
export const DEFAULT_THEME_ID = 'indigo';

export const BUILTIN_THEMES: readonly BuiltinTheme[] = [
  {
    id: 'indigo',
    name: 'Indigo',
    description: 'The original look: violet accents on neutral grey.',
    tokens: {
      dark: {
        bg: '#1e1e1e',
        panel: '#232325',
        elev: '#26262a',
        input: '#141416',
        border: '#2b2b2f',
        text: '#d6d6d9',
        muted: '#919198',
        accent: '#9789f7',
        'accent-solid': '#6d5ce7',
        'on-accent': '#ffffff',
        'accent-soft': '#8b7cf629',
        sel: '#8b7cf62b',
        pos: '#81c995',
        neg: '#f28b82',
        'logo-a': '#a191ff',
        'logo-b': '#6d5ce7',
      },
      light: {
        bg: '#ffffff',
        panel: '#fafafb',
        elev: '#ffffff',
        input: '#ffffff',
        border: '#e5e5e9',
        text: '#1d1d21',
        muted: '#68686e',
        accent: '#6354d2',
        'accent-solid': '#6354d2',
        'on-accent': '#ffffff',
        'accent-soft': '#6d5ce71a',
        sel: '#6d5ce71a',
        pos: '#1a7a35',
        neg: '#c5372c',
        'logo-a': '#a191ff',
        'logo-b': '#6d5ce7',
      },
    },
  },
  {
    id: 'fjord',
    name: 'Fjord',
    description: 'Cool frost blue on polar-night slate (after Nord).',
    tokens: {
      dark: {
        bg: '#2e3440',
        panel: '#323a47',
        elev: '#3b4252',
        input: '#272c36',
        border: '#434c5e',
        text: '#e5e9f0',
        muted: '#a7b0c0',
        accent: '#88c0d0',
        'accent-solid': '#4c6f99',
        'on-accent': '#ffffff',
        'accent-soft': '#88c0d024',
        sel: '#88c0d026',
        pos: '#a3be8c',
        neg: '#eb959a',
        'logo-a': '#8fbcbb',
        'logo-b': '#5e81ac',
      },
      light: {
        bg: '#eceff4',
        panel: '#e5e9f0',
        elev: '#f4f6f9',
        input: '#ffffff',
        border: '#d1d8e3',
        text: '#2e3440',
        muted: '#545e70',
        accent: '#38658f',
        'accent-solid': '#38658f',
        'on-accent': '#ffffff',
        'accent-soft': '#5e81ac1f',
        sel: '#5e81ac21',
        pos: '#3f6b2a',
        neg: '#a33a44',
        'logo-a': '#8fbcbb',
        'logo-b': '#5e81ac',
      },
    },
  },
  {
    id: 'grove',
    name: 'Grove',
    description: 'Sage and aqua on warm forest tones (after Everforest).',
    tokens: {
      dark: {
        bg: '#272e33',
        panel: '#2d353b',
        elev: '#343f44',
        input: '#1e2326',
        border: '#3d484d',
        text: '#d3c6aa',
        muted: '#a6ae9f',
        accent: '#83c0b7',
        'accent-solid': '#3f7a73',
        'on-accent': '#ffffff',
        'accent-soft': '#7fbbb324',
        sel: '#7fbbb326',
        pos: '#a7c080',
        neg: '#e98a8c',
        'logo-a': '#a7c080',
        'logo-b': '#3f7a73',
      },
      light: {
        bg: '#fdf6e3',
        panel: '#f4f0d9',
        elev: '#fffbef',
        input: '#fffdf5',
        border: '#e0dcc7',
        text: '#404d4a',
        muted: '#5b6661',
        accent: '#2b6b64',
        'accent-solid': '#2b6b64',
        'on-accent': '#ffffff',
        'accent-soft': '#35a77c1f',
        sel: '#35a77c21',
        pos: '#4d6b1d',
        neg: '#b13a3a',
        'logo-a': '#8da101',
        'logo-b': '#35a77c',
      },
    },
  },
  {
    id: 'ember',
    name: 'Ember',
    description: 'Warm gold on charcoal and parchment (after Gruvbox).',
    tokens: {
      dark: {
        bg: '#282828',
        panel: '#2f2c2b',
        elev: '#3c3836',
        input: '#1d2021',
        border: '#504945',
        text: '#ebdbb2',
        muted: '#bdae93',
        accent: '#fabd2f',
        'accent-solid': '#d79921',
        'on-accent': '#282828',
        'accent-soft': '#fabd2f1f',
        sel: '#fabd2f21',
        pos: '#b8bb26',
        neg: '#fb7f6c',
        'logo-a': '#fabd2f',
        'logo-b': '#d65d0e',
      },
      light: {
        bg: '#fbf1c7',
        panel: '#f2e5bc',
        elev: '#fdf6dd',
        input: '#fffbea',
        border: '#dccfa8',
        text: '#3c3836',
        muted: '#665c54',
        accent: '#8a4f08',
        'accent-solid': '#d79921',
        'on-accent': '#282828',
        'accent-soft': '#d799211f',
        sel: '#d7992124',
        pos: '#5f6a0e',
        neg: '#9d0006',
        'logo-a': '#fabd2f',
        'logo-b': '#d65d0e',
      },
    },
  },
];

export function isBuiltin(id: string): boolean {
  return BUILTIN_THEMES.some((t) => t.id === id);
}

export const HEX_COLOUR = /^#[0-9a-f]{6}([0-9a-f]{2})?$/;

type Rgba = [number, number, number, number];

function parseHex(s: string): Rgba {
  const v = (i: number) => parseInt(s.slice(i, i + 2), 16) / 255;
  return [v(1), v(3), v(5), s.length === 9 ? v(7) : 1];
}

function over(top: Rgba, bottom: Rgba): Rgba {
  const mix = (t: number, b: number) => t * top[3] + b * (1 - top[3]);
  return [mix(top[0], bottom[0]), mix(top[1], bottom[1]), mix(top[2], bottom[2]), 1];
}

/** The colour of "a" or "a/b" (a laid over b; a translucent base sits on bg). */
function surface(m: ThemeColours, name: string): Rgba {
  const parts = name.split('/');
  let c = parseHex(m[parts[parts.length - 1] ?? ''] ?? '#000000');
  if (c[3] < 1) c = over(c, parseHex(m['bg'] ?? '#000000'));
  for (let i = parts.length - 2; i >= 0; i--) c = over(parseHex(m[parts[i] ?? ''] ?? '#000000'), c);
  return c;
}

function luminance(c: Rgba): number {
  const lin = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
}

/** WCAG 2.1 contrast ratio of two opaque colours (1–21). */
export function contrast(a: string, b: string): number {
  return ratio(parseHex(a), parseHex(b));
}

function ratio(a: Rgba, b: Rgba): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

export type ContrastResult = { mode: Mode; fg: string; bg: string; ratio: number; ok: boolean };

/** Every contract pair of both variants, with its ratio (rounded down to 2 decimals). */
export function contrastReport(tokens: ThemeTokens): ContrastResult[] {
  const out: ContrastResult[] = [];
  for (const mode of ['dark', 'light'] as const) {
    const m = tokens[mode];
    for (const p of CONTRAST_PAIRS) {
      for (const bg of p.bg) {
        const r = Math.floor(ratio(surface(m, p.fg), surface(m, bg)) * 100) / 100;
        out.push({ mode, fg: p.fg, bg, ratio: r, ok: r >= MIN_CONTRAST });
      }
    }
  }
  return out;
}

/** Whether every key of both variants is a hex colour. */
export function completeTokens(tokens: ThemeTokens): boolean {
  return (['dark', 'light'] as const).every((m) => THEME_KEYS.every((k) => HEX_COLOUR.test(tokens[m][k] ?? '')));
}
