// UI state (PLAN §7.4): app-wide toggles and settings, ported from the legacy S.* globals.
// Everything here except sidebarAutoCollapsed and the transient dialogs is persisted (see
// collectUi); hydrate applies a parsed preferences document.

import { create } from 'zustand';

import type { UiScalePref } from '../styles/scale';
import type { ThemePref } from '../styles/theme';
import {
  CARD_KEYS,
  DEFAULT_AI,
  DEFAULT_CFG,
  type AiPrefs,
  type CatMode,
  type ChartCfg,
  type Mode,
  type ParsedPrefs,
  type PrefsDoc,
} from './prefs';

/** Below this window width the sidebar collapses by itself (legacy). */
export const NARROW_PX = 900;

export type UiState = {
  themePref: ThemePref;
  uiScalePref: UiScalePref;
  mode: Mode;
  /** The user's choice (persisted as sbOpen). Hidden by default (v7). */
  sidebarOpen: boolean;
  /** Hidden because the window is narrow; restored when it widens again. Not persisted. */
  sidebarAutoCollapsed: boolean;
  vizOpen: boolean;
  paneHeight: string;
  cards: string[];
  cardOrder: string[] | null;
  catMode: CatMode;
  cfg: ChartCfg;
  ai: AiPrefs;
  importFastPath: boolean;
  /** Keyboard shortcuts window (not persisted). */
  keysOpen: boolean;

  setTheme: (t: ThemePref) => void;
  setUiScale: (s: UiScalePref) => void;
  /** The title-bar toggle: shows or hides the sidebar and forgets any auto-collapse. */
  toggleSidebar: () => void;
  /** Called when the window crosses NARROW_PX (legacy resize handler). */
  setNarrow: (narrow: boolean) => void;
  setMode: (m: Mode) => void;
  toggleViz: () => void;
  setAiOpen: (open: boolean) => void;
  setKeysOpen: (open: boolean) => void;
  hydrate: (p: ParsedPrefs, narrow: boolean) => void;
};

export const uiDefaults = {
  themePref: 'system',
  uiScalePref: 'auto',
  mode: 'table',
  sidebarOpen: false,
  sidebarAutoCollapsed: false,
  vizOpen: true,
  paneHeight: '',
  cards: [...CARD_KEYS],
  cardOrder: null,
  catMode: 'donut',
  cfg: DEFAULT_CFG,
  ai: DEFAULT_AI,
  importFastPath: true,
  keysOpen: false,
} satisfies Partial<UiState>;

export const useUi = create<UiState>()((set) => ({
  ...uiDefaults,
  setTheme: (themePref) => {
    set({ themePref });
  },
  setUiScale: (uiScalePref) => {
    set({ uiScalePref });
  },
  toggleSidebar: () => {
    set((s) => {
      const shown = s.sidebarOpen && !s.sidebarAutoCollapsed;
      return { sidebarOpen: !shown, sidebarAutoCollapsed: false };
    });
  },
  setNarrow: (narrow) => {
    set((s) => {
      if (narrow && s.sidebarOpen && !s.sidebarAutoCollapsed) return { sidebarAutoCollapsed: true };
      if (!narrow && s.sidebarAutoCollapsed) return { sidebarAutoCollapsed: false };
      return {};
    });
  },
  setMode: (mode) => {
    set({ mode });
  },
  // Legacy toggleViz: from the overview it returns to the table with the pane shown.
  toggleViz: () => {
    set((s) => (s.mode === 'ov' ? { mode: 'table', vizOpen: true } : { vizOpen: !s.vizOpen }));
  },
  setAiOpen: (open) => {
    set((s) => ({ ai: { ...s.ai, open } }));
  },
  setKeysOpen: (keysOpen) => {
    set({ keysOpen });
  },
  hydrate: (p, narrow) => {
    set((s) => ({
      themePref: p.theme ?? s.themePref,
      uiScalePref: p.ui ?? s.uiScalePref,
      mode: p.mode ?? s.mode,
      sidebarOpen: p.sbOpen ?? s.sidebarOpen,
      // legacy applyPrefs: a saved open sidebar on a narrow window starts auto-collapsed
      sidebarAutoCollapsed: (p.sbOpen ?? s.sidebarOpen) && narrow,
      vizOpen: p.noViz === undefined ? s.vizOpen : !p.noViz,
      paneHeight: p.vizH ?? s.paneHeight,
      cards: p.cards ?? s.cards,
      cardOrder: p.cardOrder === undefined ? s.cardOrder : p.cardOrder,
      catMode: p.catMode ?? s.catMode,
      cfg: { ...s.cfg, ...p.cfg },
      ai: { ...s.ai, ...p.ai },
      importFastPath: p.importFastPath ?? s.importFastPath,
    }));
  },
}));

/** The persisted UI slice, in preferences-document form. */
export function collectUi(s: UiState): Omit<PrefsDoc, 'v' | 'views'> {
  return {
    theme: s.themePref,
    ui: s.uiScalePref,
    vizH: s.paneHeight,
    noViz: !s.vizOpen,
    mode: s.mode,
    cards: s.cards,
    cardOrder: s.cardOrder,
    catMode: s.catMode,
    cfg: s.cfg,
    sbOpen: s.sidebarOpen,
    ai: s.ai,
    importFastPath: s.importFastPath,
  };
}

/** Whether the sidebar is on screen. */
export const sidebarShown = (s: UiState) => s.sidebarOpen && !s.sidebarAutoCollapsed;
