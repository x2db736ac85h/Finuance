// The assistant's API (PLAN §6.2, ADR 0015): connections and models, the user's settings, chats,
// and the streamed answer of a question (server-sent events, read by ../../assistant/sse).

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

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

/** Asks for a digest now (ADR 0033): the worker writes it, and a notification says when it is ready. */
export function useRequestDigest() {
  return useMutation({
    mutationFn: async () => {
      data(await api.POST('/assistant/digest'));
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

export type Attachment = Schemas['AssistantAttachment'];

/** POST /assistant/chats/{id}/attachments: a JPEG for the chat's next question. */
export async function uploadAttachment(chatId: string, jpeg: Blob): Promise<Attachment> {
  const res = await globalThis.fetch(
    `${window.location.origin}/api/v1/assistant/chats/${encodeURIComponent(chatId)}/attachments`,
    {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'image/jpeg', 'X-Finuance-Client': 'web' },
      body: jpeg,
    },
  );
  if (!res.ok) throw new ApiError(await toProblem(res));
  return (await res.json()) as Attachment;
}

/** Where a chat's photo is shown from (same origin; the session cookie authorises it). */
export const attachmentUrl = (chatId: string, id: string) =>
  `/api/v1/assistant/chats/${encodeURIComponent(chatId)}/attachments/${encodeURIComponent(id)}`;

export type Proposal = Schemas['ActionProposal'];

/** The assistant headers of an Apply: the server records the change as the assistant's (ADR 0030). */
type ApplyHeader = { 'X-Assistant-Chat': string; 'X-Assistant-Proposal': string; 'X-Assistant-Auto'?: boolean };

/** The category's id: the proposal's, else created now (or found, if it was created meanwhile). */
async function categoryFor(p: Proposal, header: ApplyHeader): Promise<string> {
  if (p.categoryId) return p.categoryId;
  const name = p.categoryName ?? '';
  const r = await api.POST('/categories', { params: { header }, body: { name } });
  if (r.response.status === 409) {
    const all = data(await api.GET('/categories'));
    const hit = all.find((c) => c.name.toLocaleLowerCase() === name.toLocaleLowerCase());
    if (hit) return hit.id;
  }
  return data(r).id;
}

async function groupFor(p: Proposal, header: ApplyHeader): Promise<string> {
  if (p.groupId) return p.groupId;
  const name = p.groupName ?? '';
  const r = await api.POST('/groups', { params: { header }, body: { name } });
  if (r.response.status === 409) {
    const hit = data(await api.GET('/groups')).find((g) => g.name.toLocaleLowerCase() === name.toLocaleLowerCase());
    if (hit) return hit.id;
  }
  return data(r).id;
}

/**
 * The target a proposal needs and lacks, or null (WEB-2). A proposal is never applied with an id
 * left out: for a shared group that would mean "no group", for a path the wrong route.
 */
export function missingTarget(p: Proposal): string | null {
  const ids = (p.ids?.length ?? 0) > 0;
  switch (p.action) {
    case 'set_category':
    case 'set_group':
      return ids ? null : 'the transactions';
    case 'set_budget':
      return p.categoryId ? null : 'the category';
    case 'add_expense':
      return p.accountId ? null : 'the account';
    case 'add_to_shared_group':
      return !ids ? 'the transactions' : p.groupId ? null : 'the shared group';
    case 'split_expense':
      return p.sharedGroupId && p.expenseId ? null : 'the shared expense';
    case 'create_goal':
      return p.goal ? null : 'the goal';
    case 'update_goal':
      return p.goal?.id ? null : 'the goal';
    case 'attach_receipt':
      return p.ids?.[0] ? null : 'the transaction';
    case 'remember':
      return p.memoryLine?.trim() ? null : 'the line to remember';
    default:
      return null;
  }
}

/**
 * Applies an assistant proposal (PLAN 15.7) through the normal endpoints with the user's session:
 * the category or group first when it is new, then the bulk change or the rule. Every request
 * carries X-Assistant-Chat and X-Assistant-Proposal: the server records the change as the
 * assistant's (one change for the whole Apply, undoable, ADR 0030) and in the activity log, and the
 * chat shows it as applied after a reload. `auto` marks an Apply the panel made by itself. Returns
 * how many transactions changed (set_*) or the rule's current matches (create_rule).
 */
export async function applyProposal(p: Proposal, chatId: string, auto = false): Promise<number> {
  const missing = missingTarget(p);
  if (missing) {
    throw new ApiError({
      type: '/problems/validation_failed',
      code: 'validation_failed',
      status: 422,
      title: 'Validation failed',
      detail: `This proposal doesn't say which ${missing.replace(/^the /, '')} it is for, so it wasn't applied. Ask the assistant again.`,
    });
  }
  const header: ApplyHeader = {
    'X-Assistant-Chat': chatId,
    'X-Assistant-Proposal': p.key,
    ...(auto ? { 'X-Assistant-Auto': true } : {}),
  };
  switch (p.action) {
    case 'set_category': {
      // take a hand-set category off again (Phase 27): back to the bank's or a rule's
      if (p.clearCategory) {
        const r = await api.POST('/transactions/bulk', {
          params: { header },
          body: { ids: p.ids ?? [], category: { reset: true } },
        });
        return data(r).updated;
      }
      const id = await categoryFor(p, header);
      const r = await api.POST('/transactions/bulk', {
        params: { header },
        body: { ids: p.ids ?? [], category: { id } },
      });
      return data(r).updated;
    }
    case 'set_group': {
      // out of whatever group they are in; nothing is created for it
      const groupId = p.removeGroup ? null : await groupFor(p, header);
      const r = await api.POST('/transactions/bulk', {
        params: { header },
        body: { ids: p.ids ?? [], set: { groupId } },
      });
      return data(r).updated;
    }
    case 'create_rule': {
      const setCategoryId = await categoryFor(p, header);
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
    case 'set_budget': {
      const r = await api.PUT('/budgets/{categoryId}', {
        params: { path: { categoryId: p.categoryId ?? '' }, header },
        body: { amountMinor: p.removeBudget ? null : (p.budget?.amountMinor ?? null) },
      });
      if (r.error) data(r);
      return 1;
    }
    case 'add_expense': {
      const categoryId = p.categoryId ?? (p.categoryName ? await categoryFor(p, header) : undefined);
      data(
        await api.POST('/transactions', {
          params: { header },
          body: {
            accountId: p.accountId ?? '',
            bookedOn: p.date ?? '',
            amountMinor: p.amount?.amountMinor ?? 0,
            payee: p.payee ?? '',
            ...(categoryId ? { categoryId } : {}),
            ...(p.note ? { note: p.note } : {}),
          },
        }),
      );
      return 1;
    }
    case 'add_to_shared_group': {
      const r = await api.POST('/transactions/bulk', {
        params: { header },
        body: { ids: p.ids ?? [], set: { groupId: p.groupId ?? null } },
      });
      return data(r).updated;
    }
    case 'split_expense': {
      const sp = p.split;
      const members = sp?.members ?? [];
      // a typed expense takes every field back unchanged; a copy only its split
      const r = await api.PATCH('/shared-groups/{id}/expenses/{expenseId}', {
        params: { path: { id: p.sharedGroupId ?? '', expenseId: p.expenseId ?? '' }, header },
        body: {
          bookedOn: p.date ?? '',
          amountMinor: p.amount?.amountMinor ?? 0,
          payee: p.payee ?? '',
          category: p.expenseCategory ?? '',
          note: p.note ?? '',
          ...(p.paidBy ? { paidBy: p.paidBy } : {}),
          split:
            sp?.mode === 'exact'
              ? { mode: 'exact', exact: members.map((m) => ({ memberId: m.memberId, shareMinor: m.amountMinor ?? 0 })) }
              : sp?.mode === 'shares'
                ? {
                    mode: 'shares',
                    members: members.map((m) => m.memberId),
                    weights: members.map((m) => m.weight ?? 1),
                  }
                : { mode: 'equal', members: members.map((m) => m.memberId) },
        },
      });
      if (r.error) data(r);
      return 1;
    }
    case 'create_goal':
    case 'update_goal': {
      const g = p.goal;
      if (!g) return 0;
      const fields = {
        name: g.name,
        deadline: g.deadline,
        baselineMonths: g.baselineMonths,
        ...(g.targetMinor !== undefined ? { targetMinor: g.targetMinor } : {}),
        ...(g.accountId ? { accountId: g.accountId } : {}),
        ...(g.categoryId ? { categoryId: g.categoryId } : {}),
        ...(g.reducePct !== undefined ? { reducePct: g.reducePct } : {}),
      };
      if (p.action === 'create_goal') {
        data(await api.POST('/goals', { params: { header }, body: { ...fields, kind: g.kind, currency: g.currency } }));
      } else {
        // the whole goal as proposed, with the version read then: a goal changed since is a 409
        data(
          await api.PATCH('/goals/{id}', {
            params: { path: { id: g.id ?? '' }, header },
            body: { ...fields, status: g.status ?? 'active', version: g.version ?? 0 },
          }),
        );
      }
      return 1;
    }
    case 'attach_receipt': {
      // the receipt goes into the matched transaction's note (ADR 0032), with a category if proposed
      const categoryId = p.categoryId ?? (p.categoryName ? await categoryFor(p, header) : undefined);
      data(
        await api.PATCH('/transactions/{id}', {
          params: { path: { id: p.ids?.[0] ?? '' }, header },
          body: { note: p.note ?? '', ...(categoryId ? { categoryId } : {}) },
        }),
      );
      return 1;
    }
    case 'remember': {
      // The person's memory is one of their settings, not a change to their data: the line is
      // added through the settings' own request, without the headers that record a data change
      // (ADR 0046). They read, change and clear it in Settings → General.
      const cur = data(await api.GET('/assistant/settings'));
      const memory = cur.memory ?? { enabled: true, auto: false, text: '' };
      const text = addMemoryLine(memory.text, p.memoryLine ?? '');
      if (text !== memory.text)
        data(await api.PUT('/assistant/settings', { body: { ...cur, memory: { ...memory, text } } }));
      return 1;
    }
  }
}

/** The longest memory, as the server has it. */
export const MEMORY_MAX_CHARS = 4000;

/**
 * A memory with one more line. The same line twice stays once (a proposal applied again after a
 * reload). Throws with a sentence for the person when the note would get too long.
 */
export function addMemoryLine(text: string, line: string): string {
  const add = line.replace(/\s+/g, ' ').trim();
  if (!add) return text;
  const has = text.split('\n').some((l) => l.trim().toLocaleLowerCase() === add.toLocaleLowerCase());
  if (has) return text;
  const next = text.trim() ? `${text.trim()}\n${add}` : add;
  if (next.length > MEMORY_MAX_CHARS)
    throw new ApiError({
      type: '/problems/validation_failed',
      code: 'validation_failed',
      status: 422,
      title: 'Validation failed',
      detail: 'Your memory is full. Shorten it in Settings → General, then apply this again.',
    });
  return next;
}

/** applyProposal as a mutation; afterwards everything that shows categories, groups or rules refetches. */
export function useApplyProposal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ proposal, chatId, auto }: { proposal: Proposal; chatId: string; auto?: boolean }) =>
      applyProposal(proposal, chatId, auto ?? false),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.assistantSettings });
      invalidateClassified(qc);
      invalidateGroups(qc);
      void qc.invalidateQueries({ queryKey: keys.rules });
      void qc.invalidateQueries({ queryKey: keys.aiChanges });
      void qc.invalidateQueries({ queryKey: keys.accounts }); // a typed expense changes an account's count
      void qc.invalidateQueries({ queryKey: keys.shared });
      void qc.invalidateQueries({ queryKey: keys.goals });
    },
  });
}

export type AIChange = Schemas['AIChange'];

/** The changes the assistant made, newest first (ADR 0030). */
export function useAIChanges(enabled = true) {
  return useInfiniteQuery({
    queryKey: keys.aiChanges,
    enabled,
    queryFn: async ({ pageParam }) =>
      data(await api.GET('/ai-changes', { params: { query: pageParam ? { cursor: pageParam } : {} } })),
    initialPageParam: '',
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** Undoes one change; everything that shows categories, groups, rules or the activity refetches. */
export function useRevertAIChange() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => data(await api.POST('/ai-changes/{id}/revert', { params: { path: { id } } })),
    onSuccess: () => {
      invalidateClassified(qc);
      invalidateGroups(qc);
      void qc.invalidateQueries({ queryKey: keys.rules });
      void qc.invalidateQueries({ queryKey: keys.aiChanges });
      void qc.invalidateQueries({ queryKey: keys.activity });
      void qc.invalidateQueries({ queryKey: keys.accounts });
      void qc.invalidateQueries({ queryKey: keys.shared });
      void qc.invalidateQueries({ queryKey: keys.goals });
    },
  });
}

/** Every change of the assistant's, newest first (all pages; a person has dozens, not thousands). */
export async function fetchAllAIChanges(): Promise<AIChange[]> {
  const out: AIChange[] = [];
  let cursor = '';
  // a bound on the pages, so a server that kept answering "there is more" can't hold the page
  for (let page = 0; page < 200; page++) {
    const r = data(await api.GET('/ai-changes', { params: { query: cursor ? { cursor } : {} } }));
    out.push(...r.changes);
    if (!r.next) break;
    cursor = r.next;
  }
  return out;
}

/** All of the assistant's changes, read afresh each time it is asked for (the bulk undo counts them). */
export function useAllAIChanges() {
  return useQuery({ queryKey: [...keys.aiChanges, 'all'], queryFn: fetchAllAIChanges, staleTime: 0, gcTime: 0 });
}

/** The changes still in effect that were applied on the days from..to (either end may be open). */
export function openChangesIn(changes: readonly AIChange[], from: string, to: string): AIChange[] {
  return changes.filter((c) => {
    if (c.revertedAt) return false;
    const day = c.appliedAt.slice(0, 10);
    return (!from || day >= from) && (!to || day <= to);
  });
}

/**
 * Undoes several changes, newest first, one request each (Phase 27). A change that can't be undone
 * because something it touched was changed since is left as it is and counted; any other failure
 * stops the run. Everything that shows what a change touched refetches at the end.
 */
export function useRevertAIChanges() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (changes: readonly AIChange[]) => {
      let undone = 0;
      let left = 0;
      const order = [...changes].sort((a, b) => b.appliedAt.localeCompare(a.appliedAt));
      for (const c of order) {
        try {
          data(await api.POST('/ai-changes/{id}/revert', { params: { path: { id: c.id } } }));
          undone++;
        } catch (e) {
          // 409: something it touched was changed since, or it was undone meanwhile. It stays.
          if (e instanceof ApiError && e.status === 409) left++;
          else throw e;
        }
      }
      return { undone, left };
    },
    onSettled: () => {
      invalidateClassified(qc);
      invalidateGroups(qc);
      for (const key of [keys.rules, keys.aiChanges, keys.activity, keys.accounts, keys.shared, keys.goals])
        void qc.invalidateQueries({ queryKey: key });
    },
  });
}
