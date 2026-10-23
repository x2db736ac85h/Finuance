// The preferences document (PLAN §7.4): what `PUT /preferences` stores. It keeps the legacy
// collectPrefs key names (theme, ui, vizH, noViz, sbOpen, …) so a legacy backup's prefs map onto it
// directly (§4.10). Search and filters are never persisted, as in the legacy app.
//
// parsePrefs is the only way a stored document enters the stores: it drops unknown keys and values
// of the wrong type, so a document written by an older or newer build can't break the UI.

import { MANUAL_SCALES, type UiScalePref } from '../styles/scale';
import type { ThemePref } from '../styles/theme';

/** Grid column id (backend ColumnRegistry name, e.g. bookedOn, payee, amount). */
export type Column = string;

export type Mode = 'table' | 'ov';
export type CatMode = 'donut' | 'bars';

/** Insight card settings (legacy S.cfg). */
export type ChartCfg = {
  flowMonths: 'auto' | 'all' | number;
  paceCmp: 'prev' | 'yoy' | 'avg3' | 'avg6' | 'none';
  payeeTop: number;
  payeeScope: 'list' | 'all';
  heatRange: 'period' | 'all' | number;
  ovMonths: number;
};

/** How much a model thinks before answering (PLAN §4.8, ADR 0015). */
export type Thinking = 'off' | 'auto' | 'low' | 'medium' | 'high';
export const THINKING: readonly Thinking[] = ['off', 'auto', 'low', 'medium', 'high'];

/**
 * Assistant panel settings (legacy S.ai). The server address is instance config now: the panel
 * picks a connection by name and a model. `think` and `keepChats` are the legacy fields, read
 * only to migrate (thinking on → auto; chat retention is a server setting now).
 */
export type AiPrefs = {
  open: boolean;
  width: number;
  max: boolean;
  connection: string;
  model: string;
  mode: string;
  thinking: Thinking;
  maxRows: number;
  temperature: number;
  scope: string;
};

export type SortPref = { column: Column; dir: 'asc' | 'desc' };
export type StatColumn = 'amount' | 'balanceAfter';
/** 'all' | 'l30' | 'l3m' | 'l12m' | 'ytd' | `m:YYYY-MM` | `y:YYYY` | 'custom' */
export type PeriodPref = string;
export type CustomRange = { from: string; to: string };

/** The persisted part of a view (legacy collectPrefs: hidden, widths, statCol, sort, period, custom). */
export type PersistedView = {
  hidden: Column[];
  widths: Partial<Record<Column, number>>;
  statColumn: StatColumn;
  sort: SortPref | null;
  period: PeriodPref;
  custom: CustomRange | null;
};

export type PrefsDoc = {
  v: 1;
  theme: ThemePref;
  ui: UiScalePref;
  /** Insights pane height, a CSS length ('' = default, half the window). */
  vizH: string;
  noViz: boolean;
  mode: Mode;
  cards: string[];
  cardOrder: string[] | null;
  catMode: CatMode;
  cfg: ChartCfg;
  sbOpen: boolean;
  ai: AiPrefs;
  importFastPath: boolean;
  views: Record<string, PersistedView>;
};

/** Insight cards in their default order (legacy CARD_DEFS). */
export const CARD_KEYS = ['sum', 'cat', 'flow', 'pace', 'payee', 'heat'] as const;

export const DEFAULT_CFG: ChartCfg = {
  flowMonths: 'auto',
  paceCmp: 'prev',
  payeeTop: 25,
  payeeScope: 'list',
  heatRange: 'period',
  ovMonths: 12,
};

export const DEFAULT_AI: AiPrefs = {
  open: false,
  width: 0,
  max: false,
  connection: '',
  model: '',
  mode: 'auto',
  thinking: 'auto',
  maxRows: 50,
  temperature: 0.2,
  scope: 'all',
};

export const PERIOD_RE = /^(all|l30|l3m|l12m|ytd|custom|m:\d{4}-\d{2}|y:\d{4})$/;
const DAY_RE = /^\d{4}-\d{2}-\d{2}$/;

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => typeof x === 'object' && x !== null && !Array.isArray(x);
const isStrings = (x: unknown): x is string[] => Array.isArray(x) && x.every((s) => typeof s === 'string');
const oneOf = <T extends string>(x: unknown, all: readonly T[]): x is T => all.includes(x as T);
const finite = (x: unknown): x is number => typeof x === 'number' && Number.isFinite(x);

export function parseTheme(x: unknown): ThemePref | undefined {
  return oneOf(x, ['system', 'dark', 'light'] as const) ? x : undefined;
}

export function parseScale(x: unknown): UiScalePref | undefined {
  if (x === 'auto') return x;
  // The legacy stored manual sizes as numbers; tolerate numeric strings too.
  const n = typeof x === 'string' ? Number(x) : x;
  return finite(n) && (MANUAL_SCALES as readonly number[]).includes(n) ? n : undefined;
}

function parseCfg(x: unknown): Partial<ChartCfg> {
  if (!isObj(x)) return {};
  const out: Partial<ChartCfg> = {};
  if (x['flowMonths'] === 'auto' || x['flowMonths'] === 'all' || finite(x['flowMonths']))
    out.flowMonths = x['flowMonths'];
  if (oneOf(x['paceCmp'], ['prev', 'yoy', 'avg3', 'avg6', 'none'] as const)) out.paceCmp = x['paceCmp'];
  if (finite(x['payeeTop'])) out.payeeTop = x['payeeTop'];
  if (oneOf(x['payeeScope'], ['list', 'all'] as const)) out.payeeScope = x['payeeScope'];
  if (x['heatRange'] === 'period' || x['heatRange'] === 'all' || finite(x['heatRange'])) out.heatRange = x['heatRange'];
  if (finite(x['ovMonths'])) out.ovMonths = x['ovMonths'];
  return out;
}

function parseAi(x: unknown): Partial<AiPrefs> {
  if (!isObj(x)) return {};
  const out: Partial<AiPrefs> = {};
  for (const k of ['open', 'max'] as const) {
    const v = x[k];
    if (typeof v === 'boolean') out[k] = v;
  }
  for (const k of ['width', 'maxRows', 'temperature'] as const) {
    const v = x[k];
    if (finite(v)) out[k] = v;
  }
  for (const k of ['connection', 'model', 'mode', 'scope'] as const) {
    const v = x[k];
    if (typeof v === 'string') out[k] = v;
  }
  const t = x['thinking'];
  if (typeof t === 'string' && (THINKING as readonly string[]).includes(t)) out.thinking = t as Thinking;
  // legacy: a checkbox "let reasoning models think"
  else if (typeof x['think'] === 'boolean') out.thinking = x['think'] ? 'auto' : 'off';
  // legacy name
  if (out.temperature === undefined && finite(x['temp'])) out.temperature = x['temp'];
  return out;
}

export function parseView(x: unknown): Partial<PersistedView> {
  if (!isObj(x)) return {};
  const out: Partial<PersistedView> = {};
  if (isStrings(x['hidden'])) out.hidden = x['hidden'];
  if (isObj(x['widths'])) {
    const w: Record<string, number> = {};
    for (const [k, v] of Object.entries(x['widths'])) if (finite(v) && v > 0) w[k] = v;
    out.widths = w;
  }
  if (oneOf(x['statColumn'], ['amount', 'balanceAfter'] as const)) out.statColumn = x['statColumn'];
  const s = x['sort'];
  if (s === null) out.sort = null;
  else if (isObj(s) && typeof s['column'] === 'string' && oneOf(s['dir'], ['asc', 'desc'] as const))
    out.sort = { column: s['column'], dir: s['dir'] };
  if (typeof x['period'] === 'string' && PERIOD_RE.test(x['period'])) out.period = x['period'];
  const c = x['custom'];
  if (c === null) out.custom = null;
  else if (isObj(c) && typeof c['from'] === 'string' && typeof c['to'] === 'string') {
    if (DAY_RE.test(c['from']) && DAY_RE.test(c['to'])) out.custom = { from: c['from'], to: c['to'] };
  }
  return out;
}

/** A stored document with everything unusable removed. Missing keys keep the store defaults. */
export type ParsedPrefs = Partial<Omit<PrefsDoc, 'cfg' | 'ai' | 'views' | 'v'>> & {
  cfg?: Partial<ChartCfg>;
  ai?: Partial<AiPrefs>;
  views?: Record<string, Partial<PersistedView>>;
};

export function parsePrefs(raw: unknown): ParsedPrefs {
  if (!isObj(raw)) return {};
  const out: ParsedPrefs = {};
  const theme = parseTheme(raw['theme']);
  if (theme) out.theme = theme;
  const ui = parseScale(raw['ui']);
  if (ui !== undefined) out.ui = ui;
  if (typeof raw['vizH'] === 'string' && /^(\d+(\.\d+)?px)?$/.test(raw['vizH'])) out.vizH = raw['vizH'];
  if (typeof raw['noViz'] === 'boolean') out.noViz = raw['noViz'];
  if (oneOf(raw['mode'], ['table', 'ov'] as const)) out.mode = raw['mode'];
  if (isStrings(raw['cards'])) out.cards = raw['cards'].filter((k) => oneOf(k, CARD_KEYS));
  if (raw['cardOrder'] === null || isStrings(raw['cardOrder'])) out.cardOrder = raw['cardOrder'];
  if (oneOf(raw['catMode'], ['donut', 'bars'] as const)) out.catMode = raw['catMode'];
  if (raw['cfg'] !== undefined) out.cfg = parseCfg(raw['cfg']);
  if (typeof raw['sbOpen'] === 'boolean') out.sbOpen = raw['sbOpen'];
  if (raw['ai'] !== undefined) out.ai = parseAi(raw['ai']);
  if (typeof raw['importFastPath'] === 'boolean') out.importFastPath = raw['importFastPath'];
  if (isObj(raw['views'])) {
    out.views = {};
    for (const [id, v] of Object.entries(raw['views'])) out.views[id] = parseView(v);
  }
  return out;
}

/**
 * The legacy cardOrder(): stored keys that still exist, then any new cards in their default order.
 */
export function cardOrder(stored: readonly string[] | null): string[] {
  const keys: readonly string[] = CARD_KEYS;
  const o = (stored ?? []).filter((k, i, a) => keys.includes(k) && a.indexOf(k) === i);
  for (const k of keys) if (!o.includes(k)) o.push(k);
  return o;
}
