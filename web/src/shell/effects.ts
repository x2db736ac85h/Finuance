// App-wide effects that follow the UI store: theme, interface size, sidebar auto-collapse.

import { useEffect, useMemo } from 'react';

import { useMeta } from '../api/hooks/me';
import { applyScale } from '../styles/scale';
import { resolveLook, watchTheme } from '../styles/theme';
import { NARROW_PX, useUi } from '../stores/uiStore';

/**
 * data-theme, data-theme-id and --ui-scale on <html>; auto sizing is re-evaluated on resize
 * (browser zoom). The colour theme comes from the user's choice, else the instance's default, and
 * approved themes from GET /meta.
 */
export function useAppearance(): void {
  const themePref = useUi((s) => s.themePref);
  const themeId = useUi((s) => s.themeId);
  const scalePref = useUi((s) => s.uiScalePref);
  const meta = useMeta().data;
  const look = useMemo(
    () => resolveLook(themeId, meta?.themes ?? [], meta?.defaults.themeId),
    [themeId, meta?.themes, meta?.defaults.themeId],
  );
  useEffect(() => watchTheme(themePref, document.documentElement, look), [themePref, look]);
  useEffect(() => {
    applyScale(scalePref);
    if (scalePref !== 'auto') return;
    const onResize = () => applyScale(scalePref);
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
    };
  }, [scalePref]);
}

/** Legacy resize handler: collapse the sidebar below 900 px, restore it when the window widens. */
export function useSidebarAutoCollapse(): void {
  const setNarrow = useUi((s) => s.setNarrow);
  useEffect(() => {
    let was = window.innerWidth < NARROW_PX;
    const onResize = () => {
      const narrow = window.innerWidth < NARROW_PX;
      if (narrow !== was) setNarrow(narrow);
      was = narrow;
    };
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
    };
  }, [setNarrow]);
}
