// App-wide effects that follow the UI store: theme, interface size, sidebar auto-collapse.

import { useEffect } from 'react';

import { applyScale } from '../styles/scale';
import { watchTheme } from '../styles/theme';
import { NARROW_PX, useUi } from '../stores/uiStore';

/** data-theme and --ui-scale on <html>; auto sizing is re-evaluated on resize (browser zoom). */
export function useAppearance(): void {
  const themePref = useUi((s) => s.themePref);
  const scalePref = useUi((s) => s.uiScalePref);
  useEffect(() => watchTheme(themePref), [themePref]);
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
