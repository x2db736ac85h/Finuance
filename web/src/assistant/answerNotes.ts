// "The assistant answered" in the bell (Phase 26, U10). An answer that finishes while nobody is
// looking at it (the panel is closed, or the browser tab is in the background) leaves a note the
// bell shows until its chat is opened. In this browser only, for as long as the page is open: an
// answer exists only while the page that asked is open (the run ends with its request), so there is
// nothing for the server to keep. Answers that outlive the tab are in the Backlog.

import { create } from 'zustand';

import { useUi } from '../stores/uiStore';
import { useChat } from './chatStore';

export type AnswerNote = {
  /** One note per chat: a newer answer in the same chat replaces it. */
  chatId: string;
  /** The question it answers, shortened, to tell chats apart. */
  question: string;
  at: string;
  failed: boolean;
  /** The bell was opened since: it no longer counts as unread. */
  seen: boolean;
};

type Notes = {
  notes: AnswerNote[];
  add: (n: Omit<AnswerNote, 'seen'>) => void;
  /** The bell was opened: nothing is unread, the notes stay listed. */
  markSeen: () => void;
  /** The chat is on screen: its note has done its job. */
  drop: (chatId: string) => void;
  reset: () => void;
};

const MAX_NOTES = 20;
const MAX_QUESTION = 60;

export const useAnswerNotes = create<Notes>()((set) => ({
  notes: [],
  add: (n) => {
    set((s) => ({
      notes: [{ ...n, seen: false }, ...s.notes.filter((x) => x.chatId !== n.chatId)].slice(0, MAX_NOTES),
    }));
  },
  markSeen: () => {
    set((s) => (s.notes.some((n) => !n.seen) ? { notes: s.notes.map((n) => ({ ...n, seen: true })) } : s));
  },
  drop: (chatId) => {
    set((s) => (s.notes.some((n) => n.chatId === chatId) ? { notes: s.notes.filter((n) => n.chatId !== chatId) } : s));
  },
  reset: () => {
    set({ notes: [] });
  },
}));

/** True while the person can see the assistant's panel: it is open, in a tab that is in front. */
export function answerInView(): boolean {
  return useUi.getState().ai.open && document.visibilityState === 'visible';
}

/** An answer ended (or failed): a note for the bell, unless it ended in front of the person. */
export function noteAnswer(chatId: string, question: string, failed: boolean): void {
  if (answerInView()) return;
  const q = question.trim().replace(/\s+/g, ' ');
  useAnswerNotes.getState().add({
    chatId,
    question: q.length > MAX_QUESTION ? `${q.slice(0, MAX_QUESTION - 1)}…` : q,
    at: new Date().toISOString(),
    failed,
  });
}

/** The note of the chat on screen goes once the person can see that chat. */
function dropSeen(): void {
  const id = useChat.getState().chatId;
  if (id && answerInView()) useAnswerNotes.getState().drop(id);
}

/** A run of the chat on screen just ended: its last question and how the answer came out. */
function runEnded(): void {
  const { chatId, turns } = useChat.getState();
  const answer = turns.at(-1);
  const question = turns.at(-2);
  if (!chatId || answer?.role !== 'assistant' || question?.role !== 'user') return;
  // stopped by the person: they know
  if (!answer.error && answer.content.endsWith('_Stopped._')) return;
  // an answer that needs their agreement first is shown as a dialog, not as an answer
  if (answer.error?.code === 'consent_required') return;
  noteAnswer(chatId, question.content, !!answer.error);
}

/**
 * Wires the notes to the chat: a run ending makes one, and whatever puts a chat in front of the
 * person (the panel opening, another chat, the tab coming back) takes its note away. Once per page.
 */
export function watchAnswerNotes(): () => void {
  const offUi = useUi.subscribe((s, prev) => {
    if (s.ai.open && !prev.ai.open) dropSeen();
  });
  const offChat = useChat.subscribe((s, prev) => {
    if (prev.busy && !s.busy) runEnded();
    if (s.chatId !== prev.chatId) dropSeen();
  });
  document.addEventListener('visibilitychange', dropSeen);
  return () => {
    offUi();
    offChat();
    document.removeEventListener('visibilitychange', dropSeen);
  };
}
