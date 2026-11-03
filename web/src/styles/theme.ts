// Theme (PLAN §7.5, legacy setTheme; colour themes since Phase 19, ADR 0026): data-theme (dark or
// light) and data-theme-id (the built-in colour theme) on <html>. While the preference is
// "system" the mode follows prefers-color-scheme, including changes while the app is open. An
// administrator-approved theme sits on the Indigo base and sets its colours through the CSSOM
// (style.setProperty), which the Content-Security-Policy allows without inline styles.

import { DEFAULT_THEME_ID, THEME_KEYS, isBuiltin, type ThemeTokens } from './themes';

export type ThemePref = 'system' | 'dark' | 'light';
export type Theme = 'dark' | 'light';

export const LIGHT_QUERY = '(prefers-color-scheme: light)';

/** Which colour theme to show: a built-in id, or an approved theme's colours. */
export type ThemeLook = { id: string; custom?: ThemeTokens | undefined };

export function resolveTheme(pref: ThemePref, prefersLight: boolean): Theme {
  if (pref === 'system') return prefersLight ? 'light' : 'dark';
  return pref;
}

/**
 * The look for a chosen theme id: a built-in theme, an approved one (from GET /meta), else the
 * instance's default, else Indigo. A retired or unknown theme therefore falls back quietly.
 */
export function resolveLook(
  id: string | undefined,
  approved: readonly { id: string; tokens: ThemeTokens }[],
  instanceDefault?: string,
): ThemeLook {
  for (const candidate of [id, instanceDefault]) {
    if (!candidate) continue;
    if (isBuiltin(candidate)) return { id: candidate };
    const t = approved.find((a) => a.id === candidate);
    if (t) return { id: candidate, custom: t.tokens };
  }
  return { id: DEFAULT_THEME_ID };
}

/**
 * Applies the theme for pref (and look) now and, for "system", whenever the OS setting changes.
 * Returns a function that stops listening.
 */
export function watchTheme(
  pref: ThemePref,
  root: HTMLElement = document.documentElement,
  look: ThemeLook = { id: DEFAULT_THEME_ID },
): () => void {
  const mq = window.matchMedia(LIGHT_QUERY);
  const apply = () => {
    const mode = resolveTheme(pref, mq.matches);
    root.dataset['theme'] = mode;
    root.dataset['themeId'] = look.custom ? DEFAULT_THEME_ID : look.id;
    for (const k of THEME_KEYS) {
      const v = look.custom?.[mode][k];
      if (v) root.style.setProperty(`--${k}`, v);
      else root.style.removeProperty(`--${k}`);
    }
  };
  apply();
  if (pref !== 'system') return () => undefined;
  mq.addEventListener('change', apply);
  return () => {
    mq.removeEventListener('change', apply);
  };
}
