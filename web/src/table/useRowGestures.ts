// Multi-selection gestures on the grid (PLAN §7.7, legacy tbody mousedown/mousemove/mouseup):
// drag across rows (auto-scrolling near the edges), Shift-click for a range from the anchor,
// ⌘/Ctrl-click to toggle a row. A range whose rows are all loaded is selected at once; one that
// crosses windows not loaded yet is resolved with POST /transactions/ids (at most 5,000 rows).
// After a drag, clicks are ignored for a moment (legacy suppressUntil): re-rendering on mousedown
// can swallow the click, so a time window is used instead of a flag.

import { useCallback, useEffect, useRef, type MouseEvent, type RefObject } from 'react';

import { fetchRange } from '../api/hooks/transactions';
import { fmtInt } from '../lib/format';
import { useSelection, type Picked } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import { toast } from '../ui/toastStore';
import type { ViewQuery } from './context';
import type { RowSource } from './data';

const MAX_RANGE = 5000;
const SUPPRESS_MS = 350;
const DRAG_PX = 5;

type Drag = { p0: number; p1: number; y: number; moved: boolean };

function picked(rows: RowSource, i: number): [string, Picked] | null {
  const r = rows.rowAt(i);
  return r ? [r.id, { amountMinor: r.amount.amountMinor, grouped: !!r.group }] : null;
}

export function useRowGestures(opts: {
  viewId: string;
  q: ViewQuery;
  rows: RowSource;
  wrap: RefObject<HTMLDivElement | null>;
  rowHeight: number;
}) {
  const { viewId } = opts;
  const latest = useRef(opts);
  useEffect(() => {
    latest.current = opts;
  });
  const drag = useRef<Drag | null>(null);
  const suppressUntil = useRef(0);
  const seq = useRef(0);

  /** Selects the rows at positions a..b (inclusive, any order). */
  const selectRange = useCallback(
    (a: number, b: number) => {
      const { rows, q } = latest.current;
      let lo = Math.max(0, Math.min(a, b));
      let hi = Math.min(rows.total - 1, Math.max(a, b));
      if (hi - lo + 1 > MAX_RANGE) {
        toast(`A selection can hold at most ${fmtInt(MAX_RANGE)} rows.`);
        if (b < a) lo = hi - MAX_RANGE + 1;
        else hi = lo + MAX_RANGE - 1;
      }
      const id = ++seq.current;
      const local = new Map<string, Picked>();
      for (let i = lo; i <= hi; i++) {
        const p = picked(rows, i);
        if (!p) break;
        local.set(...p);
      }
      const sel = useSelection.getState();
      if (local.size === hi - lo + 1) {
        sel.setLoading(false);
        sel.set(viewId, local);
        return;
      }
      sel.setLoading(true);
      void fetchRange({ context: q.context, ...(q.sort ? { sort: q.sort } : {}), offset: lo, limit: hi - lo + 1 })
        .then((list) => {
          if (id !== seq.current) return;
          const out = new Map<string, Picked>();
          for (const x of list) out.set(x.id, { amountMinor: x.amountMinor, grouped: !!x.groupId });
          useSelection.getState().set(viewId, out);
        })
        .catch(() => {
          if (id === seq.current) toast("Couldn't select those rows. Try again.", { kind: 'error' });
        })
        .finally(() => {
          if (id === seq.current) useSelection.getState().setLoading(false);
        });
    },
    [viewId],
  );

  const rowAtPoint = (x: number, y: number): number | null => {
    const tr = document.elementFromPoint(x, y)?.closest('tr[data-i]');
    if (!tr || !latest.current.wrap.current?.contains(tr)) return null;
    return Number((tr as HTMLElement).dataset['i']);
  };

  useEffect(() => {
    let last: { x: number; y: number } | null = null;
    let timer: ReturnType<typeof setInterval> | undefined;
    const dragTo = (x: number, y: number) => {
      const d = drag.current;
      const p = d && rowAtPoint(x, y);
      if (!d || p === null || p === d.p1) return;
      d.p1 = p;
      selectRange(d.p0, p);
    };
    const move = (e: globalThis.MouseEvent) => {
      const d = drag.current;
      if (!d) return;
      if (!d.moved && Math.abs(e.clientY - d.y) < DRAG_PX) return;
      if (!d.moved) {
        d.moved = true;
        latest.current.wrap.current?.classList.add('dragsel');
        window.getSelection()?.removeAllRanges();
      }
      last = { x: e.clientX, y: e.clientY };
      dragTo(e.clientX, e.clientY);
      timer ??= setInterval(() => {
        const w = latest.current.wrap.current;
        if (!drag.current || !last || !w) return;
        const r = w.getBoundingClientRect();
        const top = r.top + (w.querySelector('thead')?.getBoundingClientRect().height ?? 0);
        const dy = last.y > r.bottom - 24 ? 1 : last.y < top + 16 ? -1 : 0;
        if (!dy) return;
        w.scrollTop += dy * latest.current.rowHeight;
        dragTo(last.x, Math.max(top + 4, Math.min(r.bottom - 4, last.y)));
      }, 40);
    };
    const up = () => {
      const d = drag.current;
      if (!d) return;
      if (d.moved) suppressUntil.current = performance.now() + SUPPRESS_MS;
      drag.current = null;
      last = null;
      latest.current.wrap.current?.classList.remove('dragsel');
      clearInterval(timer);
      timer = undefined;
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      clearInterval(timer);
    };
  }, [selectRange]);

  const onMouseDown = useCallback(
    (e: MouseEvent<HTMLTableSectionElement>) => {
      if (e.button !== 0) return;
      const tr = (e.target as Element).closest('tr[data-i]');
      if (!tr) return;
      const i = Number((tr as HTMLElement).dataset['i']);
      const sel = useSelection.getState();
      const view = viewOf(useViews.getState().views, viewId);
      if (e.shiftKey) {
        e.preventDefault();
        const anchor = (sel.viewId === viewId ? sel.anchor : null) ?? view.selectedIndex ?? i;
        selectRange(anchor, i);
        return;
      }
      if (e.metaKey || e.ctrlKey) {
        e.preventDefault();
        const { rows } = latest.current;
        const next = new Map(sel.viewId === viewId ? sel.rows : []);
        // ⌘-click with a single row open starts from that row (legacy)
        if (!next.size && view.selectedIndex !== null && view.selectedIndex !== i) {
          const p = picked(rows, view.selectedIndex);
          if (p) next.set(...p);
        }
        const p = picked(rows, i);
        if (p) {
          if (next.has(p[0])) next.delete(p[0]);
          else next.set(...p);
        }
        seq.current++;
        sel.set(viewId, next);
        sel.setAnchor(viewId, i);
        return;
      }
      drag.current = { p0: i, p1: i, y: e.clientY, moved: false };
      sel.setAnchor(viewId, i);
    },
    [viewId, selectRange],
  );

  /** Whether a click should be ignored (modifier clicks, or right after a drag). */
  const ignoreClick = useCallback(
    (e: MouseEvent) => e.shiftKey || e.metaKey || e.ctrlKey || performance.now() < suppressUntil.current,
    [],
  );

  return { onMouseDown, ignoreClick };
}
