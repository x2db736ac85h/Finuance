// New / edit group (legacy openGroupForm): a name (unique, at most 40 characters), one of eight
// colours and an optional budget (PLAN 15.9, e.g. for a trip); Enter submits. Renaming moves view
// filters to the new name.

import { useState } from 'react';

import { useCreateGroup, useGroups, useUpdateGroup, type Group } from '../api/hooks/groups';
import { errorMessage } from '../lib/errors';
import { parseMoneyInput } from '../lib/format';
import { inputText } from '../overview/Budgets';
import { norm } from '../lib/text';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { TextInput } from '../ui/inputs';
import { Popover } from '../ui/Popover';
import { toast } from '../ui/toastStore';
import { colorVar, GROUP_COLORS, nextColor } from './colors';
import { renameGroupFilters } from './filters';

type Props = {
  anchor: Element | null;
  /** The group to edit; none creates one. */
  group?: Group;
  onClose: () => void;
  /** After a successful save, with the saved group (e.g. to assign it). */
  onDone?: (g: Group) => void;
};

export function GroupForm({ anchor, group, onClose, onDone }: Props) {
  const groups = useGroups().data ?? [];
  const create = useCreateGroup();
  const update = useUpdateGroup();
  const [name, setName] = useState(group?.name ?? '');
  const [color, setColor] = useState(() => group?.color ?? nextColor(groups.map((g) => g.color)));
  const cur = group?.spent.currency ?? groups[0]?.spent.currency ?? 'PLN';
  const [budget, setBudget] = useState(group?.budgetMinor ? inputText(group.budgetMinor, cur) : '');
  const busy = create.isPending || update.isPending;

  const submit = () => {
    const clean = name.trim().replace(/\s+/g, ' ');
    if (!clean) {
      toast('Enter a group name');
      return;
    }
    if (groups.some((g) => g.id !== group?.id && norm(g.name) === norm(clean))) {
      toast('A group with this name already exists');
      return;
    }
    // empty: no budget (removes one the group had); otherwise a positive amount
    const parsed = budget.trim() ? parseMoneyInput(budget, cur) : 0;
    if (parsed === null || parsed < 0) {
      toast('Enter the budget as an amount, e.g. 2500, or leave it empty');
      return;
    }
    const budgetMinor = parsed || group?.budgetMinor ? { budgetMinor: parsed } : {};
    const done = (g: Group) => {
      if (group && group.name !== g.name) renameGroupFilters(group.name, g.name);
      onClose();
      onDone?.(g);
    };
    const fail = (e: unknown) => {
      toast(errorMessage(e), { kind: 'error' });
    };
    if (group) update.mutate({ id: group.id, name: clean, color, ...budgetMinor }, { onSuccess: done, onError: fail });
    else create.mutate({ name: clean, color, ...budgetMinor }, { onSuccess: done, onError: fail });
  };

  return (
    <Popover
      open
      anchor={anchor}
      onClose={onClose}
      label={group ? 'Edit group' : 'New group'}
      width={270}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          submit();
        }
      }}
    >
      <div className="pop-h">
        <Icon name="tag" />
        <b>{group ? 'Edit group' : 'New group'}</b>
      </div>
      <div className="pop-s">
        <TextInput
          aria-label="Group name"
          placeholder="Name, e.g. Italy trip 2025"
          value={name}
          maxLength={40}
          autoComplete="off"
          spellCheck={false}
          autoFocus
          onFocus={(e) => {
            e.currentTarget.select();
          }}
          onChange={(e) => {
            setName(e.target.value);
          }}
        />
        <div className="gpal" role="radiogroup" aria-label="Colour">
          {GROUP_COLORS.map((_, k) => (
            <button
              key={k}
              type="button"
              role="radio"
              aria-checked={k === color}
              aria-label={`Colour ${String(k + 1)}`}
              title={`Colour ${String(k + 1)}`}
              className={k === color ? 'gsw on' : 'gsw'}
              style={colorVar(k)}
              onClick={() => {
                setColor(k);
              }}
            />
          ))}
        </div>
        <label className="gbud">
          <span>Budget ({cur})</span>
          <TextInput
            aria-label="Group budget"
            placeholder="optional, e.g. 2500"
            inputMode="decimal"
            value={budget}
            onChange={(e) => {
              setBudget(e.target.value);
            }}
          />
        </label>
      </div>
      <div className="pop-f">
        <span className="hint">{group ? 'Renaming keeps all its transactions.' : 'Pick a name and a colour.'}</span>
        <Button size="sm" variant="primary" disabled={busy} onClick={submit}>
          {group ? 'Save' : 'Create'}
        </Button>
      </div>
    </Popover>
  );
}
