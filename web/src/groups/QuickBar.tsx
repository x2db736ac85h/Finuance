// The quick bar (legacy #qbar, renderQbar): while rows are selected it floats at the bottom of the
// content area with the count, the Σ of their amounts, the groups as dots (add to a group), +
// (new group, then add), Ungroup when any selected row has a group, Category and Transfer menus
// (Phase 15), and close. Toasts move up while it is shown.

import { useEffect, useState } from 'react';

import { useAssignGroup, useGroups, type Group } from '../api/hooks/groups';
import { useBulkClassify, type BulkAction } from '../api/hooks/rules';
import { useCategories } from '../api/hooks/transactions';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { selectedIn, useSelection } from '../stores/selectionStore';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { Menu, MenuItem } from '../ui/Menu';
import { usePopover } from '../ui/Popover';
import { toast } from '../ui/toastStore';
import { GroupDots } from './GroupDots';
import { GroupForm } from './GroupForm';

type Props = { viewId: string; currency: string };

export function QuickBar({ viewId, currency }: Props) {
  const rows = useSelection((s) => selectedIn(s, viewId));
  const loading = useSelection((s) => s.viewId === viewId && s.loading);
  const groups = useGroups().data ?? [];
  const assign = useAssignGroup();
  const [form, setForm] = useState<HTMLElement | null>(null);
  const classify = useBulkClassify();
  const cats = useCategories().data ?? [];
  const catMenu = usePopover();
  const xferMenu = usePopover();
  const shown = rows.size > 0;

  useEffect(() => {
    document.body.classList.toggle('has-qbar', shown);
    return () => {
      document.body.classList.remove('has-qbar');
    };
  }, [shown]);

  if (!shown) return null;
  let sum = 0;
  let grouped = false;
  for (const r of rows.values()) {
    sum += r.amountMinor;
    grouped ||= r.grouped;
  }

  const apply = (g: Group | null) => {
    const ids = [...rows.keys()];
    assign.mutate(
      { ids, groupId: g?.id ?? null },
      {
        onSuccess: () => {
          useSelection.getState().clear(viewId);
          toast(
            g
              ? `${plural(ids.length, 'transaction')} added to “${g.name}”`
              : `${plural(ids.length, 'transaction')} removed from their group`,
          );
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };

  const run = (action: BulkAction, done: string) => {
    catMenu.close();
    xferMenu.close();
    const ids = [...rows.keys()];
    classify.mutate(
      { ids, ...action },
      {
        onSuccess: (r) => {
          useSelection.getState().clear(viewId);
          toast(`${plural(r.updated, 'transaction')} ${done}`);
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  const busy = loading || assign.isPending || classify.isPending;

  return (
    <div className="qbar" role="toolbar" aria-label="Selected transactions">
      <span className="qn" aria-live="polite">
        <b>{fmtInt(rows.size)}</b> selected ·{' '}
        <span className={sum < 0 ? 'neg' : undefined}>{fmtMoney({ amountMinor: sum, currency })}</span>
      </span>
      <span className="qsep" />
      <span className="ql">Add to group</span>
      <GroupDots
        groups={groups}
        small
        disabled={loading || assign.isPending}
        onPick={apply}
        onNew={(anchor) => {
          setForm(anchor);
        }}
      />
      {grouped && (
        <>
          <span className="qsep" />
          <Button
            size="sm"
            variant="ghost"
            title="Remove the selected transactions from their group"
            disabled={loading || assign.isPending}
            onClick={() => {
              apply(null);
            }}
          >
            Ungroup
          </Button>
        </>
      )}
      <span className="qsep" />
      <Button
        size="sm"
        variant="ghost"
        icon="tag"
        disabled={busy}
        onClick={catMenu.toggle}
        aria-expanded={catMenu.open}
      >
        Category…
      </Button>
      <Button
        size="sm"
        variant="ghost"
        icon="right"
        disabled={busy}
        onClick={xferMenu.toggle}
        aria-expanded={xferMenu.open}
      >
        Transfer…
      </Button>
      <Menu open={catMenu.open} anchor={catMenu.anchor} onClose={catMenu.close} label="Set category" width={240}>
        {cats.map((c) => (
          <MenuItem
            key={c.id}
            onSelect={() => {
              run({ category: { id: c.id } }, `set to “${c.name}”`);
            }}
          >
            {c.name}
          </MenuItem>
        ))}
        <MenuItem
          icon="left"
          onSelect={() => {
            run({ category: { reset: true } }, "back to the bank's category");
          }}
        >
          Use the bank&apos;s category
        </MenuItem>
      </Menu>
      <Menu open={xferMenu.open} anchor={xferMenu.anchor} onClose={xferMenu.close} label="Transfer" width={240}>
        <MenuItem
          onSelect={() => {
            run({ transfer: 'yes' }, 'marked as transfers');
          }}
        >
          Mark as transfer
        </MenuItem>
        <MenuItem
          onSelect={() => {
            run({ transfer: 'no' }, 'marked as not transfers');
          }}
        >
          Not a transfer
        </MenuItem>
        <MenuItem
          onSelect={() => {
            run({ transfer: 'auto' }, 'back to automatic');
          }}
        >
          Automatic
        </MenuItem>
      </Menu>
      <span className="qsep" />
      <IconButton
        icon="x"
        size="sm"
        title="Clear selection (Esc)"
        onClick={() => {
          useSelection.getState().clear(viewId);
        }}
      />
      {form && (
        <GroupForm
          anchor={form}
          onClose={() => {
            setForm(null);
          }}
          onDone={apply}
        />
      )}
    </div>
  );
}
