import { createElement } from 'react';

import { ICONS, type IconName } from './icons';

/** A legacy icon (PLAN §7.1): `<span class="ico"><svg …/></span>`, stroked with currentColor. */
export function Icon({ name, className }: { name: IconName; className?: string }) {
  return (
    <span className={className ? `ico ${className}` : 'ico'} aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        {ICONS[name].map(([tag, attrs], i) => createElement(tag, { key: i, ...attrs }))}
      </svg>
    </span>
  );
}
