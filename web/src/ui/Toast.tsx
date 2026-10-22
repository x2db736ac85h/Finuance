import { useEffect, useState } from 'react';

import { useToasts } from './toastStore';

/** Fade-out time of `.toast` (legacy transition 0.18 s). */
const FADE_MS = 180;

/** Renders the head of the toast queue as the legacy `.toast`. Mount once. */
export function Toaster() {
  const head = useToasts((s) => s.queue[0]);
  const shift = useToasts((s) => s.shift);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    if (!head) return;
    const t0 = setTimeout(() => {
      setShown(true);
    }, 0);
    const t1 = setTimeout(() => {
      setShown(false);
    }, head.ms);
    const t2 = setTimeout(shift, head.ms + FADE_MS);
    return () => {
      clearTimeout(t0);
      clearTimeout(t1);
      clearTimeout(t2);
    };
  }, [head, shift]);

  return (
    <div
      className={['toast', head?.kind === 'error' && 'err', head && shown && 'on'].filter(Boolean).join(' ')}
      role={head?.kind === 'error' ? 'alert' : 'status'}
      aria-live={head?.kind === 'error' ? 'assertive' : 'polite'}
    >
      {head?.msg}
    </div>
  );
}
