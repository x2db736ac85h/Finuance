// The table mode of a view (PLAN §7.6–§7.8): insights pane, toolbar, grid and the column menu. The status bar and
// the inspector read the same query (useQueryContext + useHead) from the cache.

import { useCallback, useEffect, useRef, useState } from 'react';

import { useGroups } from '../api/hooks/groups';
import { groupColor } from '../groups/colors';
import { QuickBar } from '../groups/QuickBar';
import { InsightsPane } from '../insights/InsightsPane';
import { searchTokens } from '../lib/text';
import { useSelection } from '../stores/selectionStore';
import { useViews, viewOf } from '../stores/viewStore';
import { availableColumns, hasBalance, type ColumnDef, type Row } from './columns';
import { ColumnMenu } from './ColumnMenu';
import { useQueryContext } from './context';
import { useHead } from './data';
import { Grid } from './Grid';
import { cleared } from './chips';
import { RuleSuggestionCard } from '../rules/RuleSuggestion';
import { MergeSuggestionCard } from './MergeSuggestion';
import { Toolbar } from './Toolbar';
import { useGridController } from './controller';

type Props = {
  viewId: string;
  title: string;
  icon: 'layers' | 'folder';
  /** A cash account's view: its toolbar adds transactions by hand (ADR 0029). */
  cash?: { accountId: string; currency: string } | undefined;
};

export function Table({ viewId, title, icon, cash }: Props) {
  const q = useQueryContext(viewId);
  const statColumn = useViews((s) => viewOf(s.views, viewId).statColumn);
  const hidden = useViews((s) => viewOf(s.views, viewId).hidden);
  const search = useViews((s) => viewOf(s.views, viewId).search);
  const selectedId = useViews((s) => viewOf(s.views, viewId).selectedId);
  const patch = useViews((s) => s.patch);
  const head = useHead(q, statColumn);
  const groups = useGroups().data;
  const columns = availableColumns({ groups: !!groups?.length, balance: hasBalance(head.data?.rows) });
  const colorOf = useCallback(
    (id: string) => {
      const g = groups?.find((x) => x.id === id);
      return g && groupColor(g.color);
    },
    [groups],
  );
  const visible = columns.filter((c) => !hidden.includes(c.id));
  const [menu, setMenu] = useState<{ col: ColumnDef; anchor: HTMLElement } | null>(null);
  const ctl = useGridController(viewId);

  // A new result moves rows: the selection's position is re-found in the loaded rows (controller).
  const resultKey = `${q.ctxKey}|${q.sortKey}`;
  const lastKey = useRef(resultKey);
  useEffect(() => {
    if (lastKey.current === resultKey) return;
    lastKey.current = resultKey;
    useSelection.getState().clear(viewId); // a new result drops the multi-selection (legacy refresh)
    if (viewOf(useViews.getState().views, viewId).selectedIndex !== null) patch(viewId, { selectedIndex: null });
  }, [resultKey, viewId, patch]);

  useEffect(
    () => () => {
      useSelection.getState().clear(viewId);
    },
    [viewId],
  );

  const onSelect = useCallback(
    (row: Row, index: number) => {
      ctl.select(row.id === selectedId ? null : row.id, index);
    },
    [ctl, selectedId],
  );

  return (
    <>
      <InsightsPane
        viewId={viewId}
        q={q}
        resolved={head.data?.period}
        dataRange={head.data?.dataRange}
        ready={head.data !== undefined}
      />
      <Toolbar
        viewId={viewId}
        title={title}
        icon={icon}
        q={q}
        total={head.data?.total}
        periodLabel={head.data?.period?.label}
        columns={columns}
        visible={visible}
        cash={cash}
      />
      <MergeSuggestionCard viewId={viewId} />
      <RuleSuggestionCard />
      <Grid
        viewId={viewId}
        q={q}
        head={head.data}
        columns={visible}
        tokens={searchTokens(search)}
        selectedId={selectedId}
        onSelect={onSelect}
        onColumnMenu={(col, anchor) => {
          setMenu({ col, anchor });
        }}
        onClearAll={() => {
          patch(viewId, cleared(viewId));
        }}
        gridRef={ctl.setGrid}
        rowSource={ctl.setRowSource}
        groupColor={colorOf}
      />
      <QuickBar viewId={viewId} currency={head.data?.rows[0]?.amount.currency ?? 'PLN'} />
      {menu && (
        <ColumnMenu
          viewId={viewId}
          q={q}
          col={menu.col}
          anchor={menu.anchor}
          onClose={() => {
            setMenu(null);
          }}
        />
      )}
    </>
  );
}
