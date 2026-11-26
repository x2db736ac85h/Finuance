// How hard the model thinks, chosen beside Send (Phase 27). The levels offered are the ones the
// chosen model has: the app knows them per model family (the capabilities the server reports with
// each model; no provider has an API that lists them). A model without a thinking control gets
// no button. The administrator can keep the level (Administration → Assistant): the button then
// says so and offers nothing.

import type { Thinking } from '../stores/prefs';
import { IconButton } from '../ui/IconButton';
import { Menu, MenuItem } from '../ui/Menu';
import { usePopover } from '../ui/Popover';

export type Reasoning = 'none' | 'effort' | 'budget' | 'switch' | 'adaptive';

const LABEL: Record<Thinking, [string, string]> = {
  auto: ['Model default', ''],
  off: ['Off', 'fastest'],
  low: ['Low', ''],
  medium: ['Medium', ''],
  high: ['High', 'slowest, best on hard questions'],
};

/** The levels a kind of thinking control has, quickest first after the default. */
export function thinkLevels(reasoning: Reasoning | undefined): Thinking[] {
  switch (reasoning) {
    case 'switch':
      return ['auto', 'off'];
    case 'effort':
    case 'budget':
    case 'adaptive':
      return ['auto', 'off', 'low', 'medium', 'high'];
    default:
      return [];
  }
}

type Props = {
  reasoning: Reasoning | undefined;
  value: Thinking;
  /** The administrator keeps the level. */
  locked: boolean;
  onChange: (t: Thinking) => void;
};

export function ThinkButton({ reasoning, value, locked, onChange }: Props) {
  const pop = usePopover();
  const levels = thinkLevels(reasoning);
  if (!levels.length) return null;
  // a level the model doesn't have (chosen for another model) runs as its default
  const shown = levels.includes(value) ? value : 'auto';
  if (locked)
    return <IconButton icon="brain" className="think-btn" title="Thinking: set by the administrator" disabled />;
  return (
    <>
      <IconButton
        icon="brain"
        className="think-btn"
        title={`Thinking: ${LABEL[shown][0]}`}
        aria-haspopup="menu"
        aria-expanded={pop.open}
        on={shown !== 'auto'}
        onClick={pop.toggle}
      />
      <Menu open={pop.open} onClose={pop.close} anchor={pop.anchor} label="Thinking" width={250}>
        {levels.map((t) => (
          <MenuItem
            key={t}
            on={t === shown}
            hint={LABEL[t][1]}
            onSelect={() => {
              pop.close();
              onChange(t);
            }}
          >
            {LABEL[t][0]}
          </MenuItem>
        ))}
      </Menu>
    </>
  );
}
