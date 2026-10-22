// A popover menu of `.mi` items, operable with the keyboard: ↑/↓/Home/End move between items,
// Enter or Space activates, Esc closes (Popover).

import type { KeyboardEvent, ReactNode } from 'react';

import { Icon } from './Icon';
import type { IconName } from './icons';
import { Popover } from './Popover';

type MenuProps = {
  open: boolean;
  onClose: () => void;
  anchor: Element | null;
  label: string;
  width?: number;
  /** Shown above the items (a `.pop-h`). */
  header?: ReactNode;
  children: ReactNode;
};

export function Menu({ children, header, ...rest }: MenuProps) {
  return (
    <Popover {...rest} role="menu" onKeyDown={moveFocus}>
      {header}
      <div className="pop-s pop-scroll">{children}</div>
    </Popover>
  );
}

function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  const items = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)')];
  if (!items.length) return;
  const at = items.indexOf(document.activeElement as HTMLElement);
  const go = (i: number) => {
    e.preventDefault();
    items[(i + items.length) % items.length]?.focus();
  };
  if (e.key === 'ArrowDown') go(at + 1);
  else if (e.key === 'ArrowUp') go(at < 0 ? -1 : at - 1);
  else if (e.key === 'Home') go(0);
  else if (e.key === 'End') go(-1);
}

type ItemProps = {
  icon?: IconName;
  hint?: ReactNode;
  on?: boolean;
  disabled?: boolean;
  onSelect: () => void;
  children: ReactNode;
};

export function MenuItem({ icon, hint, on, disabled, onSelect, children }: ItemProps) {
  return (
    <button type="button" role="menuitem" className={on ? 'mi on' : 'mi'} disabled={disabled} onClick={onSelect}>
      {icon && <Icon name={icon} />}
      <span>{children}</span>
      {hint !== undefined && <span className="hint">{hint}</span>}
    </button>
  );
}
