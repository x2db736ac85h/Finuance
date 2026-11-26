// UI state (PLAN §7.4): app-wide toggles and settings, ported from the legacy S.* globals.
// Everything here except sidebarAutoCollapsed and the transient dialogs is persisted (see
// collectUi); hydrate applies a parsed preferences document.

import { create } from 'zustand';

import type { SizeStep, UiScalePref } from '../styles/scale';
import type { ThemePref } from '../styles/theme';
import { MY_THEMES_MAX, myThemeId, type MyTheme } from '../styles/themeFile';
import type { ThemeTokens } from '../styles/themes';
import {
  CARD_KEYS,
  DEFAULT_AI,
  DEFAULT_CFG,
  MAX_RULE_HINTS,
  type AiPrefs,
  type CatMode,
  type ChartCfg,
  type ParsedPrefs,
  type PrefsDoc,
} from './prefs';

/** Below this window width the sidebar collapses by itself (legacy). */
export const NARROW_PX = 900;

export type UiState = {
  themePref: ThemePref;
  /** Colour theme id ('' = the instance's default). */
  themeId: string;
  /** Colour themes this person imported (Phase 27). */
  myThemes: MyTheme[];
  uiScalePref: UiScalePref;
  /** The size slider's step around the auto size (used while uiScalePref is auto). */
  uiStep: SizeStep;
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
  ruleHints: string[];
  /** Keyboard shortcuts window (not persisted). */
  keysOpen: boolean;

  setTheme: (t: ThemePref) => void;
  setThemeId: (id: string) => void;
  /** Keeps an imported theme, chooses it and returns its id. */
  addMyTheme: (name: string, tokens: ThemeTokens) => string;
  /** Drops an imported theme; the instance's default takes over if it was the chosen one. */
  removeMyTheme: (id: string) => void;
  setUiScale: (s: UiScalePref) => void;
  /** The size slider: auto, moved by a step (replaces a fixed size from older documents). */
  setUiStep: (step: SizeStep) => void;
  /** The title-bar toggle: shows or hides the sidebar and forgets any auto-collapse. */
  toggleSidebar: () => void;
  /** Called when the window crosses NARROW_PX (legacy resize handler). */
  setNarrow: (narrow: boolean) => void;
  toggleViz: () => void;
  showViz: () => void;
  setAiOpen: (open: boolean) => void;
  /** Merges assistant settings (panel width, connection, model, mode, thinking, …). */
  setAi: (change: Partial<AiPrefs>) => void;
  setKeysOpen: (open: boolean) => void;
  setImportFastPath: (on: boolean) => void;
  /** Hides a rule suggestion for good (dismissed, or made into a rule). */
  dismissRuleHint: (key: string) => void;
  /** Insight pane height as a CSS length ('' = the default 50vh). */
  setPaneHeight: (h: string) => void;
  setCards: (cards: string[]) => void;
  setCardOrder: (order: string[]) => void;
  setCatMode: (m: CatMode) => void;
  setCfg: (change: Partial<ChartCfg>) => void;
  hydrate: (p: ParsedPrefs, narrow: boolean) => void;
};

export const uiDefaults = {
  themePref: 'system',
  themeId: '',
  myThemes: [],
  uiScalePref: 'auto',
  uiStep: 0,
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
  ruleHints: [] as string[],
  keysOpen: false,
} satisfies Partial<UiState>;

export const useUi = create<UiState>()((set, get) => ({
  ...uiDefaults,
  setTheme: (themePref) => {
    set({ themePref });
  },
  setThemeId: (themeId) => {
    set({ themeId });
  },
  addMyTheme: (name, tokens) => {
    const id = myThemeId(
      name,
      get().myThemes.map((t) => t.id),
    );
    set((s) => ({ myThemes: [...s.myThemes, { id, name, tokens }].slice(-MY_THEMES_MAX), themeId: id }));
    return id;
  },
  removeMyTheme: (id) => {
    set((s) => ({ myThemes: s.myThemes.filter((t) => t.id !== id), themeId: s.themeId === id ? '' : s.themeId }));
  },
  setUiScale: (uiScalePref) => {
    set({ uiScalePref });
  },
  setUiStep: (uiStep) => {
    set({ uiScalePref: 'auto', uiStep });
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
  toggleViz: () => {
    set((s) => ({ vizOpen: !s.vizOpen }));
  },
  // Legacy toggleViz from the overview: back at the table the pane is shown.
  showViz: () => {
    set({ vizOpen: true });
  },
  setAiOpen: (open) => {
    set((s) => ({ ai: { ...s.ai, open } }));
  },
  setAi: (change) => {
    set((s) => ({ ai: { ...s.ai, ...change } }));
  },
  setKeysOpen: (keysOpen) => {
    set({ keysOpen });
  },
  setImportFastPath: (importFastPath) => {
    set({ importFastPath });
  },
  dismissRuleHint: (key) => {
    set((s) => (s.ruleHints.includes(key) ? {} : { ruleHints: [...s.ruleHints, key].slice(-MAX_RULE_HINTS) }));
  },
  setPaneHeight: (paneHeight) => {
    set({ paneHeight });
  },
  setCards: (cards) => {
    set({ cards });
  },
  setCardOrder: (cardOrder) => {
    set({ cardOrder });
  },
  setCatMode: (catMode) => {
    set({ catMode });
  },
  setCfg: (change) => {
    set((s) => ({ cfg: { ...s.cfg, ...change } }));
  },
  hydrate: (p, narrow) => {
    set((s) => ({
      themePref: p.theme ?? s.themePref,
      themeId: p.themeId ?? s.themeId,
      myThemes: p.myThemes ?? s.myThemes,
      uiScalePref: p.ui ?? s.uiScalePref,
      uiStep: p.uiStep ?? s.uiStep,
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
      ruleHints: p.ruleHints ?? s.ruleHints,
    }));
  },
}));

/** The persisted UI slice, in preferences-document form. */
export function collectUi(s: UiState): Omit<PrefsDoc, 'v' | 'views'> {
  return {
    theme: s.themePref,
    themeId: s.themeId,
    ...(s.myThemes.length ? { myThemes: s.myThemes } : {}),
    ui: s.uiScalePref,
    uiStep: s.uiStep,
    vizH: s.paneHeight,
    noViz: !s.vizOpen,
    cards: s.cards,
    cardOrder: s.cardOrder,
    catMode: s.catMode,
    cfg: s.cfg,
    sbOpen: s.sidebarOpen,
    ai: s.ai,
    importFastPath: s.importFastPath,
    ruleHints: s.ruleHints,
  };
}

/** Whether the sidebar is on screen. */
export const sidebarShown = (s: UiState) => s.sidebarOpen && !s.sidebarAutoCollapsed;
