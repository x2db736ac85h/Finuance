// Keyboard control of the table (PLAN task 10.8, legacy moveSel): the mounted table registers its
// grid and loaded rows here, and the global keyboard manager moves the selection with ↑/↓, j/k and
// PgUp/PgDn. A move into rows not loaded yet scrolls there first and selects the row once its
// window arrives.

import { useCallback, useEffect, useMemo, useRef } from 'react';

import { useViews, viewOf } from '../stores/viewStore';
import type { RowSource } from './data';
import type { GridHandle } from './Grid';

type State = {
  viewId: string;
  grid: GridHandle | null;
  rows: RowSource | null;
  /** The selected row's position, when known. */
  index: number | null;
  /** A position moved to whose row isn't loaded yet. */
  pending: number | null;
};

let active: { current: State } | null = null;

function selectAt(st: State, i: number): boolean {
  const row = st.rows?.rowAt(i);
  if (!row) return false;
  st.index = i;
  st.pending = null;
  useViews.getState().patch(st.viewId, { selectedId: row.id });
  return true;
}

export function useGridController(viewId: string) {
  const ref = useRef<State>({ viewId, grid: null, rows: null, index: null, pending: null });
  useEffect(() => {
    ref.current.viewId = viewId;
    ref.current.index = null;
    const me = ref;
    active = me;
    return () => {
      if (active === me) active = null;
    };
  }, [viewId]);
  const setGrid = useCallback((h: GridHandle | null) => {
    ref.current.grid = h;
  }, []);
  const setRowSource = useCallback((rows: RowSource) => {
    const st = ref.current;
    st.rows = rows;
    if (st.pending !== null) selectAt(st, st.pending);
  }, []);
  const select = useCallback((id: string | null, index: number) => {
    const st = ref.current;
    st.index = id ? index : null;
    useViews.getState().patch(st.viewId, { selectedId: id });
  }, []);
  return useMemo(() => ({ setGrid, setRowSource, select }), [setGrid, setRowSource, select]);
}

export type Move = number | 'pageDown' | 'pageUp';

/** Moves the selection of the table on screen; false when there is no table or no rows. */
export function moveSelection(move: Move): boolean {
  const st = active?.current;
  const total = st?.rows?.total ?? 0;
  if (!st?.grid || !st.rows || !total) return false;
  const page = st.grid.pageRows();
  const d = move === 'pageDown' ? page : move === 'pageUp' ? -page : move;
  let cur = st.index;
  const sel = viewOf(useViews.getState().views, st.viewId).selectedId;
  if (cur === null && sel) {
    const at = st.rows.indexOf(sel);
    if (at >= 0) cur = at;
  }
  const target = cur === null ? (d > 0 ? 0 : total - 1) : Math.min(total - 1, Math.max(0, cur + d));
  st.grid.scrollToIndex(target);
  if (!selectAt(st, target)) st.pending = target;
  return true;
}

/** Esc after popovers and search (legacy order): deselect the row. Multi-selection joins in 10.11. */
export function escapeSelection(): boolean {
  const st = active?.current;
  if (!st) return false;
  if (!viewOf(useViews.getState().views, st.viewId).selectedId) return false;
  st.index = null;
  useViews.getState().patch(st.viewId, { selectedId: null });
  return true;
}
