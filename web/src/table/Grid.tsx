// The transactions grid (PLAN §7.7, legacy renderHead/renderBody): a fixed-layout <table> with a
// colgroup, sticky header and row-number gutter, virtualised over the view's total. Rows arrive in
// windows of 200 (data.ts); rows not loaded yet render as skeletons. The row height is measured
// after the first rows render, so the interface size and fonts never break the scroll maths.

import { useVirtualizer, type Virtualizer } from '@tanstack/react-virtual';
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from 'react';

import { without } from '../lib/objects';
import { uiScale } from '../styles/scale';
import { selectedIn, useSelection } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import { Icon } from '../ui/Icon';
import { toast } from '../ui/toastStore';
import { gutterWidth, type ColumnDef, type Row } from './columns';
import { useRows, type Page, type RowSource } from './data';
import type { ViewQuery } from './context';
import { hl } from './highlight';
import { useRowGestures } from './useRowGestures';

export type GridHandle = {
  scrollToIndex: (i: number) => void;
  /** Rows that fit on screen (PgUp/PgDn). */
  pageRows: () => number;
};

type Props = {
  viewId: string;
  q: ViewQuery;
  head: Page | undefined;
  columns: readonly ColumnDef[];
  tokens: readonly string[];
  /** The selected row's position in the result, if known (keeps it marked across windows). */
  selectedId: string | null;
  onSelect: (row: Row, index: number) => void;
  onColumnMenu: (col: ColumnDef, anchor: HTMLElement) => void;
  onClearAll: () => void;
  /** Exposes scrolling to the keyboard handling. */
  gridRef?: (h: GridHandle | null) => void;
  /** Reports the loaded rows (for keyboard moves). */
  rowSource?: (rows: RowSource) => void;
  /** The colour of a group (the Group column's pills). */
  groupColor: (id: string) => string | undefined;
};

const DEFAULT_ROW_PX = 30;
const MIN_WIDTH = 48;

export function Grid(props: Props) {
  const {
    viewId,
    q,
    head,
    columns,
    tokens,
    selectedId,
    onSelect,
    onColumnMenu,
    onClearAll,
    gridRef,
    rowSource,
    groupColor,
  } = props;
  const wrap = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLTableSectionElement>(null);
  const [rowH, setRowH] = useState(() => DEFAULT_ROW_PX * uiScale());
  const total = head?.total ?? 0;
  const scale = uiScale();
  const widths = useViews((s) => viewOf(s.views, viewId).widths);
  const sort = useViews((s) => viewOf(s.views, viewId).sort);
  const filters = useViews((s) => viewOf(s.views, viewId).filters);

  // TanStack Virtual returns fresh functions each render; the React Compiler skips this component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtual = useVirtualizer({
    count: total,
    getScrollElement: () => wrap.current,
    estimateSize: () => rowH,
    overscan: 10,
  });
  const items = virtual.getVirtualItems();
  const first = items[0]?.index ?? 0;
  const last = items.at(-1)?.index ?? 0;
  const rows = useRows(q, head, first, last);
  const { rowAt } = rows;
  const marked = useSelection((s) => selectedIn(s, viewId));
  const gestures = useRowGestures({ viewId, q, rows, wrap, rowHeight: rowH });
  const onRowClick = useCallback(
    (e: MouseEvent, row: Row, index: number) => {
      if (gestures.ignoreClick(e)) return;
      useSelection.getState().clear(viewId); // a plain click ends the multi-selection (legacy)
      onSelect(row, index);
    },
    [gestures, onSelect, viewId],
  );

  useEffect(() => {
    rowSource?.(rows);
  });

  // Back to the top when the result changes (legacy refresh()).
  useEffect(() => {
    if (wrap.current) wrap.current.scrollTop = 0;
  }, [q.ctxKey, q.sortKey]);

  // Measure a real row once rows exist; re-estimate when it differs (legacy S.measured).
  const hasRow = items.some((it) => rowAt(it.index));
  useLayoutEffect(() => {
    const tr = body.current?.querySelector('tr[data-i]');
    if (!tr) return;
    const h = tr.getBoundingClientRect().height;
    if (h && Math.abs(h - rowH) > 0.5) setRowH(h);
  }, [hasRow, rowH, scale]);
  useEffect(() => {
    virtual.measure();
  }, [rowH, virtual]);

  const handle = useCallback(
    (v: Virtualizer<HTMLDivElement, Element>): GridHandle => ({
      scrollToIndex: (i) => {
        v.scrollToIndex(i, { align: 'auto' });
      },
      pageRows: () => Math.max(1, Math.floor((wrap.current?.clientHeight ?? 600) / rowH) - 1),
    }),
    [rowH],
  );
  useEffect(() => {
    gridRef?.(handle(virtual));
    return () => {
      gridRef?.(null);
    };
  }, [gridRef, handle, virtual]);

  const gw = Math.round(gutterWidth(total) * scale);
  const widthOf = (c: ColumnDef) => Math.round((widths[c.id] ?? c.width) * scale);
  const tableWidth = gw + columns.reduce((n, c) => n + widthOf(c), 0);
  const padTop = items[0]?.start ?? 0;
  const padBottom = items.length ? virtual.getTotalSize() - (items.at(-1)?.end ?? 0) : 0;
  const ncol = columns.length + 1;

  return (
    <div className="grid-wrap" ref={wrap} role="region" aria-label="Transactions">
      <table className="grid" style={{ width: tableWidth }} aria-rowcount={total}>
        <colgroup>
          <col style={{ width: gw }} />
          {columns.map((c) => (
            <col key={c.id} style={{ width: widthOf(c) }} />
          ))}
        </colgroup>
        <thead>
          <tr>
            <th className="gutter">#</th>
            {columns.map((c) => (
              <HeaderCell
                key={c.id}
                viewId={viewId}
                col={c}
                width={widthOf(c)}
                sorted={sort?.column === c.id ? sort.dir : null}
                filtered={!!(filters[c.id]?.cond.trim() || filters[c.id]?.excluded.length)}
                onMenu={onColumnMenu}
              />
            ))}
          </tr>
        </thead>
        <tbody ref={body} onMouseDown={gestures.onMouseDown}>
          {head && total === 0 ? (
            <tr>
              <td className="none" colSpan={ncol}>
                <div className="none-in">
                  No rows match the current search and filters.{' '}
                  <button type="button" className="link" onClick={onClearAll}>
                    Clear all
                  </button>
                </div>
              </td>
            </tr>
          ) : (
            <>
              {padTop > 0 && (
                <tr aria-hidden="true">
                  <td className="sp" colSpan={ncol} style={{ height: padTop }} />
                </tr>
              )}
              {items.map((it) => {
                const row = rowAt(it.index);
                return row ? (
                  <DataRow
                    key={row.id}
                    row={row}
                    index={it.index}
                    columns={columns}
                    tokens={tokens}
                    selected={row.id === selectedId}
                    marked={marked.has(row.id)}
                    onClick={onRowClick}
                    groupColor={groupColor}
                  />
                ) : (
                  <tr key={`s${String(it.index)}`} className="skel" aria-busy="true">
                    <td className="gutter">{it.index + 1}</td>
                    <td colSpan={columns.length}>
                      <span className="sk" />
                    </td>
                  </tr>
                );
              })}
              {padBottom > 0 && (
                <tr aria-hidden="true">
                  <td className="sp" colSpan={ncol} style={{ height: padBottom }} />
                </tr>
              )}
            </>
          )}
        </tbody>
      </table>
    </div>
  );
}

function DataRow({
  row,
  index,
  columns,
  tokens,
  selected,
  marked,
  onClick,
  groupColor,
}: {
  row: Row;
  index: number;
  columns: readonly ColumnDef[];
  tokens: readonly string[];
  selected: boolean;
  marked: boolean;
  onClick: (e: MouseEvent, row: Row, index: number) => void;
  groupColor: (id: string) => string | undefined;
}) {
  const cls = [selected && 'sel', marked && 'msel'].filter(Boolean).join(' ');
  return (
    <tr
      data-i={index}
      className={cls || undefined}
      aria-selected={selected || marked}
      onClick={(e) => {
        onClick(e, row, index);
      }}
    >
      <td className="gutter">{index + 1}</td>
      {columns.map((c) => {
        const text = c.text(row);
        const cls = c.negative?.(row) ? `${c.type} neg` : c.type;
        return (
          <td
            key={c.id}
            className={cls}
            title={text.length > 12 ? text : undefined}
            onDoubleClick={() => {
              void copyText(text);
            }}
          >
            {cell(c, text, tokens, row.group && groupColor(row.group.id))}
          </td>
        );
      })}
    </tr>
  );
}

function cell(c: ColumnDef, text: string, tokens: readonly string[], color: string | undefined): ReactNode {
  if (c.id === 'group' && text) {
    return (
      <span className="gtag" title={text}>
        <i style={{ background: color } satisfies CSSProperties} />
        <span>{hl(text, tokens)}</span>
      </span>
    );
  }
  return hl(text, tokens);
}

/** Double-click a cell: copy its value (legacy). `done` replaces the toast text. */
export async function copyText(text: string, done?: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast(done ?? (text ? `Copied “${text.length > 40 ? `${text.slice(0, 40)}…` : text}”` : 'Copied an empty value'));
  } catch {
    toast("Couldn't copy: the browser blocked the clipboard.", { kind: 'error' });
  }
}

/** Column header: sort on click (asc → desc → off), menu on the funnel or right-click, resize. */
function HeaderCell({
  viewId,
  col,
  width,
  sorted,
  filtered,
  onMenu,
}: {
  viewId: string;
  col: ColumnDef;
  width: number;
  sorted: 'asc' | 'desc' | null;
  filtered: boolean;
  onMenu: (col: ColumnDef, anchor: HTMLElement) => void;
}) {
  const funnel = useRef<HTMLButtonElement>(null);
  const [resizing, setResizing] = useState(false);
  const patch = useViews((s) => s.patch);
  const cycle = () => {
    const next = sorted === null ? 'asc' : sorted === 'asc' ? 'desc' : null;
    patch(viewId, { sort: next ? { column: col.id, dir: next } : null });
  };
  const startResize = (e: PointerEvent<HTMLSpanElement>) => {
    e.preventDefault();
    e.stopPropagation();
    const x0 = e.clientX;
    const scale = uiScale();
    const w0 = width;
    setResizing(true);
    const move = (ev: globalThis.PointerEvent) => {
      const w = Math.max(MIN_WIDTH, w0 + ev.clientX - x0) / scale;
      const views = useViews.getState().views;
      patch(viewId, { widths: { ...viewOf(views, viewId).widths, [col.id]: Math.round(w) } });
    };
    const up = () => {
      setResizing(false);
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };
  const resetWidth = () => {
    patch(viewId, { widths: without(viewOf(useViews.getState().views, viewId).widths, col.id) });
  };
  return (
    <th
      className={[filtered && 'filtered', sorted && 'sorted'].filter(Boolean).join(' ') || undefined}
      aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : 'none'}
      title={`${col.label} — click to sort, right-click for options`}
      onClick={cycle}
      onContextMenu={(e) => {
        e.preventDefault();
        if (funnel.current) onMenu(col, funnel.current);
      }}
    >
      <div className="th">
        <span className={`tt ${col.type}`}>
          {col.id === 'group' ? (
            <Icon name="tag" />
          ) : col.type === 'number' ? (
            '#'
          ) : col.type === 'date' ? (
            <Icon name="cal" />
          ) : (
            'Aa'
          )}
        </span>
        <span className="tn">{col.label}</span>
        {sorted && (
          <span className="si">
            <Icon name={sorted === 'asc' ? 'up' : 'down'} />
          </span>
        )}
        <button
          ref={funnel}
          type="button"
          className={filtered ? 'tb on' : 'tb'}
          title="Filter & sort"
          aria-label={`Filter and sort ${col.label}`}
          aria-haspopup="menu"
          onClick={(e) => {
            e.stopPropagation();
            onMenu(col, e.currentTarget);
          }}
        >
          <Icon name="filter" />
        </button>
      </div>
      <span
        className={resizing ? 'rz on' : 'rz'}
        title="Drag to resize · double-click to reset"
        onPointerDown={startResize}
        onClick={(e) => {
          e.stopPropagation();
        }}
        onDoubleClick={(e) => {
          e.stopPropagation();
          resetWidth();
        }}
      />
    </th>
  );
}
