import { Fragment } from 'react';

/** Mac keyboards show ⌘, everything else Ctrl (legacy MODK). */
export const MOD = /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl';

/** A shortcut as one short label: ⌘F on macOS, Ctrl+F elsewhere. */
export function modKey(key: string): string {
  return MOD === '⌘' ? `⌘${key}` : `Ctrl+${key}`;
}

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
