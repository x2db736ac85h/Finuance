import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import type { MessageRequest } from '../api/hooks/assistant';
import { parsePrefs } from '../stores/prefs';
import { uiDefaults, useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { chipsOf, cleared, withoutChip } from '../table/chips';
import { contextOf } from '../table/context';
import { renderApp } from '../test/render';
import { account, API, db, problem, resetDb, server } from '../test/server';
import { applyEvent, useChat, type AiTurn } from './chatStore';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  // The shell loads the panel lazily; transform its chunk here, so the first test's findBy waits
  // for the render and not for a cold import under a busy parallel run.
  await import('./ChatPanel');
});
afterAll(() => {
  server.close();
});
afterEach(() => {
  server.resetHandlers();
});

const CHAT = '00000000-0000-4000-8000-00000000c001';

const status = (over: Partial<Schemas['AssistantConnection']>[] = [{}]): Schemas['AssistantStatus'] => ({
  enabled: over.length > 0,
  maxRowsLimit: 50,
  connections: over.map((o, i) => ({
    name: i === 0 ? 'lmstudio' : `c${String(i)}`,
    label: i === 0 ? 'LM Studio' : `Conn ${String(i)}`,
    api: 'openai',
    cloud: false,
    reachable: true,
    defaultModel: 'qwen3-14b',
    models: [
      { id: 'qwen3-14b', tools: true, reasoning: 'switch', contextTokens: 8192 },
      { id: 'gemma-3-12b', tools: false, reasoning: 'none', contextTokens: 8192 },
    ],
    ...o,
  })),
});

const meta: Schemas['AssistantMeta'] = {
  durationMs: 1234,
  approxTokensSent: 1830,
  lookups: 1,
  rounds: 2,
  connection: 'lmstudio',
  model: 'qwen3-14b',
  mode: 'tools',
  scope: 'all',
};

const step = {
  index: 0,
  tool: 'search_transactions',
  args: { query: 'kino' },
  summary: 'query: kino → 10 matches',
  result: '{"currency":"PLN","matched":10}',
  matched: 10,
  toolFilter: { query: 'kino', label: 'query: kino' },
};

const sse = (events: [string, unknown][]) =>
  events.map(([e, d]) => `event: ${e}\ndata: ${JSON.stringify(d)}\n\n`).join('');

type State = {
  status: Schemas['AssistantStatus'];
  settings: Schemas['AssistantSettings'];
  chats: Schemas['AssistantChat'][];
  sent: MessageRequest[];
  settingsPuts: Schemas['AssistantSettings'][];
  stops: number;
  deleted: string[];
  /** The stored chat's lookups and the proposals applied in it. */
  storedSteps: Schemas['AssistantStep'][];
  applied: string[];
  answer: () => Response;
};

let st: State;

function handlers() {
  server.use(
    http.get(`${API}/assistant/status`, () => HttpResponse.json(st.status)),
    http.get(`${API}/assistant/settings`, () => HttpResponse.json(st.settings)),
    http.put(`${API}/assistant/settings`, async ({ request }) => {
      st.settings = (await request.json()) as Schemas['AssistantSettings'];
      st.settingsPuts.push(st.settings);
      return HttpResponse.json(st.settings);
    }),
    http.get(`${API}/assistant/chats`, () => HttpResponse.json({ chats: st.chats, maxChats: 25 })),
    http.post(`${API}/assistant/chats`, () => {
      const c = {
        id: CHAT,
        title: '',
        connection: '',
        model: '',
        createdAt: '2025-03-20T10:00:00Z',
        updatedAt: '2025-03-20T10:00:00Z',
      };
      return HttpResponse.json(c, { status: 201 });
    }),
    http.get(`${API}/assistant/chats/:id`, ({ params }) =>
      HttpResponse.json({
        ...st.chats.find((c) => c.id === params['id']),
        noTools: false,
        appliedProposals: st.applied,
        turns: [
          { role: 'user', seq: 1, content: 'Earlier question', createdAt: '2025-03-19T10:00:00Z' },
          {
            role: 'assistant',
            seq: 4,
            content: 'Earlier **answer**',
            steps: st.storedSteps,
            meta,
            createdAt: '2025-03-19T10:00:05Z',
          },
        ],
      }),
    ),
    http.delete(`${API}/assistant/chats/:id`, ({ params }) => {
      st.deleted.push(String(params['id']));
      return new HttpResponse(null, { status: 204 });
    }),
    http.post(`${API}/assistant/chats/:id/messages`, async ({ request }) => {
      st.sent.push((await request.json()) as MessageRequest);
      return st.answer();
    }),
    http.post(`${API}/assistant/chats/:id/stop`, () => {
      st.stops++;
      return new HttpResponse(null, { status: 204 });
    }),
  );
}

const streamed = (events: [string, unknown][]) => () =>
  new HttpResponse(sse(events), { headers: { 'Content-Type': 'text/event-stream' } });

beforeEach(() => {
  resetDb();
  db.accounts = [account({ txCount: 917, firstOn: '2024-01-01', lastOn: '2025-03-17' })];
  db.prefs = { version: 1, prefs: { ai: { open: true } } };
  useUi.setState({ ...uiDefaults, ai: { ...uiDefaults.ai, open: true } });
  useViews.getState().reset();
  useChat.setState({ chatId: null, turns: [], busy: false, noTools: false, applied: [], ctrl: null });
  st = {
    status: status(),
    settings: { retentionDays: 2, cloudConsent: [] },
    chats: [],
    sent: [],
    settingsPuts: [],
    stops: 0,
    deleted: [],
    storedSteps: [step],
    applied: [],
    answer: streamed([
      ['reasoning', { delta: 'Looking up kino.' }],
      ['step', step],
      ['content', { delta: 'Found **10** visits.\n\n| Date | Amount |\n|---|---:|\n' }],
      ['content', { delta: '| 2025-03-02 | -45,00 |' }],
      ['done', { messageSeq: 4, meta }],
    ]),
  };
  handlers();
});

const panel = async () => screen.findByRole('complementary', { name: 'Assistant' });

describe('ChatPanel', () => {
  it('streams an answer with its reasoning, lookup and meta, and shows the lookup in the table', async () => {
    const { router } = renderApp('/t/all');
    const p = await panel();
    expect(await within(p).findByText('Ask about your money')).toBeTruthy();
    expect(within(p).getByText(/917 transactions/)).toBeTruthy();
    expect(await within(p).findByText('qwen3-14b')).toBeTruthy();

    fireEvent.click(within(p).getByText('Which subscriptions or regular bills do I pay?'));
    expect(
      await within(p).findByText((_, el) => el?.tagName === 'P' && el.textContent === 'Found 10 visits.'),
    ).toBeTruthy();
    const sent = st.sent[0];
    expect(sent?.text).toBe('Which subscriptions or regular bills do I pay?');
    expect(sent?.scope).toBe('all');
    expect(sent?.settings).toMatchObject({
      connection: 'lmstudio',
      model: 'qwen3-14b',
      mode: 'auto',
      think: 'auto',
      maxRows: 50,
    });
    expect(sent?.timeZone).toBeTruthy();

    expect(within(p).getByText('Thinking')).toBeTruthy();
    expect(within(p).getByText('Searched transactions')).toBeTruthy();
    expect(within(p).getByText('10', { selector: 'b' })).toBeTruthy();
    expect(within(p).getByText('-45,00').className).toBe('r');
    expect(within(p).getByText('1.2 s · ~1,830 tokens sent · 1 lookup · qwen3-14b')).toBeTruthy();

    fireEvent.click(within(p).getByText('Show in table'));
    expect(viewOf(useViews.getState().views, 'all').toolFilter).toEqual(step.toolFilter);
    expect(router.state.location.pathname).toBe('/t/all');
    expect(await screen.findByText('Showing 10 matching rows')).toBeTruthy();
  });

  it('shows a proposal as a card; Apply creates the category and sends the change marked with the chat and proposal', async () => {
    const ids = Array.from({ length: 14 }, (_, i) => `00000000-0000-4000-8000-${String(i + 1).padStart(12, '0')}`);
    const proposal: Schemas['ActionProposal'] = {
      key: 'proposal-internet',
      action: 'set_category',
      reason: 'Monthly internet bills',
      matched: 14,
      ids,
      categoryName: 'Internet',
      sample: [
        {
          id: ids[0] ?? '',
          bookedOn: '2025-02-20',
          payee: 'OPERATOR INTERNETU',
          amount: { amountMinor: -7999, currency: 'PLN' },
          category: 'Rachunki',
        },
      ],
      toolFilter: { payee: 'OPERATOR INTERNETU', label: 'payee: OPERATOR INTERNETU' },
    };
    st.answer = streamed([
      [
        'step',
        {
          index: 0,
          tool: 'propose_action',
          args: {},
          summary: 'proposed set category → 14 transactions',
          result: '{}',
          matched: 14,
          proposal,
        },
      ],
      ['content', { delta: 'Review the change and click Apply.' }],
      ['done', { messageSeq: 4, meta }],
    ]);
    const calls: { path: string; chat: string | null; proposal?: string | null; body: unknown }[] = [];
    server.use(
      http.get(`${API}/categories`, () => HttpResponse.json([])),
      http.post(`${API}/categories`, async ({ request }) => {
        calls.push({ path: '/categories', chat: request.headers.get('X-Assistant-Chat'), body: await request.json() });
        return HttpResponse.json({ id: '00000000-0000-4000-8000-0000000000c9', name: 'Internet' }, { status: 201 });
      }),
      http.post(`${API}/transactions/bulk`, async ({ request }) => {
        calls.push({
          path: '/transactions/bulk',
          chat: request.headers.get('X-Assistant-Chat'),
          proposal: request.headers.get('X-Assistant-Proposal'),
          body: await request.json(),
        });
        return HttpResponse.json({ updated: 14 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/t/all');
    const p = await panel();
    await user.type(
      await within(p).findByRole('textbox', { name: 'Question' }),
      'Put the internet bills in Internet{Enter}',
    );
    const card = await within(p).findByRole('group', { name: 'Proposed change' });
    expect(within(card).getByText('Set the category “Internet” for 14 transactions')).toBeTruthy();
    expect(card.textContent).toContain('Monthly internet bills');
    expect(card.textContent).toContain('The category is new and is created first.');
    expect(card.textContent).toContain('OPERATOR INTERNETU');
    expect(card.textContent).toContain('and 13 more');
    expect(card.textContent).toContain('Nothing changes until you apply it.');
    expect(calls).toEqual([]);

    await user.click(within(card).getByRole('button', { name: 'Apply' }));
    expect(await screen.findByText('Applied to 14 transactions')).toBeTruthy();
    expect(calls).toEqual([
      { path: '/categories', chat: null, body: { name: 'Internet' } },
      {
        path: '/transactions/bulk',
        chat: CHAT,
        proposal: 'proposal-internet',
        body: { ids, category: { id: '00000000-0000-4000-8000-0000000000c9' } },
      },
    ]);
    expect(within(card).getByText('✓ Applied')).toBeTruthy();
    expect(within(card).queryByRole('button', { name: 'Apply' })).toBeNull();
    expect(useChat.getState().applied).toEqual(['proposal-internet']);

    // Show in table puts the proposal's rows in the view
    await user.click(within(card).getByRole('button', { name: 'Show in table' }));
    expect(viewOf(useViews.getState().views, 'all').toolFilter).toEqual(proposal.toolFilter);
  });

  it('a rule proposal with an existing category creates the rule', async () => {
    const proposal: Schemas['ActionProposal'] = {
      key: 'proposal-gym-rule',
      action: 'create_rule',
      matched: 19,
      categoryName: 'Sport',
      categoryId: '00000000-0000-4000-8000-0000000000c5',
      ruleName: 'KLUB FITNESS RUCH',
      match: { filters: [{ column: 'payee', cond: 'KLUB FITNESS RUCH' }] },
      sample: [],
    };
    st.answer = streamed([
      [
        'step',
        {
          index: 0,
          tool: 'propose_action',
          args: {},
          summary: 'proposed create rule',
          result: '{}',
          matched: 19,
          proposal,
        },
      ],
      ['done', { messageSeq: 4, meta }],
    ]);
    const rules: { chat: string | null; body: unknown }[] = [];
    server.use(
      http.get(`${API}/categories`, () => HttpResponse.json([])),
      http.post(`${API}/rules`, async ({ request }) => {
        rules.push({ chat: request.headers.get('X-Assistant-Chat'), body: await request.json() });
        return HttpResponse.json({}, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    renderApp('/t/all');
    const p = await panel();
    await user.type(await within(p).findByRole('textbox', { name: 'Question' }), 'Make a rule for the gym{Enter}');
    const card = await within(p).findByRole('group', { name: 'Proposed change' });
    expect(
      within(card).getByText('Create the rule “KLUB FITNESS RUCH”: payee contains “KLUB FITNESS RUCH” → Sport'),
    ).toBeTruthy();
    expect(card.textContent).toContain('It matches 19 transactions now and future imports');
    expect(card.textContent).not.toContain('created first');
    await user.click(within(card).getByRole('button', { name: 'Apply' }));
    expect(await screen.findByText('Rule “KLUB FITNESS RUCH” created · applying…')).toBeTruthy();
    expect(rules).toEqual([
      {
        chat: CHAT,
        body: {
          name: 'KLUB FITNESS RUCH',
          enabled: true,
          match: proposal.match,
          setCategoryId: proposal.categoryId,
          markTransfer: false,
        },
      },
    ]);
  });

  it('asks about the table view with its context', async () => {
    db.prefs = { version: 1, prefs: { ai: { open: true, scope: 'view' } } };
    renderApp('/t/all');
    const p = await panel();
    useViews.getState().patch('all', { filters: { payee: { cond: 'kino', excluded: [] } } });
    const box = await within(p).findByRole('textbox', { name: 'Question' });
    await userEvent.type(box, 'How much?{Enter}');
    await waitFor(() => {
      expect(st.sent).toHaveLength(1);
    });
    expect(st.sent[0]?.scope).toBe('view');
    expect(st.sent[0]?.context).toEqual({ filters: [{ column: 'payee', cond: 'kino' }], includeTransfers: false });
  });

  it('explains how to fix an error', async () => {
    st.answer = streamed([['error', { code: 'tools_rejected', message: 'The model does not support tools.' }]]);
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByText('What were my 5 biggest expenses this year?'));
    const alert = await within(p).findByRole('alert');
    expect(alert.textContent).toContain('The model does not support tools.');
    expect(alert.textContent).toContain('Choose Plan mode in the assistant settings.');
  });

  it('shows a problem that keeps the question from starting', async () => {
    st.answer = () => problem(409, 'run_in_progress', 'A reply is still being written in another chat or tab');
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByText('What were my 5 biggest expenses this year?'));
    expect((await within(p).findByRole('alert')).textContent).toContain('A reply is still being written');
  });

  it('stops an answer being written', async () => {
    let close: (() => void) | undefined;
    st.answer = () => {
      const enc = new TextEncoder();
      const body = new ReadableStream<Uint8Array>({
        start(c) {
          c.enqueue(enc.encode(sse([['content', { delta: 'Still thinking' }]])));
          close = () => {
            c.enqueue(
              enc.encode(
                sse([
                  ['content', { delta: '\n\n_Stopped._' }],
                  ['done', { messageSeq: 3, meta: { ...meta, stopped: true } }],
                ]),
              ),
            );
            c.close();
          };
        },
      });
      return new HttpResponse(body, { headers: { 'Content-Type': 'text/event-stream' } });
    };
    server.use(
      http.post(`${API}/assistant/chats/:id/stop`, () => {
        st.stops++;
        close?.();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByText('What were my 5 biggest expenses this year?'));
    expect(await within(p).findByText(/Still thinking/)).toBeTruthy();
    fireEvent.click(within(p).getByRole('button', { name: 'Stop' }));
    expect(await within(p).findByText('Stopped.')).toBeTruthy();
    expect(st.stops).toBe(1);
    expect(within(p).getByText(/stopped · qwen3-14b/)).toBeTruthy();
    expect(useChat.getState().busy).toBe(false);
  });

  it('reopens a chat with its applied proposal shown as applied (from the activity log)', async () => {
    const proposal: Schemas['ActionProposal'] = {
      key: 'aaaaaaaaaaaaaaaa',
      action: 'set_group',
      matched: 3,
      groupName: 'Trip',
      groupId: '00000000-0000-4000-8000-0000000000a1',
      sample: [],
    };
    const other: Schemas['ActionProposal'] = { ...proposal, key: 'bbbbbbbbbbbbbbbb', groupName: 'Home' };
    const prop = (p: Schemas['ActionProposal']): Schemas['AssistantStep'] => ({
      tool: 'propose_action',
      args: {},
      summary: 'proposed set group',
      result: '{}',
      matched: 3,
      proposal: p,
    });
    st.storedSteps = [prop(proposal), prop(other)];
    st.applied = ['aaaaaaaaaaaaaaaa'];
    st.chats = [
      {
        id: CHAT,
        title: 'Earlier question',
        connection: 'lmstudio',
        model: 'qwen3-14b',
        createdAt: '2025-03-19T10:00:00Z',
        updatedAt: '2025-03-19T10:00:05Z',
      },
    ];
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByTitle('Earlier question'));
    const cards = await within(p).findAllByRole('group', { name: 'Proposed change' });
    expect(cards).toHaveLength(2);
    expect(within(cards[0] as HTMLElement).getByText('✓ Applied')).toBeTruthy();
    expect(within(cards[1] as HTMLElement).getByRole('button', { name: 'Apply' })).toBeTruthy();
  });

  it('opens and deletes a recent chat', async () => {
    st.chats = [
      {
        id: CHAT,
        title: 'Earlier question',
        connection: 'lmstudio',
        model: 'qwen3-14b',
        createdAt: '2025-03-19T10:00:00Z',
        updatedAt: '2025-03-19T10:00:05Z',
      },
    ];
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByTitle('Earlier question'));
    expect(await within(p).findByText('answer', { selector: 'b' })).toBeTruthy();
    expect(useChat.getState().chatId).toBe(CHAT);
    fireEvent.click(within(p).getByTitle('Delete this chat'));
    await waitFor(() => {
      expect(st.deleted).toEqual([CHAT]);
    });
    await waitFor(() => {
      expect(useChat.getState().chatId).toBe(null);
    });
  });

  it('says when the assistant is not set up', async () => {
    st.status = status([]);
    renderApp('/t/all');
    const p = await panel();
    expect(await within(p).findByText('The assistant isn’t set up')).toBeTruthy();
    expect(within(p).queryByRole('textbox', { name: 'Question' })).toBeNull();
  });
});

describe('ChatSettings', () => {
  it('saves the retention and warns about keeping chats forever', async () => {
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByTitle('Assistant settings'));
    const keep = await within(p).findByLabelText('Keep chats');
    await waitFor(() => {
      expect((keep as HTMLSelectElement).disabled).toBe(false);
    });
    expect(within(p).getByText('Chats are deleted 2 days after their last message.')).toBeTruthy();
    fireEvent.change(keep, { target: { value: 'forever' } });
    await waitFor(() => {
      expect(st.settingsPuts).toEqual([{ retentionDays: null, cloudConsent: [] }]);
    });
    expect(await within(p).findByText(/Chats are kept until you delete them/)).toBeTruthy();
  });

  it('asks for consent before a cloud connection gets data', async () => {
    st.status = status([{}, { cloud: true, label: 'Claude (cloud)', name: 'claude' }]);
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByTitle('Assistant settings'));
    const conn = await within(p).findByLabelText('Connection');
    await waitFor(() => {
      expect(within(conn).getAllByRole('option')).toHaveLength(2);
    });
    fireEvent.change(conn, { target: { value: 'claude' } });
    const dialog = await screen.findByRole('dialog', { name: 'Send data to a cloud service?' });
    fireEvent.click(within(dialog).getByText('Allow Claude (cloud)'));
    await waitFor(() => {
      expect(st.settingsPuts).toEqual([{ retentionDays: 2, cloudConsent: ['claude'] }]);
    });
    expect(useUi.getState().ai.connection).toBe('claude');
    expect(await within(p).findByText('cloud')).toBeTruthy();
    expect(within(p).getByText(/You agreed to send your questions/)).toBeTruthy();
  });

  it('switches the model and the thinking level', async () => {
    renderApp('/t/all');
    const p = await panel();
    fireEvent.click(await within(p).findByTitle('Assistant settings'));
    const model = await within(p).findByLabelText('Model');
    fireEvent.change(model, { target: { value: 'gemma-3-12b' } });
    fireEvent.change(within(p).getByLabelText('Thinking'), { target: { value: 'off' } });
    expect(useUi.getState().ai).toMatchObject({ connection: 'lmstudio', model: 'gemma-3-12b', thinking: 'off' });
    expect(within(p).getByText(/This model has no thinking control/)).toBeTruthy();
    act(() => {
      fireEvent.click(within(p).getByText('Done'));
    });
    expect(await within(p).findByText('Ask about your money')).toBeTruthy();
  });
});

describe('chat state', () => {
  const turn: AiTurn = {
    role: 'assistant',
    content: 'a',
    reasoning: 'r',
    steps: [],
    pending: true,
    scope: { view: false, viewId: 'all' },
  };

  it('applies stream events, and plan mode starts the answer again', () => {
    let t = applyEvent(turn, { type: 'content', delta: 'b' });
    t = applyEvent(t, { type: 'step', step: { ...step, index: 0 } });
    expect(t).toMatchObject({ content: 'ab', steps: [{ tool: 'search_transactions' }] });
    t = applyEvent(t, { type: 'mode', mode: 'plan', reason: 'no tools' });
    expect(t).toMatchObject({ content: '', reasoning: '', steps: [] });
    t = applyEvent(t, { type: 'done', messageSeq: 3, meta });
    expect(t.pending).toBe(false);
    expect(applyEvent(turn, { type: 'error', code: 'timeout', message: 'slow' }).error).toEqual({
      code: 'timeout',
      message: 'slow',
    });
  });

  it('migrates the legacy thinking checkbox', () => {
    expect(parsePrefs({ ai: { think: true } }).ai?.thinking).toBe('auto');
    expect(parsePrefs({ ai: { think: false } }).ai?.thinking).toBe('off');
    expect(parsePrefs({ ai: { thinking: 'high', connection: 'lm' } }).ai).toMatchObject({
      thinking: 'high',
      connection: 'lm',
    });
    expect(parsePrefs({ ai: { thinking: 'max' } }).ai?.thinking).toBeUndefined();
  });

  it('puts a lookup in the view as a removable chip', () => {
    const v = { ...viewOf({}, 'all'), toolFilter: { query: 'kino', label: 'query: kino' } };
    expect(chipsOf('all', v, undefined)).toContainEqual({ key: 'tool', label: '✦', value: 'query: kino' });
    expect(contextOf('all', v).toolFilter).toEqual(v.toolFilter);
    expect(withoutChip('all', v, 'tool')).toEqual({ toolFilter: null });
    expect(cleared('all').toolFilter).toBe(null);
  });
});
