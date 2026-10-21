// Theme (PLAN §7.5, legacy setTheme): data-theme on <html>. While the preference is "system" the
// theme follows prefers-color-scheme, including changes while the app is open.

export type ThemePref = 'system' | 'dark' | 'light';
export type Theme = 'dark' | 'light';

export const LIGHT_QUERY = '(prefers-color-scheme: light)';

export function resolveTheme(pref: ThemePref, prefersLight: boolean): Theme {
  if (pref === 'system') return prefersLight ? 'light' : 'dark';
  return pref;
}

/**
 * Applies the theme for pref now and, for "system", whenever the OS setting changes.
 * Returns a function that stops listening.
 */
export function watchTheme(pref: ThemePref, root: HTMLElement = document.documentElement): () => void {
  const mq = window.matchMedia(LIGHT_QUERY);
  const apply = () => {
    root.dataset['theme'] = resolveTheme(pref, mq.matches);
  };
  apply();
  if (pref !== 'system') return () => undefined;
  mq.addEventListener('change', apply);
  return () => {
    mq.removeEventListener('change', apply);
  };
}
