import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from './api';
import { App } from './App';
import { logsLink, parseEvents } from './useFeed';

const TOKEN = 'a-control-token-of-thirty-two-bytes!';

const event: Schemas['Event'] = {
  time: '2026-11-21T12:00:00Z',
  persona: 'fox',
  action: 'assistant.ask',
  area: 'assistant',
  result: 'ok',
  durationMs: 412,
  requestId: 'sim-fox-0123456789abcdef',
};

let config: Schemas['ConfigDoc'];
let puts: Schemas['ConfigDoc'][];
let posts: string[];
let seenHeaders: Headers[];

const reset = () => {
  config = {
    generation: 4,
    config: {
      personas: 10,
      paused: false,
      intensity: 1000,
      activeHours: true,
      clockRate: 1,
      mistakes: 20,
      assistantPerDay: 100,
    },
  };
  puts = [];
  posts = [];
  seenHeaders = [];
};

const authed = (request: Request, answer: () => Response) => {
  seenHeaders.push(request.headers);
  if (request.headers.get('Authorization') !== `Bearer ${TOKEN}`) {
    return HttpResponse.json(
      { status: 401, code: 'unauthorized', title: 'The control token is missing or wrong.' },
      { status: 401 },
    );
  }
  return answer();
};

const API = `${window.location.origin}/api`;

const server = setupServer(
  http.get(`${API}/state`, ({ request }) =>
    authed(request, () =>
      HttpResponse.json({
        target: 'https://lab.example',
        version: 'test',
        paused: config.config.paused,
        generation: config.generation,
        grafana: 'http://localhost:3000',
        totals: { ok: 3, error: 1, skipped: 0 },
        personas: [
          {
            slug: 'otter',
            name: 'Otter',
            animal: 'otter',
            says: 'the tidy one',
            on: true,
            signedIn: true,
            counts: { ok: 2, error: 1, skipped: 0 },
          },
          {
            slug: 'fox',
            name: 'Fox',
            animal: 'fox',
            says: 'the one who asks',
            on: true,
            signedIn: true,
            action: 'assistant.ask',
            counts: { ok: 1, error: 0, skipped: 0 },
          },
        ],
      } satisfies Schemas['State']),
    ),
  ),
  http.get(`${API}/catalogue`, ({ request }) =>
    authed(request, () =>
      HttpResponse.json({
        areas: ['table', 'assistant'],
        actions: [
          { name: 'table.window', area: 'table', cost: 'light', mistake: false },
          { name: 'assistant.ask', area: 'assistant', cost: 'model', mistake: false },
        ],
        characters: [{ animal: 'fox', says: 'the one who asks', weights: { 'assistant.ask': 25 } }],
        presets: ['error-storm', 'quiet-day'],
        ceilings: { personas: 50, intensity: 3000, clockRate: 1440, assistantPerDay: 500, actionsPerMinute: 30 },
      } satisfies Schemas['Catalogue']),
    ),
  ),
  http.get(`${API}/config`, ({ request }) => authed(request, () => HttpResponse.json(config))),
  http.put(`${API}/config`, async ({ request }) => {
    const body = (await request.json()) as Schemas['ConfigDoc'];
    return authed(request, () => {
      if (body.generation !== config.generation) return HttpResponse.json(config, { status: 409 });
      puts.push(body);
      config = { generation: config.generation + 1, config: body.config };
      return HttpResponse.json(config);
    });
  }),
  http.post(`${API}/pause`, ({ request }) =>
    authed(request, () => {
      posts.push('pause');
      config = { generation: config.generation + 1, config: { ...config.config, paused: true } };
      return HttpResponse.json(config);
    }),
  ),
  http.post(`${API}/presets/:name`, ({ request, params }) =>
    authed(request, () => {
      posts.push(`preset ${String(params['name'])}`);
      return HttpResponse.json(config);
    }),
  ),
  http.post(`${API}/act`, async ({ request }) => {
    const body = (await request.json()) as Schemas['ActRequest'];
    return authed(request, () => {
      posts.push(`act ${body.persona} ${body.action}`);
      return HttpResponse.json({ ...event, persona: body.persona, action: body.action });
    });
  }),
  http.get(`${API}/events`, ({ request }) =>
    authed(
      request,
      () =>
        new HttpResponse(`event: action\ndata: ${JSON.stringify(event)}\n\n`, {
          headers: { 'Content-Type': 'text/event-stream' },
        }),
    ),
  ),
);

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
beforeEach(() => {
  reset();
  window.sessionStorage.clear();
  window.matchMedia = ((q: string) => ({
    matches: false,
    media: q,
    addEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

async function openWithToken(token = TOKEN) {
  const user = userEvent.setup();
  render(<App />);
  await user.type(screen.getByLabelText('Control token'), token);
  await user.click(screen.getByRole('button', { name: 'Open' }));
  return user;
}

describe('the tuning page', () => {
  it('asks for the control token and sends it with the client header on every request', async () => {
    await openWithToken();
    expect(await screen.findByRole('heading', { name: 'Simulator' })).toBeTruthy();
    expect(await screen.findByText('https://lab.example')).toBeTruthy();
    expect(seenHeaders.length).toBeGreaterThan(0);
    for (const h of seenHeaders) {
      expect(h.get('X-Finuance-Client')).toBe('web');
      expect(h.get('Authorization')).toBe(`Bearer ${TOKEN}`);
    }
  });

  it('goes back to the form when the simulator refuses the token', async () => {
    await openWithToken('a-wrong-token-that-is-long-enough!!');
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByLabelText('Control token')).toBeTruthy();
  });

  it('changes a weight with the generation it read', async () => {
    const user = await openWithToken();
    const input = await screen.findByLabelText('Weight of assistant.ask');
    expect((input as HTMLInputElement).placeholder).toBe('25'); // the characters' own
    await user.type(input, '7');
    await waitFor(() => {
      expect(puts.at(-1)?.config.weights).toEqual({ 'assistant.ask': 7 });
    });
    expect(puts[0]?.generation).toBe(4);
  });

  it('switches an area and a persona off', async () => {
    const user = await openWithToken();
    const area = await screen.findByRole('group', { name: 'table' });
    await user.click(within(area).getByRole('checkbox', { name: 'table' }));
    await waitFor(() => {
      expect(puts.at(-1)?.config.areasOff).toEqual(['table']);
    });
    const otter = screen.getByRole('article', { name: 'Otter' });
    await user.click(within(otter).getByRole('checkbox', { name: 'On' }));
    await waitFor(() => {
      expect(puts.at(-1)?.config.off).toEqual(['otter']);
    });
  });

  it('pauses and applies a preset', async () => {
    const user = await openWithToken();
    await user.click(await screen.findByRole('button', { name: 'Pause' }));
    await user.click(screen.getByRole('button', { name: 'error storm' }));
    await waitFor(() => {
      expect(posts).toEqual(['pause', 'preset error-storm']);
    });
  });

  it('acts now and shows the request id with a link to its logs', async () => {
    const user = await openWithToken();
    await user.click(await screen.findByRole('button', { name: 'Act now' }));
    const result = await screen.findByLabelText('Result');
    expect(posts).toEqual(['act fox assistant.ask']);
    expect(within(result).getByText('sim-fox-0123456789abcdef')).toBeTruthy();
    expect(within(result).getByRole('link', { name: 'Logs and trace' }).getAttribute('href')).toContain(
      'localhost:3000/explore',
    );
  });

  it('shows the feed', async () => {
    await openWithToken();
    const feed = await screen.findByRole('region', { name: 'Feed' });
    await waitFor(() => {
      expect(within(feed).getByText('sim-fox-0123456789abcdef')).toBeTruthy();
    });
  });
});

describe('the feed parser and the links', () => {
  it('reads complete events and keeps the rest', () => {
    const text = `event: action\ndata: ${JSON.stringify(event)}\n\n: keep-alive\n\nevent: action\ndata: {"pers`;
    const { events, rest } = parseEvents(text);
    expect(events).toEqual([event]);
    expect(rest).toBe('event: action\ndata: {"pers');
  });

  it('builds a Loki query for the request id', () => {
    const url = new URL(logsLink('http://localhost:3000/', 'sim-fox-1'));
    expect(url.pathname).toBe('/explore');
    expect(url.searchParams.get('panes')).toContain('request_id=\\"sim-fox-1\\"');
  });
});
