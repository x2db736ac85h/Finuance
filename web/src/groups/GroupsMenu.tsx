// The toolbar's Groups button and menu (legacy #grpBtn, openGroupsMenu): every group with its colour,
// count and money out, and its budget's progress when it has one (PLAN 15.9); clicking one shows
// only its transactions (the Group column's `=Name` filter), again shows everything. The chart
// button opens the group window, ✎ renames, recolours or budgets, × deletes the group (labels
// only, after a confirmation), New group, Show all.

import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useDeleteGroup, useGroups, type Group } from '../api/hooks/groups';
import { useMe, useMeta } from '../api/hooks/me';
import { ShareDialog } from '../shared/dialogs';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { without } from '../lib/objects';
import { useViews, viewOf } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { Modal } from '../ui/Modal';
import { Popover } from '../ui/Popover';
import { toast } from '../ui/toastStore';
import { groupColor } from './colors';
import { activeGroup, renameGroupFilters, toggleGroupFilter } from './filters';
import { GroupForm } from './GroupForm';
import { BudgetBar, GroupReportWindow } from './GroupReport';

export function GroupsButton({ viewId }: { viewId: string }) {
  const [btn, setBtn] = useState<HTMLButtonElement | null>(null);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<{ group?: Group } | null>(null);
  const [del, setDel] = useState<Group | null>(null);
  const [report, setReport] = useState<Group | null>(null);
  const [share, setShare] = useState<Group | null>(null);
  const groups = useGroups().data ?? [];
  const navigate = useNavigate();
  // Share… (Phase 19): only where shared groups are on and never in the demo
  const sharedOn = useMeta().data?.features.sharedGroups !== false;
  const isDemo = useMe().data?.isDemo === true;
  const canShare = sharedOn && !isDemo;
  const cond = useViews((s) => viewOf(s.views, viewId).filters.group?.cond ?? '');
  const active = cond.startsWith('=') ? cond.slice(1) : null;
  const patch = useViews((s) => s.patch);

  return (
    <>
      <button
        ref={setBtn}
        type="button"
        className={open ? 'btn on' : 'btn'}
        title="Transaction groups (e.g. a trip)"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <Icon name="tag" />
        <span className="bl">Groups</span>
        {groups.length > 0 && <span className="cnt">{groups.length}</span>}
      </button>
      {open && !form && (
        <Popover
          open
          anchor={btn}
          onClose={() => {
            setOpen(false);
          }}
          label="Groups"
          width={320}
        >
          <div className="pop-h">
            <Icon name="tag" />
            <b>Groups</b>
            <span className="muted">{groups.length}</span>
          </div>
          <div className="pop-s">
            {groups.length ? (
              groups.map((g) => (
                <div
                  key={g.id}
                  className={`gr${active === g.name ? ' on' : ''}${g.budgetMinor !== undefined ? ' has-bud' : ''}`}
                >
                  <button
                    type="button"
                    className="gr-main"
                    aria-pressed={active === g.name}
                    title={active === g.name ? 'Show all transactions' : 'Show only this group'}
                    onClick={() => {
                      toggleGroupFilter(viewId, g.name);
                      setOpen(false);
                    }}
                  >
                    <i className="gdot-i" style={{ background: groupColor(g.color) }} />
                    <span className="gn">{g.name}</span>
                    {g.sharedGroupId && <span className="sh-badge">shared</span>}
                    <span className="gc">
                      {fmtInt(g.count)} · {fmtMoney(g.spent, { whole: true })}
                    </span>
                  </button>
                  <IconButton
                    icon="chart"
                    size="sm"
                    title={`Report for ${g.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setOpen(false);
                      setReport(g);
                    }}
                  />
                  {canShare && (
                    <IconButton
                      icon="swap"
                      size="sm"
                      title={g.sharedGroupId ? `Open the shared group ${g.name}` : `Share ${g.name}…`}
                      onClick={(e) => {
                        e.stopPropagation();
                        setOpen(false);
                        if (g.sharedGroupId) void navigate(`/s/${g.sharedGroupId}`);
                        else setShare(g);
                      }}
                    />
                  )}
                  <IconButton
                    icon="pen"
                    size="sm"
                    title={`Edit ${g.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setForm({ group: g });
                    }}
                  />
                  <IconButton
                    icon="x"
                    size="sm"
                    title={`Delete ${g.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setOpen(false);
                      setDel(g);
                    }}
                  />
                  {g.budgetMinor !== undefined && (
                    <BudgetBar spent={g.spent.amountMinor} budget={g.budgetMinor} cur={g.spent.currency} />
                  )}
                </div>
              ))
            ) : (
              <div className="hint gr-empty">
                No groups yet. Create one, then add transactions: open a transaction and pick its colour dot, or drag
                across rows (or ⌘/Ctrl-click, Shift-click) and use the quick bar.
              </div>
            )}
          </div>
          <div className="pop-f">
            {active ? (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  patch(viewId, { filters: without(viewOf(useViews.getState().views, viewId).filters, 'group') });
                  setOpen(false);
                }}
              >
                Show all
              </Button>
            ) : (
              <span className="hint">Click a group to show only its transactions</span>
            )}
            <Button
              size="sm"
              icon="plus"
              onClick={() => {
                setForm({});
              }}
            >
              New group
            </Button>
          </div>
        </Popover>
      )}
      {form && (
        <GroupForm
          anchor={btn}
          {...(form.group ? { group: form.group } : {})}
          onClose={() => {
            setForm(null);
          }}
        />
      )}
      {report && (
        <GroupReportWindow
          group={report}
          onClose={() => {
            setReport(null);
          }}
          onShow={() => {
            if (activeGroup(viewId) !== report.name) toggleGroupFilter(viewId, report.name);
            setReport(null);
          }}
        />
      )}
      {share && (
        <ShareDialog
          from={{ id: share.id, name: share.name, color: share.color, currency: share.spent.currency }}
          onClose={() => {
            setShare(null);
          }}
        />
      )}
      <DeleteGroup
        group={del}
        onClose={() => {
          setDel(null);
        }}
      />
    </>
  );
}

function DeleteGroup({ group, onClose }: { group: Group | null; onClose: () => void }) {
  const del = useDeleteGroup();
  return (
    <Modal open={group !== null} onClose={onClose} title="Delete group" size="sm">
      <div className="modal-form">
        <p>
          Delete the group <b>{group?.name}</b>? No transactions are deleted —{' '}
          {plural(group?.count ?? 0, 'transaction')} only lose this group label.
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => {
              if (!group) return;
              del.mutate(group.id, {
                onSuccess: () => {
                  renameGroupFilters(group.name, null);
                  toast(`Group “${group.name}” deleted — transactions kept`);
                  onClose();
                },
                onError: (e) => {
                  toast(errorMessage(e), { kind: 'error' });
                },
              });
            }}
          >
            Delete group
          </Button>
        </div>
      </div>
    </Modal>
  );
}
