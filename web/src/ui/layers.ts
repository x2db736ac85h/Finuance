// Open overlays (PLAN §7.11 Esc order). Modals and popovers register while open so the global
// keyboard manager knows that Esc and the plain-key shortcuts belong to them.

import { useEffect } from 'react';
import { create } from 'zustand';

type Kind = 'modal' | 'pop';

export const useLayers = create<{ modal: number; pop: number }>()(() => ({ modal: 0, pop: 0 }));

export function useLayer(kind: Kind, open: boolean): void {
  useEffect(() => {
    if (!open) return;
    useLayers.setState((s) => ({ [kind]: s[kind] + 1 }));
    return () => {
      useLayers.setState((s) => ({ [kind]: s[kind] - 1 }));
    };
  }, [kind, open]);
}
