// The open chat (legacy S.chat): its turns as the panel shows them, the run in progress and how it
// is stopped. Chats live on the server; this store holds the one on screen.

import { create } from 'zustand';

import { ApiError } from '../api/client';
import {
  createChat,
  fetchChat,
  sendMessage,
  stopRun,
  type ChatDetail,
  type MessageRequest,
  type Meta,
  type RunEvent,
  type Step,
} from '../api/hooks/assistant';

export type TurnError = { code: string; message: string };

/** Where a turn's lookups ran: every account, or one table view ("Show in table" goes there). */
export type TurnScope = { view: boolean; viewId: string };

export type UserTurn = { role: 'user'; content: string };
export type AiTurn = {
  role: 'assistant';
  content: string;
  reasoning: string;
  steps: Step[];
  meta?: Meta;
  error?: TurnError;
  pending: boolean;
  scope: TurnScope;
};
export type UiTurn = UserTurn | AiTurn;

type ChatStore = {
  chatId: string | null;
  turns: UiTurn[];
  busy: boolean;
  /** Auto mode switched this chat to plan mode. */
  noTools: boolean;
  /** The keys of the chat's proposals the user applied (from the activity log, PLAN 17.6). */
  applied: string[];
  ctrl: AbortController | null;
  newChat: () => Promise<void>;
  open: (id: string) => Promise<void>;
  /** Forget the chat on screen when it was deleted. */
  dropped: (id: string) => void;
  send: (text: string, req: Omit<MessageRequest, 'text'>, scope: TurnScope, hooks?: SendHooks) => Promise<void>;
  /** A proposal of this chat was applied. */
  markApplied: (key: string) => void;
  stop: () => Promise<void>;
};

export type SendHooks = {
  /** A cloud connection needs the user's agreement (403 consent_required). */
  onConsent?: () => void;
  /** The chat list changed (a new chat, a new title or order). */
  onChanged?: () => void;
};

/** How long a stop request may take before the connection is simply closed. */
const STOP_GRACE_MS = 3000;

/** The turns of a stored chat. */
export function turnsOf(c: ChatDetail, viewId = 'all'): UiTurn[] {
  return c.turns.map((t): UiTurn => {
    if (t.role === 'user') return { role: 'user', content: t.content };
    const scope: TurnScope =
      t.meta?.scope === 'view'
        ? { view: true, viewId: t.meta.accountIds?.[0] ?? viewId }
        : { view: false, viewId: 'all' };
    return {
      role: 'assistant',
      content: t.content,
      reasoning: t.reasoning ?? '',
      steps: t.steps ?? [],
      ...(t.meta ? { meta: t.meta } : {}),
      ...(t.error ? { error: t.error } : {}),
      pending: false,
      scope,
    };
  });
}

/** Applies a stream event to the answer being written. */
export function applyEvent(t: AiTurn, e: RunEvent): AiTurn {
  switch (e.type) {
    case 'content':
      return { ...t, content: t.content + e.delta };
    case 'reasoning':
      return { ...t, reasoning: t.reasoning + e.delta };
    case 'step': {
      const steps = [...t.steps];
      steps[e.step.index] = e.step;
      return { ...t, steps };
    }
    case 'mode':
      // the server starts again in plan mode: what was written so far is dropped
      return { ...t, content: '', reasoning: '', steps: [] };
    case 'done':
      return { ...t, meta: e.meta, pending: false };
    case 'error':
      return { ...t, error: { code: e.code, message: e.message }, pending: false };
  }
}

function lastAi(turns: UiTurn[], f: (t: AiTurn) => AiTurn): UiTurn[] {
  const i = turns.length - 1;
  const t = turns[i];
  if (!t || t.role !== 'assistant') return turns;
  const out = turns.slice();
  out[i] = f(t);
  return out;
}

export const useChat = create<ChatStore>()((set, get) => ({
  chatId: null,
  turns: [],
  busy: false,
  noTools: false,
  applied: [],
  ctrl: null,

  newChat: async () => {
    await get().stop();
    set({ chatId: null, turns: [], noTools: false, applied: [] });
  },

  open: async (id) => {
    await get().stop();
    const c = await fetchChat(id);
    set({ chatId: c.id, turns: turnsOf(c), noTools: c.noTools, applied: c.appliedProposals });
  },

  dropped: (id) => {
    if (get().chatId === id) set({ chatId: null, turns: [], noTools: false, applied: [] });
  },

  markApplied: (key) => {
    set((s) => (s.applied.includes(key) ? s : { applied: [...s.applied, key] }));
  },

  send: async (text, req, scope, hooks = {}) => {
    if (get().busy || !text.trim()) return;
    const ctrl = new AbortController();
    const answer: AiTurn = { role: 'assistant', content: '', reasoning: '', steps: [], pending: true, scope };
    set((s) => ({ busy: true, ctrl, turns: [...s.turns, { role: 'user', content: text.trim() }, answer] }));
    try {
      let id = get().chatId;
      if (!id) {
        id = (await createChat()).id;
        set({ chatId: id });
      }
      await sendMessage(
        id,
        { ...req, text: text.trim() },
        (e) => {
          if (e.type === 'mode') set({ noTools: true });
          set((s) => ({ turns: lastAi(s.turns, (t) => applyEvent(t, e)) }));
        },
        ctrl.signal,
      );
      // a stream that ended without done or error (the connection dropped)
      set((s) => ({
        turns: lastAi(s.turns, (t) =>
          t.pending ? { ...t, pending: false, error: { code: 'interrupted', message: 'The answer was cut off.' } } : t,
        ),
      }));
    } catch (err) {
      const aborted = ctrl.signal.aborted;
      let error: TurnError | undefined;
      if (err instanceof ApiError) {
        error = { code: err.code, message: err.problem.detail ?? err.problem.title };
        if (err.code === 'consent_required') hooks.onConsent?.();
      } else if (!aborted) {
        error = { code: 'unreachable', message: 'The server could not be reached.' };
      }
      set((s) => ({
        turns: lastAi(s.turns, (t) =>
          aborted && !error
            ? { ...t, pending: false, content: t.content ? `${t.content}\n\n_Stopped._` : '_Stopped._' }
            : { ...t, pending: false, ...(error ? { error } : {}) },
        ),
      }));
    } finally {
      set({ busy: false, ctrl: null });
      hooks.onChanged?.();
    }
  },

  stop: async () => {
    const { busy, ctrl, chatId } = get();
    if (!busy || !ctrl) return;
    // the server stops the model and ends the stream with what was written; if that takes too
    // long, closing the connection stops it too
    const timer = setTimeout(() => {
      ctrl.abort();
    }, STOP_GRACE_MS);
    try {
      if (chatId) await stopRun(chatId);
      else ctrl.abort();
    } catch {
      ctrl.abort();
    }
    for (let i = 0; get().busy && i < 200; i++) await new Promise((r) => setTimeout(r, 25));
    clearTimeout(timer);
  },
}));
