// The global keyboard manager (PLAN §7.11, legacy document keydown handler). Open modals handle
// their own keys; an open popover takes Esc and blocks the plain-key shortcuts; plain keys never
// fire while typing in a form control.

import { useEffect, useEffectEvent } from 'react';

import { isMod, isTyping } from '../lib/keyboard';
import { useLayers } from '../ui/layers';

export type KeyActions = {
  /** ⌘/Ctrl F and `/`. Returns false when there is nothing to search yet. */
  focusSearch: () => boolean;
  openFiles: () => void;
  toggleAssistant: () => void;
  openShortcuts: () => void;
};

export function handleKey(e: KeyboardEvent, a: KeyActions, layers: { modal: number; pop: number }): void {
  if (layers.modal > 0) return;
  const k = e.key.toLowerCase();
  if (isMod(e) && !e.altKey) {
    if (k === 'f') {
      if (a.focusSearch()) e.preventDefault();
    } else if (k === 'o') {
      e.preventDefault();
      a.openFiles();
    } else if (k === 'j') {
      e.preventDefault();
      a.toggleAssistant();
    }
    return;
  }
  // Esc: modals and popovers close themselves, the search box clears itself (SearchBox); clearing
  // the row selection joins here with the grid (Phase 10).
  if (layers.pop > 0 || isTyping(document.activeElement) || e.altKey) return;
  if (e.key === '?') {
    e.preventDefault();
    a.openShortcuts();
  } else if (e.key === '/') {
    if (a.focusSearch()) e.preventDefault();
  }
}

export function useGlobalKeys(actions: KeyActions): void {
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    handleKey(e, actions, useLayers.getState());
  });
  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      onKey(e);
    };
    document.addEventListener('keydown', listener);
    return () => {
      document.removeEventListener('keydown', listener);
    };
  }, []);
}
