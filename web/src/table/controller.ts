// Keyboard control of the table (PLAN task 10.8, legacy moveSel): the mounted table registers its
// grid and loaded rows here, and the global keyboard manager moves the selection with ↑/↓, j/k and
// PgUp/PgDn. A move into rows not loaded yet scrolls there first and selects the row once its
// window arrives. The selected row's position lives in the view (`selectedIndex`) for the status
// bar and the inspector; it is re-found in the loaded rows when the result changes.

import { useCallback, useEffect, useMemo, useRef } from 'react';

import { useSelection } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import type { RowSource } from './data';
import type { GridHandle } from './Grid';

type State = {
  viewId: string;
  grid: GridHandle | null;
  rows: RowSource | null;
  /** A position moved to whose row isn't loaded yet. */
  pending: number | null;
};

let active: { current: State } | null = null;

const viewState = (st: State) => viewOf(useViews.getState().views, st.viewId);

function selectAt(st: State, i: number): boolean {
  const row = st.rows?.rowAt(i);
  if (!row) return false;
  st.pending = null;
  useViews.getState().patch(st.viewId, { selectedId: row.id, selectedIndex: i });
  return true;
}

export function useGridController(viewId: string) {
  const ref = useRef<State>({ viewId, grid: null, rows: null, pending: null });
  useEffect(() => {
    ref.current.viewId = viewId;
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
    if (st.pending !== null) {
      selectAt(st, st.pending);
      return;
    }
    // Keep the selected row's position in step with the result (sort or filters changed).
    const { selectedId, selectedIndex } = viewState(st);
    if (!selectedId) return;
    if (selectedIndex !== null) {
      // a position whose window isn't loaded (scrolled away) is still right
      const r = rows.rowAt(selectedIndex);
      if (!r || r.id === selectedId) return;
    }
    const at = rows.indexOf(selectedId);
    const next = at >= 0 ? at : null;
    if (next !== selectedIndex) useViews.getState().patch(st.viewId, { selectedIndex: next });
  }, []);
  const select = useCallback((id: string | null, index: number) => {
    const st = ref.current;
    st.pending = null;
    useViews.getState().patch(st.viewId, { selectedId: id, selectedIndex: id ? index : null });
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
  const cur = viewState(st).selectedIndex;
  const target = cur === null ? (d > 0 ? 0 : total - 1) : Math.min(total - 1, Math.max(0, cur + d));
  st.grid.scrollToIndex(target);
  if (!selectAt(st, target)) st.pending = target;
  return true;
}

/** Esc after popovers and search (legacy order): the multi-selection first, then the row. */
export function escapeSelection(): boolean {
  const st = active?.current;
  if (!st) return false;
  const sel = useSelection.getState();
  if (sel.viewId === st.viewId && sel.rows.size) {
    sel.clear(st.viewId);
    return true;
  }
  if (!viewState(st).selectedId) return false;
  st.pending = null;
  useViews.getState().patch(st.viewId, { selectedId: null, selectedIndex: null });
  return true;
}
