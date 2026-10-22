// A pill per group plus "New" (legacy groupPicker): the inspector's Group row and the quick bar's
// small dots. The current group is outlined; clicking it removes the label.

import type { Group } from '../api/hooks/groups';
import { Icon } from '../ui/Icon';
import { colorVar } from './colors';

type Props = {
  groups: readonly Group[];
  /** The current group's id (inspector). */
  current?: string | undefined;
  small?: boolean;
  disabled?: boolean;
  onPick: (g: Group) => void;
  onNew: (anchor: HTMLElement) => void;
};

export function GroupDots({ groups, current, small, disabled, onPick, onNew }: Props) {
  const size = small ? ' sm' : '';
  return (
    <>
      {groups.map((g) => {
        const on = g.id === current;
        return (
          <button
            key={g.id}
            type="button"
            className={`gdot${size}${on ? ' on' : ''}`}
            style={colorVar(g.color)}
            title={on ? `${g.name} — click to remove` : g.name}
            aria-label={small ? `Add to ${g.name}` : g.name}
            aria-pressed={small ? undefined : on}
            disabled={disabled}
            onClick={() => {
              onPick(g);
            }}
          >
            <i />
            {!small && <span>{g.name}</span>}
          </button>
        );
      })}
      <button
        type="button"
        className={`gdot add${size}`}
        title="New group…"
        aria-label="New group"
        disabled={disabled}
        onClick={(e) => {
          onNew(e.currentTarget);
        }}
      >
        <Icon name="plus" />
        {!small && <span>New</span>}
      </button>
    </>
  );
}
