// The assistant's API (PLAN §6.2, ADR 0015): connections and models, the user's settings, chats,
// and the streamed answer of a question (server-sent events, read by ../../assistant/sse).

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { readEvents } from '../../assistant/sse';
import { api, ApiError, data, toProblem, type Schemas } from '../client';
import { keys } from '../keys';
import { invalidateGroups } from './groups';
import { invalidateClassified } from './rules';

export type AssistantStatus = Schemas['AssistantStatus'];
export type AssistantConnection = Schemas['AssistantConnection'];
export type AssistantSettings = Schemas['AssistantSettings'];
export type ChatSummary = Schemas['AssistantChat'];
export type ChatDetail = Schemas['AssistantChatDetail'];
export type Turn = Schemas['AssistantTurn'];
export type Step = Schemas['AssistantStep'];
export type Meta = Schemas['AssistantMeta'];
export type MessageRequest = Schemas['AssistantMessageRequest'];

/** Connections and models; the server caches the model lists for 60 s. */
export function useAssistantStatus(enabled = true) {
  return useQuery({
    queryKey: keys.assistantStatus,
    queryFn: async () => data(await api.GET('/assistant/status')),
    staleTime: 60_000,
    enabled,
  });
}

/** Checks the model servers again (the "Test" button). */
export function useRefreshStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => data(await api.GET('/assistant/status', { params: { query: { refresh: true } } })),
    onSuccess: (s) => {
      qc.setQueryData(keys.assistantStatus, s);
    },
  });
}

export function useAssistantSettings(enabled = true) {
  return useQuery({
    queryKey: keys.assistantSettings,
    queryFn: async () => data(await api.GET('/assistant/settings')),
    enabled,
  });
}

export function useSaveAssistantSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (s: AssistantSettings) => data(await api.PUT('/assistant/settings', { body: s })),
    onSuccess: (s) => {
      qc.setQueryData(keys.assistantSettings, s);
      // a shorter retention may have removed chats
      void qc.invalidateQueries({ queryKey: keys.chats });
    },
  });
}

export function useChats(enabled = true) {
  return useQuery({
    queryKey: keys.chats,
    queryFn: async () => data(await api.GET('/assistant/chats')),
    enabled,
  });
}

export async function createChat(): Promise<ChatSummary> {
  return data(await api.POST('/assistant/chats'));
}

export async function fetchChat(id: string): Promise<ChatDetail> {
  return data(await api.GET('/assistant/chats/{id}', { params: { path: { id } } }));
}

export function useDeleteChat() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      data(await api.DELETE('/assistant/chats/{id}', { params: { path: { id } } }));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: keys.chats }),
  });
}

export async function stopRun(chatId: string): Promise<void> {
  data(await api.POST('/assistant/chats/{id}/stop', { params: { path: { id: chatId } } }));
}

/** One event of a streamed answer, parsed. */
export type RunEvent =
  | { type: 'content' | 'reasoning'; delta: string }
  | { type: 'step'; step: Step & { index: number } }
  | { type: 'mode'; mode: 'plan'; reason: string }
  | { type: 'done'; messageSeq: number; meta: Meta }
  | { type: 'error'; code: string; message: string };

const str = (v: unknown, def: string) => (typeof v === 'string' ? v : def);

function parse(event: string, raw: string): RunEvent | null {
  let d: unknown;
  try {
    d = JSON.parse(raw);
  } catch {
    return null;
  }
  const o = d as Record<string, unknown>;
  switch (event) {
    case 'content':
    case 'reasoning':
      return { type: event, delta: str(o['delta'], '') };
    case 'step':
      return { type: 'step', step: d as Step & { index: number } };
    case 'mode':
      return { type: 'mode', mode: 'plan', reason: str(o['reason'], '') };
    case 'done':
      return { type: 'done', messageSeq: Number(o['messageSeq'] ?? 0), meta: o['meta'] as Meta };
    case 'error':
      return { type: 'error', code: str(o['code'], 'internal'), message: str(o['message'], '') };
  }
  return null;
}

/**
 * POST /assistant/chats/{id}/messages and reads the events until the stream ends. A problem before
 * the stream starts (409, 429, 403 consent_required, 503) is thrown as an ApiError. Aborting the
 * signal closes the connection, which stops the run on the server.
 */
export async function sendMessage(
  chatId: string,
  body: MessageRequest,
  onEvent: (e: RunEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const res = await globalThis.fetch(
    `${window.location.origin}/api/v1/assistant/chats/${encodeURIComponent(chatId)}/messages`,
    {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-Finuance-Client': 'web', Accept: 'text/event-stream' },
      body: JSON.stringify(body),
      signal,
    },
  );
  if (!res.ok || !res.body) throw new ApiError(await toProblem(res));
  await readEvents(res.body, (e) => {
    const ev = parse(e.event, e.data);
    if (ev) onEvent(ev);
  });
}

export type Proposal = Schemas['ActionProposal'];

/** The category's id: the proposal's, else created now (or found, if it was created meanwhile). */
async function categoryFor(p: Proposal): Promise<string> {
  if (p.categoryId) return p.categoryId;
  const name = p.categoryName ?? '';
  const r = await api.POST('/categories', { body: { name } });
  if (r.response.status === 409) {
    const all = data(await api.GET('/categories'));
    const hit = all.find((c) => c.name.toLocaleLowerCase() === name.toLocaleLowerCase());
    if (hit) return hit.id;
  }
  return data(r).id;
}

async function groupFor(p: Proposal): Promise<string> {
  if (p.groupId) return p.groupId;
  const name = p.groupName ?? '';
  const r = await api.POST('/groups', { body: { name } });
  if (r.response.status === 409) {
    const hit = data(await api.GET('/groups')).find((g) => g.name.toLocaleLowerCase() === name.toLocaleLowerCase());
    if (hit) return hit.id;
  }
  return data(r).id;
}

/**
 * Applies an assistant proposal (PLAN 15.7) through the normal endpoints with the user's session:
 * the category or group first when it is new, then the bulk change or the rule. The main request
 * carries X-Assistant-Chat, so the server logs it as an applied proposal. Returns how many
 * transactions changed (set_*) or the rule's current matches (create_rule).
 */
export async function applyProposal(p: Proposal, chatId: string): Promise<number> {
  const header = { 'X-Assistant-Chat': chatId };
  switch (p.action) {
    case 'set_category': {
      const id = await categoryFor(p);
      const r = await api.POST('/transactions/bulk', {
        params: { header },
        body: { ids: p.ids ?? [], category: { id } },
      });
      return data(r).updated;
    }
    case 'set_group': {
      const groupId = await groupFor(p);
      const r = await api.POST('/transactions/bulk', {
        params: { header },
        body: { ids: p.ids ?? [], set: { groupId } },
      });
      return data(r).updated;
    }
    case 'create_rule': {
      const setCategoryId = await categoryFor(p);
      data(
        await api.POST('/rules', {
          params: { header },
          body: {
            name: p.ruleName ?? p.categoryName ?? 'Rule',
            enabled: true,
            match: p.match ?? {},
            setCategoryId,
            markTransfer: false,
          },
        }),
      );
      return p.matched;
    }
  }
}

/** applyProposal as a mutation; afterwards everything that shows categories, groups or rules refetches. */
export function useApplyProposal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ proposal, chatId }: { proposal: Proposal; chatId: string }) => applyProposal(proposal, chatId),
    onSuccess: () => {
      invalidateClassified(qc);
      invalidateGroups(qc);
      void qc.invalidateQueries({ queryKey: keys.rules });
    },
  });
}
