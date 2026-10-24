// Settings → Rules (PLAN task 15.2): the rules in the order they apply, with drag or arrow
// reordering, enable/disable, edit, delete, and "Apply rules now".

import { useState, type DragEvent } from 'react';

import { useApplyRules, useDeleteRule, useReorderRules, useRules, useUpdateRule, type Rule } from '../api/hooks/rules';
import { moved } from '../insights/ChartsMenu';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { draftOf, emptyDraft, matchOf, RuleEditor, ruleSummary } from './RuleEditor';

type Editing = { rule?: Rule } | null;

export function RulesSettings() {
  const rules = useRules();
  const reorder = useReorderRules();
  const update = useUpdateRule();
  const apply = useApplyRules();
  const [editing, setEditing] = useState<Editing>(null);
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const [drag, setDrag] = useState<string | null>(null);
  const list = rules.data ?? [];
  const setOrder = (ids: string[]) => {
    reorder.mutate(ids, {
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  const ids = list.map((r) => r.id);
  const half = (e: DragEvent<HTMLElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    return e.clientY >= r.top + r.height / 2;
  };
  const toggle = (r: Rule, enabled: boolean) => {
    const d = draftOf(r);
    update.mutate(
      {
        id: r.id,
        name: d.name,
        enabled,
        match: matchOf(d),
        ...(d.setCategoryId ? { setCategoryId: d.setCategoryId } : {}),
        markTransfer: d.markTransfer,
      },
      {
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  return (
    <>
      <div className="set-card">
        <div className="set-row">
          <div className="lbl grow">Rules</div>
          <Button
            disabled={!list.length || apply.isPending}
            title="Apply every rule again and look for transfers between your accounts"
            onClick={() => {
              apply.mutate(undefined, {
                onSuccess: () => {
                  toast('Applying rules…');
                },
                onError: (e) => {
                  toast(errorMessage(e), { kind: 'error' });
                },
              });
            }}
          >
            Apply rules now
          </Button>
          <Button
            variant="primary"
            icon="plus"
            onClick={() => {
              setEditing({});
            }}
          >
            New rule
          </Button>
        </div>
        <p className="hint">
          Rules set a category or mark transfers, on every account and all time. They apply from top to bottom, so a
          lower rule wins over a higher one; categories you set by hand are never changed. Rules run again after every
          import. Tip: filter the table and use <b>Save filters as rule…</b> in the toolbar.
        </p>
        {rules.isError && <p className="set-err">{errorMessage(rules.error)}</p>}
        {rules.isSuccess && !list.length && <p className="hint">No rules yet.</p>}
        <ul className="set-list rules-list" aria-label="Rules">
          {list.map((r, j) => (
            <li
              key={r.id}
              className={drag === r.id ? 'dragging' : undefined}
              draggable
              onDragStart={(e) => {
                setDrag(r.id);
                e.dataTransfer.effectAllowed = 'move';
                e.dataTransfer.setData('text/plain', r.id);
              }}
              onDragOver={(e) => {
                if (drag) e.preventDefault();
              }}
              onDrop={(e) => {
                e.preventDefault();
                if (drag && drag !== r.id) {
                  const rest = ids.filter((x) => x !== drag);
                  setOrder(moved(ids, drag, rest.indexOf(r.id) + (half(e) ? 1 : 0)));
                }
                setDrag(null);
              }}
              onDragEnd={() => {
                setDrag(null);
              }}
            >
              <span className="grip" title="Drag to reorder" aria-hidden="true">
                ⠿
              </span>
              <input
                type="checkbox"
                aria-label={`Enable ${r.name}`}
                checked={r.enabled}
                onChange={(e) => {
                  toggle(r, e.target.checked);
                }}
              />
              <div className={`set-li-main${r.enabled ? '' : ' off'}`}>
                <b>{r.name}</b>
                <span className="hint">{ruleSummary(r)}</span>
              </div>
              <IconButton
                icon="up"
                size="sm"
                title={`Move ${r.name} up`}
                disabled={j === 0 || reorder.isPending}
                onClick={() => {
                  setOrder(moved(ids, r.id, j - 1));
                }}
              />
              <IconButton
                icon="down"
                size="sm"
                title={`Move ${r.name} down`}
                disabled={j === list.length - 1 || reorder.isPending}
                onClick={() => {
                  setOrder(moved(ids, r.id, j + 1));
                }}
              />
              <Button
                size="sm"
                aria-label={`Edit ${r.name}`}
                onClick={() => {
                  setEditing({ rule: r });
                }}
              >
                Edit
              </Button>
              <Button
                size="sm"
                variant="ghost"
                aria-label={`Delete ${r.name}`}
                onClick={() => {
                  setDeleting(r);
                }}
              >
                Delete
              </Button>
            </li>
          ))}
        </ul>
      </div>
      {editing && (
        <RuleEditor
          title={editing.rule ? 'Edit rule' : 'New rule'}
          initial={editing.rule ? draftOf(editing.rule) : emptyDraft()}
          {...(editing.rule ? { rule: editing.rule } : {})}
          onClose={() => {
            setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteRule
          rule={deleting}
          onClose={() => {
            setDeleting(null);
          }}
        />
      )}
    </>
  );
}

function DeleteRule({ rule, onClose }: { rule: Rule; onClose: () => void }) {
  const del = useDeleteRule();
  return (
    <Modal open onClose={onClose} title="Delete rule" size="sm">
      <div className="modal-form">
        <p>
          Delete the rule <b>{rule.name}</b>? The transactions it categorised go back to the bank&apos;s category (or
          another matching rule&apos;s).
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => {
              del.mutate(rule.id, {
                onSuccess: () => {
                  toast(`Rule “${rule.name}” deleted`);
                  onClose();
                },
                onError: (e) => {
                  toast(errorMessage(e), { kind: 'error' });
                },
              });
            }}
          >
            Delete rule
          </Button>
        </div>
      </div>
    </Modal>
  );
}
