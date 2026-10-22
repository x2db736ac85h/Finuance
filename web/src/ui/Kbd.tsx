import { Fragment } from 'react';

/** Mac keyboards show ⌘, everything else Ctrl (legacy MODK). */
export const MOD = /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl';

/** A key or a key combination: ['⌘', 'F'] → ⌘ + F. */
export function Kbd({ keys }: { keys: readonly string[] }) {
  return (
    <>
      {keys.map((k, i) => (
        <Fragment key={i}>
          {i > 0 && <span className="plus">+</span>}
          <kbd>{k}</kbd>
        </Fragment>
      ))}
    </>
  );
}
