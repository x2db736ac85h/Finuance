// The chart framework (PLAN §7.8, legacy §13.2): every chart is a ChartDef whose use() reads its
// data and returns the parts a card or the modal shows. Charts draw in logical units (w × h =
// pixels / interface scale) and always provide a table twin (accessibility, legacy c.table).

import type { ReactNode } from 'react';

import type { Schemas } from '../api/client';
import type { CatMode, ChartCfg, CustomRange, PeriodPref } from '../stores/prefs';
import type { FilterState } from '../stores/viewStore';
import type { ViewQuery } from '../table/context';

export type ChartKey = 'sum' | 'cat' | 'flow' | 'pace' | 'payee' | 'heat';

/** The table twin: column titles, which columns are numeric (right-aligned) and the rows as text. */
export type Twin = { cols: string[]; num: boolean[]; rows: string[][] };

export type FilterColumn = Schemas['FilterColumn'];

/** What a click on a chart does to the table (legacy toggleFilter / setPeriod). */
export type ChartActions = {
  /** Sets a column condition, or clears it when it is already the same (legacy toggleFilter). */
  toggleFilter: (column: FilterColumn, cond: string) => void;
  setPeriod: (period: PeriodPref, custom?: CustomRange) => void;
  /** Selects a transaction in the table, scrolling to it. */
  locate: (id: string) => void;
  setCatMode: (m: CatMode) => void;
};

/** Everything a chart needs besides its data. */
export type ChartEnv = {
  w: number;
  h: number;
  /** In the modal (larger sizes, more months). */
  big: boolean;
  cfg: ChartCfg;
  catMode: CatMode;
  /** The view's period preference and filters (highlighting and toggles). */
  period: PeriodPref;
  filters: Partial<Record<string, FilterState>>;
  actions: ChartActions;
};

export type ChartParts = {
  loading: boolean;
  error: boolean;
  /** Header subtitle (legacy c.sub). */
  sub: string;
  /** Header legend or controls (legacy c.cx). */
  legend?: ReactNode;
  /** The drawing; a message when there is nothing to draw. */
  body: ReactNode;
  table: Twin | null;
};

/** A range setting shown in the modal (legacy cfgCtl). */
export type CfgControl = {
  key: keyof ChartCfg;
  label: string;
  options: readonly (readonly [value: string | number, label: string])[];
};

export type ChartDef = {
  key: ChartKey;
  title: string;
  cfgControls?: readonly CfgControl[];
  use: (q: ViewQuery, env: ChartEnv) => ChartParts;
};

export const PENDING: ChartParts = { loading: true, error: false, sub: '', body: null, table: null };

/** The parts of a query result: loading and error states, else what render() makes of the data. */
export function partsOf<D>(
  r: { data: D | undefined; isPending: boolean; isError: boolean },
  render: (d: D) => ChartParts,
): ChartParts {
  if (r.data !== undefined) return render(r.data);
  if (r.isError) return { ...PENDING, loading: false, error: true, body: <Empty>Could not draw this chart</Empty> };
  return PENDING;
}

/** The legacy emptyMsg. */
export function Empty({ children }: { children: ReactNode }) {
  return <div className="viz-empty">{children}</div>;
}
