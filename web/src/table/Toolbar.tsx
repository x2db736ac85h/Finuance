// The toolbar above the grid (PLAN §7.6, legacy .toolbar): the view's title and size, removable
// chips with Clear all, Save filters as rule, and the Groups, Columns and Export buttons.

import { useState } from 'react';

import { exportCsv } from '../api/hooks/transactions';
import { errorMessage } from '../lib/errors';
import { fmtInt, plural } from '../lib/format';
import { useViews, viewOf, type ViewState } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { usePopover } from '../ui/Popover';
import { toast } from '../ui/toastStore';
import { chipsOf, cleared, withoutChip } from './chips';
import { columnById, type ColumnDef, type ColumnId } from './columns';
import { ColumnsMenu } from './ColumnsMenu';
import { GroupsButton } from '../groups/GroupsMenu';
import { RuleEditor, type RuleDraft } from '../rules/RuleEditor';
import type { ViewQuery } from './context';
import { useCashDialogs } from '../cash/store';

type Props = {
  viewId: string;
  title: string;
  icon: 'layers' | 'folder';
  q: ViewQuery;
  total: number | undefined;
  periodLabel: string | undefined;
  columns: readonly ColumnDef[];
  visible: readonly ColumnDef[];
  cash?: { accountId: string; currency: string } | undefined;
};

/** Saves a Blob under a file name (an <a download> made with the DOM API, never HTML strings). */
export function saveBlob(blob: Blob, filename: string): void {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = filename;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => {
    URL.revokeObjectURL(a.href);
  }, 2000);
}

export function Toolbar({ viewId, title, icon, q, total, periodLabel, columns, visible, cash }: Props) {
  const v = useViews((s) => viewOf(s.views, viewId));
  const patch = useViews((s) => s.patch);
  const cols = usePopover();
  const [exporting, setExporting] = useState(false);
  const [ruleDraft, setRuleDraft] = useState<RuleDraft | null>(null);
  const chips = chipsOf(viewId, v, periodLabel);
  const draft = ruleDraftOf(v);

  const doExport = async () => {
    setExporting(true);
    try {
      const { blob, filename } = await exportCsv({
        context: q.context,
        ...(q.sort ? { sort: q.sort } : {}),
        columns: visible.map((c) => c.id),
        delimiter: ';',
        view:
          title
            .replace(/[^A-Za-z0-9_-]+/g, '-')
            .replace(/^-+|-+$/g, '')
            .toLowerCase() || 'transactions',
      });
      saveBlob(blob, filename);
      toast(`Exported ${plural(total ?? 0, 'row')}`);
    } catch (e) {
      toast(errorMessage(e), { kind: 'error' });
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="toolbar">
      <div className="vt-title">
        <Icon name={icon} />
        <span>{title}</span>
      </div>
      <span className="vt-meta">{total === undefined ? '' : `${fmtInt(total)} × ${String(visible.length)}`}</span>
      <div className="chips">
        {chips.map((c) => (
          <span key={c.key} className="chip">
            {c.icon && <Icon name={c.icon} />}
            {c.label && `${c.label} `}
            <b>{c.value}</b>
            <IconButton
              icon="x"
              title="Remove"
              aria-label={`Remove ${c.label ? `${c.label} ` : ''}${c.value}`}
              onClick={() => {
                patch(viewId, withoutChip(viewId, v, c.key));
              }}
            />
          </span>
        ))}
        {chips.length > 0 && (
          <button
            type="button"
            className="link"
            onClick={() => {
              patch(viewId, cleared(viewId));
            }}
          >
            Clear all
          </button>
        )}
      </div>
      <div className="tools">
        {cash && (
          <Button
            variant="primary"
            icon="plus"
            title="Add a transaction to this cash account (N)"
            onClick={() => {
              useCashDialogs.getState().openTransaction(cash);
            }}
          >
            Add
          </Button>
        )}
        {draft && (
          <Button
            icon="sliders"
            title="Turn this view's search and filters into a rule that sets a category"
            onClick={() => {
              setRuleDraft(draft);
            }}
          >
            Save filters as rule…
          </Button>
        )}
        <GroupsButton viewId={viewId} />
        <Button icon="columns" onClick={cols.toggle} aria-expanded={cols.open}>
          Columns
        </Button>
        <Button
          icon="download"
          onClick={() => void doExport()}
          disabled={exporting || !visible.length || !total}
          title="Export the rows shown (filters, sort and visible columns) as CSV"
        >
          Export
        </Button>
      </div>
      {cols.anchor && <ColumnsMenu viewId={viewId} columns={columns} anchor={cols.anchor} onClose={cols.close} />}
      {ruleDraft && (
        <RuleEditor
          title="New rule from filters"
          initial={ruleDraft}
          onClose={() => {
            setRuleDraft(null);
          }}
        />
      )}
    </div>
  );
}

/**
 * A rule draft from the view's search and column filters (the period, sort and accounts don't go
 * into rules: a rule applies to every account, all time); null when there is nothing to match.
 */
export function ruleDraftOf(v: Pick<ViewState, 'search' | 'filters'>): RuleDraft | null {
  const filters = Object.entries(v.filters)
    .filter(([, f]) => f && (f.cond.trim() || f.excluded.length))
    .map(([column, f]) => ({ column: column as ColumnId, cond: f?.cond.trim() ?? '', excluded: f?.excluded ?? [] }));
  const search = v.search.trim();
  if (!search && !filters.length) return null;
  const first = filters.find((f) => f.cond);
  const name = first ? `${columnById(first.column)?.label ?? first.column} ${first.cond}` : search;
  return { name: name.slice(0, 80), enabled: true, search, filters, setCategoryId: '', markTransfer: false };
}
