import { useEffect, useState } from 'react';

import { useToasts } from './toastStore';

/** Fade-out time of `.toast` (legacy transition 0.18 s). */
const FADE_MS = 180;
/** How long a toast stays once another one is waiting. */
const SHORT_MS = 1200;

/** Renders the head of the toast queue as the legacy `.toast`. Mount once. */
export function Toaster() {
  const head = useToasts((s) => s.queue[0]);
  const waiting = useToasts((s) => s.queue.length > 1);
  const shift = useToasts((s) => s.shift);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    if (!head) return;
    // Another toast is waiting: this one gets a short moment (actions stay clickable), as the
    // legacy toast was replaced at once by a newer message.
    const ms = waiting ? Math.min(head.ms, SHORT_MS) : head.ms;
    const t0 = setTimeout(() => {
      setShown(true);
    }, 0);
    const t1 = setTimeout(() => {
      setShown(false);
    }, ms);
    const t2 = setTimeout(shift, ms + FADE_MS);
    return () => {
      clearTimeout(t0);
      clearTimeout(t1);
      clearTimeout(t2);
    };
  }, [head, shift, waiting]);

  return (
    <div
      className={['toast', head?.kind === 'error' && 'err', head?.actions.length && 'act', head && shown && 'on']
        .filter(Boolean)
        .join(' ')}
      role={head?.kind === 'error' ? 'alert' : 'status'}
      aria-live={head?.kind === 'error' ? 'assertive' : 'polite'}
    >
      {head?.msg}
      {head?.actions.map((a) => (
        <button
          key={a.label}
          type="button"
          className="link"
          onClick={() => {
            a.run();
            setShown(false);
            setTimeout(shift, FADE_MS);
          }}
        >
          {a.label}
        </button>
      ))}
    </div>
  );
}
