// A colour theme as a file (Phase 27): JSON, because a theme already is an object of two variants
// with the same keys. Reading one is strict: both variants, exactly the known tokens, hex colours
// only. Nothing else of a file is ever used, so a theme from somebody else can set colours and no
// more (they are applied with style.setProperty, never as text of a style sheet).

import { completeTokens, contrastReport, HEX_COLOUR, THEME_KEYS, type ThemeTokens } from './themes';

export const THEME_FILE_FORMAT = 'finuance-theme';
export const THEME_FILE_VERSION = 1;
/** A theme file is a few hundred bytes; anything much larger is not one. */
export const THEME_FILE_MAX_BYTES = 16 * 1024;
export const THEME_NAME_MAX = 40;
/** How many imported themes a person keeps (they live in the preferences document). */
export const MY_THEMES_MAX = 8;
/** Ids of a person's own imported themes: never a built-in's or an administrator's. */
export const MY_THEME_PREFIX = 'my-';

export type MyTheme = { id: string; name: string; tokens: ThemeTokens };

export function themeFileText(name: string, tokens: ThemeTokens): string {
  const variant = (m: 'dark' | 'light') => Object.fromEntries(THEME_KEYS.map((k) => [k, tokens[m][k] ?? '']));
  return (
    JSON.stringify(
      {
        format: THEME_FILE_FORMAT,
        version: THEME_FILE_VERSION,
        name,
        tokens: { dark: variant('dark'), light: variant('light') },
      },
      null,
      2,
    ) + '\n'
  );
}

/** A file name for a theme: its name in lower case with dashes. */
export function themeFileName(name: string): string {
  const slug = slugOf(name) || 'theme';
  return `${slug}.finuance-theme.json`;
}

function slugOf(name: string): string {
  return name
    .normalize('NFKD')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24)
    .replace(/-+$/, '');
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

/** The tokens of an unknown value when they are a complete, colours-only theme; else what is wrong. */
export function readTokens(raw: unknown): { tokens: ThemeTokens } | { problems: string[] } {
  const problems: string[] = [];
  if (!isObj(raw)) return { problems: ['The file has no colours ("tokens").'] };
  const out: ThemeTokens = { dark: {}, light: {} };
  for (const mode of ['dark', 'light'] as const) {
    const m = raw[mode];
    if (!isObj(m)) {
      problems.push(`The ${mode} variant is missing.`);
      continue;
    }
    for (const k of Object.keys(m))
      if (!THEME_KEYS.includes(k)) problems.push(`${mode}: “${k.slice(0, 40)}” is not a colour of a theme.`);
    for (const k of THEME_KEYS) {
      const v = m[k];
      const hex = typeof v === 'string' ? v.trim().toLowerCase() : '';
      if (v === undefined) problems.push(`${mode}: “${k}” is missing.`);
      else if (!HEX_COLOUR.test(hex)) problems.push(`${mode}: “${k}” must be a colour such as #13141a.`);
      else out[mode][k] = hex;
    }
  }
  if (problems.length) return { problems: problems.slice(0, 8) };
  return { tokens: out };
}

export type ThemeFileResult =
  { ok: true; name: string; tokens: ThemeTokens; lowContrast: number } | { ok: false; problems: string[] };

/** Reads a theme file's text. `lowContrast` counts the text/surface pairs below 4.5:1 (a warning). */
export function parseThemeFile(text: string): ThemeFileResult {
  if (text.length > THEME_FILE_MAX_BYTES) return { ok: false, problems: ['The file is too large to be a theme.'] };
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return { ok: false, problems: ['The file is not JSON.'] };
  }
  if (!isObj(raw) || raw['format'] !== THEME_FILE_FORMAT)
    return { ok: false, problems: ['This is not a Finuance theme file (its "format" must be "finuance-theme").'] };
  if (raw['version'] !== THEME_FILE_VERSION)
    return { ok: false, problems: ['This theme file is of a version this app does not read.'] };
  const name = typeof raw['name'] === 'string' ? raw['name'].replace(/\s+/g, ' ').trim().slice(0, THEME_NAME_MAX) : '';
  if (!name) return { ok: false, problems: ['The theme has no name.'] };
  const read = readTokens(raw['tokens']);
  if ('problems' in read) return { ok: false, problems: read.problems };
  return { ok: true, name, tokens: read.tokens, lowContrast: contrastReport(read.tokens).filter((r) => !r.ok).length };
}

/** An id for an imported theme that none of `taken` has: `my-<name>`, then `-2`, `-3`, … */
export function myThemeId(name: string, taken: readonly string[]): string {
  const base = MY_THEME_PREFIX + (slugOf(name) || 'theme');
  if (!taken.includes(base)) return base;
  for (let n = 2; ; n++) if (!taken.includes(`${base}-${String(n)}`)) return `${base}-${String(n)}`;
}

/** The imported themes of a preferences document, with anything that is not one left out. */
export function parseMyThemes(raw: unknown): MyTheme[] {
  if (!Array.isArray(raw)) return [];
  const out: MyTheme[] = [];
  for (const t of raw) {
    if (out.length >= MY_THEMES_MAX) break;
    if (!isObj(t)) continue;
    const id = t['id'];
    const name = t['name'];
    if (typeof id !== 'string' || !/^my-[a-z0-9][a-z0-9-]{0,28}$/.test(id) || out.some((o) => o.id === id)) continue;
    if (typeof name !== 'string' || !name.trim()) continue;
    const read = readTokens(t['tokens']);
    if ('problems' in read || !completeTokens(read.tokens)) continue;
    out.push({ id, name: name.trim().slice(0, THEME_NAME_MAX), tokens: read.tokens });
  }
  return out;
}
