// The status bar of the table mode (PLAN §7.6 StatusBar, legacy renderStatus): rows shown of the
// view's total (accent when filtered), visible columns, the period, the skipped duplicates (a link
// to the duplicates window) and the
// selected row on the left; the totals picker and Σ / avg / min / max on the right. It reads the
// same head query as the grid (same key), so it costs no extra request.

import { useEffect } from 'react';

import { useHasDuplicates } from '../api/hooks/duplicates';
import { useGroups } from '../api/hooks/groups';
import { useImports } from '../api/hooks/imports';
import { useDupDialog } from '../duplicates/store';
import { fmtInt, fmtMoney } from '../lib/format';
import { useViews, viewOf } from '../stores/viewStore';
import { Icon } from '../ui/Icon';
import { availableColumns, columnById, hasBalance } from './columns';
import { useQueryContext } from './context';
import { useHead } from './data';

type Props = {
  viewId: string;
  /** Transactions in the view without search, filters or period (the accounts' counts). */
  all: number;
};

type StatColumn = 'amount' | 'balanceAfter';

export function TableStatus({ viewId, all }: Props) {
  const q = useQueryContext(viewId);
  const v = useViews((s) => viewOf(s.views, viewId));
  const patch = useViews((s) => s.patch);
  const head = useHead(q, v.statColumn).data;
  const shown = head?.total;
  const groups = useGroups().data;
  const columns = availableColumns({ groups: !!groups?.length, balance: hasBalance(head?.rows) });
  const visible = columns.filter((c) => !v.hidden.includes(c.id)).length;
  const filtered = shown !== undefined && shown !== all;
  const numbers = columns.filter((c) => c.type === 'number');
  const statColumn: StatColumn = numbers.some((c) => c.id === v.statColumn) ? v.statColumn : 'amount';
  // A stored column the view no longer offers falls back to Amount (legacy renderStatus).
  const stale = !!head && statColumn !== v.statColumn;
  useEffect(() => {
    if (stale) patch(viewId, { statColumn: 'amount' });
  }, [stale, patch, viewId]);
  const statLabel = columnById(statColumn)?.label ?? statColumn;
  const stats = head?.stats?.column === statColumn ? head.stats : undefined;
  // Totals are minor units of the view's currency (an account has one; "all" uses the first row's).
  const currency = head?.rows[0]?.amount.currency ?? 'PLN';
  const money = (n: number | undefined) => (n === undefined ? '–' : fmtMoney({ amountMinor: n, currency }));
  const dupes = head?.duplicatesSkipped ?? 0;
  // With nothing skipped the link stays while decided groups exist (legacy "duplicates reviewed");
  // that needs two imports in the view, so other views never ask.
  const imports = useImports().data ?? [];
  const twoImports =
    imports.filter((i) => i.status === 'committed' && (viewId === 'all' || i.accountId === viewId)).length > 1;
  // An open duplicates window shows the current count as decisions change it.
  useEffect(() => {
    const d = useDupDialog.getState();
    if (d.target && d.target.skipped !== dupes && head) d.open({ ...d.target, skipped: dupes });
  }, [dupes, head]);
  const reviewed =
    useHasDuplicates(viewId === 'all' ? undefined : [viewId], !!head && dupes === 0 && twoImports).data === true;

  return (
    <>
      <div id="stL">
        <span className={filtered ? 'st accent' : 'st'}>
          <Icon name={filtered ? 'filter' : 'table'} />
          <b>{shown === undefined ? '…' : fmtInt(shown)}</b> of {fmtInt(all)} rows
        </span>
        <span className="st">
          {visible} / {columns.length} columns
        </span>
        {head?.period && v.period !== 'all' && <span className="st">{head.period.label}</span>}
        {(dupes > 0 || reviewed) && (
          <span className="st">
            <button
              type="button"
              className="st-link"
              title="Review, compare and un-merge duplicates"
              onClick={() => {
                useDupDialog.getState().open({ accountIds: viewId === 'all' ? undefined : [viewId], skipped: dupes });
              }}
            >
              {dupes > 0 ? `${fmtInt(dupes)} duplicate${dupes === 1 ? '' : 's'} skipped` : 'duplicates reviewed'}
            </button>
          </span>
        )}
        {v.selectedId && v.selectedIndex !== null && <span className="st">Row {fmtInt(v.selectedIndex + 1)}</span>}
      </div>
      <div id="stR">
        {numbers.length > 1 && (
          <span className="st" title="Which number column the totals are calculated for">
            <span className="muted">totals of</span>
            <select
              className="st-sel"
              aria-label="Totals of"
              value={statColumn}
              onChange={(e) => {
                patch(viewId, { statColumn: e.target.value as StatColumn });
              }}
            >
              {numbers.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.label}
                </option>
              ))}
            </select>
          </span>
        )}
        <span className="st" title={`Totals of ${statLabel} for the rows shown`}>
          Σ <b className={(stats?.sum ?? 0) < 0 ? 'neg' : undefined}>{money(stats?.sum)}</b>
        </span>
        <span className="st">
          avg <b>{money(stats?.avg)}</b>
        </span>
        <span className="st">
          min <b>{money(stats?.min)}</b>
        </span>
        <span className="st">
          max <b>{money(stats?.max)}</b>
        </span>
      </div>
    </>
  );
}
