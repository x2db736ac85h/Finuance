import type { ButtonHTMLAttributes } from 'react';

import { Icon } from './Icon';
import type { IconName } from './icons';

type Props = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'className' | 'title'> & {
  icon: IconName;
  /** Tooltip and accessible name. */
  title: string;
  on?: boolean;
  size?: 'sm';
  className?: string;
};

/** The legacy square icon button `.ib` (`.on` when its panel is shown). */
export function IconButton({ icon, title, on, size, className, type = 'button', ...rest }: Props) {
  const cls = ['ib', size, on && 'on', className].filter(Boolean).join(' ');
  return (
    <button type={type} className={cls} title={title} aria-label={title} {...rest}>
      <Icon name={icon} />
    </button>
  );
}
