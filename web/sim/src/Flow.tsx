// The animals' actions as they happen (PLAN 25.16, ADR 0044): each action from the feed travels as
// a dot from the animals through the web server and the API to where its work goes (the
// database, the worker for an import, the mock model for a question), in its result's colour.
// With reduced motion there are no dots, only the counts of the last minute.
import { useEffect, useRef, useState } from 'react';

import type { Event } from './useFeed';

type Stop = 'db' | 'worker' | 'mock';

const NODES: Record<'animals' | 'web' | 'api' | Stop, { x: number; y: number; label: string }> = {
  animals: { x: 50, y: 60, label: 'Animals' },
  web: { x: 190, y: 60, label: 'Web server' },
  api: { x: 320, y: 60, label: 'API' },
  db: { x: 470, y: 22, label: 'Database' },
  worker: { x: 470, y: 60, label: 'Worker' },
  mock: { x: 470, y: 98, label: 'Mock model' },
};

/** Where an action's work goes. */
export function stopOf(ev: Event): Stop {
  if (ev.area === 'assistant' || ev.action === 'cash.receipt') return 'mock';
  if (ev.area === 'imports') return 'worker';
  return 'db';
}

/** At most this many dots move at once; the feed is sampled above 50 actions a second anyway. */
const MAX_DOTS = 40;

type Dot = { id: number; stop: Stop; result: Event['result'] };

function useReducedMotion(): boolean {
  const query = '(prefers-reduced-motion: reduce)';
  const [reduced, setReduced] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const m = window.matchMedia(query);
    const on = () => {
      setReduced(m.matches);
    };
    m.addEventListener('change', on);
    return () => {
      m.removeEventListener('change', on);
    };
  }, []);
  return reduced;
}

export function Flow({ events }: { events: Event[] }) {
  const reduced = useReducedMotion();
  const [dots, setDots] = useState<Dot[]>([]);
  const seen = useRef<Event | undefined>(undefined);
  const next = useRef(0);
  useEffect(() => {
    // the feed puts the newest first: the new ones are those before the last one seen
    const fresh: Event[] = [];
    for (const ev of events) {
      if (ev === seen.current) break;
      fresh.push(ev);
    }
    seen.current = events[0];
    if (reduced || fresh.length === 0) return;
    setDots((d) =>
      [...d, ...fresh.reverse().map((ev) => ({ id: next.current++, stop: stopOf(ev), result: ev.result }))].slice(
        -MAX_DOTS,
      ),
    );
  }, [events, reduced]);

  // the minute before the newest action in the feed (a render reads no clock)
  const minuteAgo = (events[0] ? Date.parse(events[0].time) : 0) - 60_000;
  const counts: Record<Stop, number> = { db: 0, worker: 0, mock: 0 };
  for (const ev of events) {
    if (Date.parse(ev.time) >= minuteAgo) counts[stopOf(ev)]++;
  }
  const label = `In the last minute: ${String(counts.db)} actions to the database, ${String(counts.worker)} imports to the worker, ${String(counts.mock)} questions to the mock model`;
  const a = NODES.animals;
  const p = NODES.api;
  return (
    <section aria-label="Flow" className="sim-flow">
      <h2>Flow</h2>
      <svg viewBox="0 0 540 120" role="img" aria-label={label}>
        <path className="sim-wire" d={`M${String(a.x)} ${String(a.y)}H${String(p.x)}`} />
        {(['db', 'worker', 'mock'] as const).map((s) => (
          <path
            key={s}
            className="sim-wire"
            d={`M${String(p.x)} ${String(p.y)}L${String(NODES[s].x)} ${String(NODES[s].y)}`}
          />
        ))}
        {Object.entries(NODES).map(([key, n]) => (
          <g key={key} className="sim-node">
            <circle cx={n.x} cy={n.y} r={9} />
            <text x={n.x} y={n.y + (key === 'db' ? -14 : 22)} textAnchor="middle">
              {n.label}
              {key in counts ? ` · ${String(counts[key as Stop])}/min` : ''}
            </text>
          </g>
        ))}
        {dots.map((d) => (
          <circle
            key={d.id}
            className={`sim-dot to-${d.stop} ${d.result}`}
            r={4}
            onAnimationEnd={() => {
              setDots((all) => all.filter((x) => x.id !== d.id));
            }}
          />
        ))}
      </svg>
    </section>
  );
}
