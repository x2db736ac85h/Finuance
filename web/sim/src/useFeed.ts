// The live feed: GET /api/events read as a stream with fetch (EventSource can't send the token).
import { useEffect, useState } from 'react';

import { headers, type Schemas } from './api';

export type Event = Schemas['Event'];

/** How many actions the feed keeps on screen. */
export const FEED_SIZE = 100;

/** Parses server-sent events from text; returns the events and what is left of an unfinished one. */
export function parseEvents(text: string): { events: Event[]; rest: string } {
  const blocks = text.split('\n\n');
  const rest = blocks.pop() ?? '';
  const events: Event[] = [];
  for (const block of blocks) {
    const lines = block.split('\n');
    if (!lines.includes('event: action')) continue;
    const data = lines.find((l) => l.startsWith('data: '));
    if (!data) continue;
    try {
      events.push(JSON.parse(data.slice(6)) as Event);
    } catch {
      // a broken line is skipped
    }
  }
  return { events, rest };
}

export function useFeed(token: string): { events: Event[]; live: boolean } {
  const [events, setEvents] = useState<Event[]>([]);
  const [live, setLive] = useState(false);
  useEffect(() => {
    const abort = new AbortController();
    let retry: ReturnType<typeof setTimeout> | undefined;
    const connect = async () => {
      try {
        const res = await fetch(`${window.location.origin}/api/events`, {
          headers: headers(token),
          signal: abort.signal,
        });
        if (!res.ok || !res.body) throw new Error(String(res.status));
        setLive(true);
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = '';
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          const parsed = parseEvents(buf + value);
          buf = parsed.rest;
          if (parsed.events.length > 0) {
            setEvents((prev) => [...parsed.events.reverse(), ...prev].slice(0, FEED_SIZE));
          }
        }
      } catch {
        // the simulator went away or the token is wrong: try again shortly
      }
      setLive(false);
      if (!abort.signal.aborted) retry = setTimeout(() => void connect(), 3000);
    };
    void connect();
    return () => {
      abort.abort();
      clearTimeout(retry);
    };
  }, [token]);
  return { events, live };
}

/** Grafana's Explore with the request's log lines, which lead to its trace (Loki's trace_id link). */
export function logsLink(grafana: string, requestId: string): string {
  const pane = {
    datasource: 'loki',
    queries: [
      { refId: 'A', expr: `{traffic=~".+"} | request_id="${requestId}"`, datasource: { type: 'loki', uid: 'loki' } },
    ],
    range: { from: 'now-6h', to: 'now' },
  };
  return `${grafana.replace(/\/$/, '')}/explore?schemaVersion=1&panes=${encodeURIComponent(JSON.stringify({ a: pane }))}`;
}
