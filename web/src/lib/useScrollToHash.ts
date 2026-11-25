import { useEffect } from 'react';
import { useLocation } from 'react-router';

/**
 * A link to a part of a page (`/settings/security#sessions`, `/shared#friends`) brings that part
 * into view once it is drawn. `ready` is anything that changes when the part appears.
 */
export function useScrollToHash(ready?: unknown): void {
  const { hash } = useLocation();
  useEffect(() => {
    const part = hash.length > 1 ? document.getElementById(hash.slice(1)) : null;
    // (the test environment's elements can't scroll)
    if (part && typeof part.scrollIntoView === 'function') part.scrollIntoView({ block: 'start' });
  }, [hash, ready]);
}
