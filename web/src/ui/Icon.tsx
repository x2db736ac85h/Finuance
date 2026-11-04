import { createElement } from 'react';

import { ALL_ICONS, type IconName } from './icons';

/** An icon (PLAN §7.1; the legacy set plus APP_ICONS): `<span class="ico"><svg …/></span>`, stroked with currentColor. */
export function Icon({ name, className }: { name: IconName; className?: string }) {
  return (
    <span className={className ? `ico ${className}` : 'ico'} aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        {ALL_ICONS[name].map(([tag, attrs], i) => createElement(tag, { key: i, ...attrs }))}
      </svg>
    </span>
  );
}
