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
let state: Partial<Schemas['State']>;
let resources: Schemas['Resources'];
let reducedMotion: boolean;
let puts: Schemas['ConfigDoc'][];
let posts: string[];
let seenHeaders: Headers[];

const reset = () => {
  config = {
    generation: 4,
    config: {
      mode: 'realistic',
      personas: 10,
      paused: false,
      intensity: 1000,
      activeHours: true,
      clockRate: 1,
      mistakes: 20,
      assistantPerDay: 100,
      rate: 50,
      rampSeconds: 60,
      loadMinutes: 30,
      pushThrough: false,
      runSeed: 1234,
      jitter: 100,
      bursts: 0,
      history: { density: 1, spread: 100, variety: 1, years: 1 },
    },
  };
  state = {};
  const pts = (...vs: number[]) => vs.map((v, i) => ({ t: 1795400000 + 15 * i, v }));
  resources = {
    available: true,
    containers: [
      { name: 'db', cpu: pts(0.2, 0.4), memoryBytes: pts(500 * 2 ** 20, 512 * 2 ** 20) },
      { name: 'api', cpu: pts(0.1, 0.15), memoryBytes: pts(100 * 2 ** 20, 110 * 2 ** 20) },
    ],
    series: [
      { name: 'actions', points: pts(10, 12.5) },
      { name: 'requests', points: pts(30, 37) },
      { name: 'p95', points: pts(0.04, 0.062) },
      { name: 'serverErrors', points: pts(0, 0) },
    ],
  };
  reducedMotion = false;
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
        mode: config.config.mode,
        rate: 0,
        rebuild: { generation: 0, pending: 0 },
        historyEstimate: 735,
        grafana: 'http://localhost:3000',
        totals: { ok: 3, error: 1, skipped: 0 },
        personas: state.personas ?? [
          {
            slug: 'otter',
            name: 'Otter',
            animal: 'otter',
            says: 'the tidy one',
            on: true,
            signedIn: true,
            missing: false,
            counts: { ok: 2, error: 1, skipped: 0 },
          },
          {
            slug: 'fox',
            name: 'Fox',
            animal: 'fox',
            says: 'the one who asks',
            on: true,
            signedIn: true,
            missing: false,
            action: 'assistant.ask',
            counts: { ok: 1, error: 0, skipped: 0 },
          },
        ],
        ...state,
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
        ceilings: {
          personas: 50,
          intensity: 3000,
          clockRate: 1440,
          assistantPerDay: 500,
          actionsPerMinute: 30,
          loadPersonas: 500,
          rate: 1000,
          rampSeconds: 3600,
          loadMinutes: 1440,
          loadActionsPerSecond: 2,
          history: { density: 20, spread: 300, variety: 5, years: 10 },
        },
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
  http.get(`${API}/resources`, ({ request }) => authed(request, () => HttpResponse.json(resources))),
  http.post(`${API}/history/rebuild`, ({ request }) =>
    authed(request, () => {
      posts.push('rebuild');
      state = { ...state, rebuild: { generation: 1, pending: 2 } };
      return HttpResponse.json({ generation: 1, pending: 2 });
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
    matches: q.includes('reduced-motion') ? reducedMotion : false,
    media: q,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
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

  it('switches to load mode and sets the target', async () => {
    const user = await openWithToken();
    await user.click(await screen.findByRole('radio', { name: 'Load: one target for the lab' }));
    await waitFor(() => {
      expect(puts.at(-1)?.config.mode).toBe('load');
    });
    const rate = await screen.findByLabelText(/^Actions a second/);
    await user.clear(rate);
    await user.type(rate, '40');
    await waitFor(() => {
      expect(puts.at(-1)?.config.rate).toBe(40);
    });
    expect(screen.getByText(/needs at least 20 animals; 10 play/)).toBeTruthy();
    expect(screen.getByRole('spinbutton', { name: /^Personas/ }).getAttribute('max')).toBe('500');
    await user.click(screen.getByRole('checkbox', { name: /Push through server errors/ }));
    await waitFor(() => {
      expect(puts.at(-1)?.config.pushThrough).toBe(true);
    });
  });

  it('shows the load and when it ends in the header', async () => {
    config.config.mode = 'load';
    state = { mode: 'load', rate: 120.4, loadEndsAt: '2026-11-23T14:30:00Z' };
    await openWithToken();
    const now = await screen.findByLabelText('Load now');
    expect(now.textContent).toContain('120 a second');
    expect(now.textContent).toContain('until');
    expect(screen.queryByLabelText('Intensity')).toBeNull();
  });

  it('draws a new run seed and sets the jitter', async () => {
    const user = await openWithToken();
    const section = await screen.findByRole('region', { name: 'Randomness' });
    await user.click(within(section).getByRole('button', { name: 'New seed' }));
    await waitFor(() => {
      expect(puts.at(-1)?.config.runSeed).toBe(0);
    });
    expect(within(section).getByLabelText(/^Mistakes/)).toBeTruthy();
    expect(within(section).getByLabelText<HTMLInputElement>('Timing jitter').value).toBe('100');
  });

  it('shows more than twenty personas as one card per animal', async () => {
    const animals = ['otter', 'fox', 'owl'];
    state = {
      personas: Array.from({ length: 30 }, (_, i) => {
        const animal = animals[i % 3] ?? 'otter';
        return {
          slug: `${animal}-${String(i)}`,
          name: animal,
          animal,
          says: `the ${animal}`,
          on: true,
          signedIn: i < 27,
          missing: i >= 27,
          counts: { ok: 1, error: 0, skipped: 0 },
        };
      }),
    };
    await openWithToken();
    const otter = await screen.findByRole('article', { name: 'Otter' });
    expect(within(otter).getByText('×10')).toBeTruthy();
    expect(within(otter).getByText(/9 signed in, 1 not in the lab/)).toBeTruthy();
    expect(screen.getAllByRole('article')).toHaveLength(3);
  });

  it('says when an animal is not in the lab', async () => {
    state = {
      personas: [
        {
          slug: 'otter-2',
          name: 'Otter 2',
          animal: 'otter',
          says: 'the tidy one',
          on: true,
          signedIn: false,
          missing: true,
          counts: { ok: 0, error: 0, skipped: 0 },
        },
      ],
    };
    await openWithToken();
    const card = await screen.findByRole('article', { name: 'Otter 2' });
    expect(within(card).getByText(/not in the lab/)).toBeTruthy();
  });

  it('sets the history profile, shows the estimate and asks for a rebuild', async () => {
    const user = await openWithToken();
    const section = await screen.findByRole('region', { name: 'History' });
    expect(within(section).getByText(/About 735 transactions per animal/)).toBeTruthy();
    const density = within(section).getByRole('spinbutton', { name: /^Spending a day/ });
    expect(density.getAttribute('max')).toBe('20');
    await user.clear(density);
    await user.type(density, '9');
    await waitFor(() => {
      expect(puts.at(-1)?.config.history).toEqual({ density: 9, spread: 100, variety: 1, years: 1 });
    });
    await user.click(within(section).getByRole('button', { name: 'Rebuild history' }));
    await waitFor(() => {
      expect(posts).toContain('rebuild');
    });
    expect(await within(section).findByRole('status')).toHaveProperty('textContent', '2 animals still to rebuild');
  });

  it('shows what the load costs: the figures and every container', async () => {
    await openWithToken();
    const section = await screen.findByRole('region', { name: 'What the load costs' });
    expect(await within(section).findByText('12.5')).toBeTruthy();
    expect(within(section).getByRole('group', { name: 'API p95' }).textContent).toContain('62 ms');
    const rows = within(section).getAllByRole('row');
    expect(rows[1]?.textContent).toContain('db');
    expect(rows[1]?.textContent).toContain('512 MiB');
    expect(rows[2]?.textContent).toContain('0.15');
  });

  it('says why there are no figures', async () => {
    resources = {
      available: false,
      error: 'the simulator was started without -prometheus',
      containers: [],
      series: [],
    };
    await openWithToken();
    const section = await screen.findByRole('region', { name: 'What the load costs' });
    expect(await within(section).findByText(/started without -prometheus/)).toBeTruthy();
  });

  it('draws the flow, and only counts with reduced motion', async () => {
    reducedMotion = true;
    await openWithToken();
    const flow = await screen.findByRole('region', { name: 'Flow' });
    await waitFor(() => {
      expect(within(flow).getByRole('img').getAttribute('aria-label')).toContain('1 questions to the mock model');
    });
    expect(flow.querySelectorAll('.sim-dot')).toHaveLength(0);
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
    expect(url.searchParams.get('panes')).toContain('|= \\"sim-fox-1\\"');
  });
});
