// The toolbar's Groups button and menu (legacy #grpBtn, openGroupsMenu): every group with its colour,
// count and money out; clicking one shows only its transactions (the Group column's `=Name`
// filter), again shows everything. ✎ renames or recolours, × deletes the group (labels only, after
// a confirmation), New group, Show all.

import { useState } from 'react';

import { useDeleteGroup, useGroups, type Group } from '../api/hooks/groups';
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
import { renameGroupFilters, toggleGroupFilter } from './filters';
import { GroupForm } from './GroupForm';

export function GroupsButton({ viewId }: { viewId: string }) {
  const [btn, setBtn] = useState<HTMLButtonElement | null>(null);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<{ group?: Group } | null>(null);
  const [del, setDel] = useState<Group | null>(null);
  const groups = useGroups().data ?? [];
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
                <div key={g.id} className={active === g.name ? 'gr on' : 'gr'}>
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
                    <span className="gc">
                      {fmtInt(g.count)} · {fmtMoney(g.spent, { whole: true })}
                    </span>
                  </button>
                  <IconButton
                    icon="pen"
                    size="sm"
                    title={`Rename or recolour ${g.name}`}
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
