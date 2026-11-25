import type { ButtonHTMLAttributes, ReactNode } from 'react';

import { Icon } from './Icon';
import type { IconName } from './icons';

type Props = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'className'> & {
  variant?: 'primary' | 'ghost' | 'danger' | 'outline';
  size?: 'sm';
  icon?: IconName;
  className?: string;
  children?: ReactNode;
};

/**
 * The legacy `.btn` (optionally `.primary`, `.ghost`, `.danger`, `.sm`), with a leading icon. `outline` (Phase 26)
 * is a quiet button with the accent as its border, for a way out of a small surface.
 */
export function Button({ variant, size, icon, className, children, type = 'button', ...rest }: Props) {
  const cls = ['btn', variant, size, className].filter(Boolean).join(' ');
  return (
    <button type={type} className={cls} {...rest}>
      {icon && <Icon name={icon} />}
      {children}
    </button>
  );
}
