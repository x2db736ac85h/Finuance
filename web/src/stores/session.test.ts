import { afterEach, describe, expect, it } from 'vitest';

import { useAnswerNotes } from '../assistant/answerNotes';
import { useChat } from '../assistant/chatStore';
import { useImportQueue } from '../import/queue';
import { resetSession, startPrefsSync } from './persist';
import { useSelection } from './selectionStore';

const doc = { version: 1, prefs: {} };

/** What one person leaves in memory: a chat with an answer, a selection, a queued upload. */
function leaveTraces() {
  useChat.setState({
    chatId: 'chat-of-alex',
    turns: [
      { role: 'user', content: 'What did I pay at the pharmacy?' },
      {
        role: 'assistant',
        content: 'APTEKA ZDROWIE, 30,13',
        reasoning: '',
        steps: [],
        pending: false,
        scope: { view: false, viewId: 'all' },
      },
    ],
    applied: ['proposal-1'],
  });
  useAnswerNotes
    .getState()
    .add({ chatId: 'chat-of-alex', question: 'What did I pay at the pharmacy?', at: '', failed: false });
  useSelection.setState({
    viewId: 'all',
    rows: new Map([['tx-1', { amountMinor: -3013, grouped: false }]]),
    anchor: 3,
  });
  useImportQueue.setState({ items: [{ key: 1, name: 'alex-bank-export.csv', status: 'preview', importId: 'imp-1' }] });
}

function expectEmpty() {
  expect(useChat.getState().chatId).toBeNull();
  expect(useChat.getState().turns).toEqual([]);
  expect(useChat.getState().applied).toEqual([]);
  expect(useAnswerNotes.getState().notes).toEqual([]);
  expect(useSelection.getState().rows.size).toBe(0);
  expect(useSelection.getState().viewId).toBeNull();
  expect(useImportQueue.getState().items).toEqual([]);
}

describe('what the app holds in memory about the signed-in person (WEB-1)', () => {
  afterEach(() => {
    resetSession();
  });

  it('is forgotten at sign-out, and the stores still work afterwards', () => {
    leaveTraces();
    let aborted = false;
    const ctrl = new AbortController();
    ctrl.signal.addEventListener('abort', () => {
      aborted = true;
    });
    useChat.setState({ ctrl, busy: true }); // an answer still streaming
    resetSession();
    expectEmpty();
    expect(aborted).toBe(true);
    expect(useChat.getState().busy).toBe(false);
    // the stores keep their actions (a reset that replaced them with plain data would break the app)
    expect(typeof useChat.getState().stop).toBe('function');
    expect(typeof useSelection.getState().clear).toBe('function');
  });

  it('is forgotten when another account signs in in the same tab, and kept for the same one', () => {
    const stopAlex = startPrefsSync('user-alex', doc);
    leaveTraces();
    // Alex's preferences load again (a reload of the document): the chat on screen stays
    stopAlex();
    const stopAlexAgain = startPrefsSync('user-alex', doc);
    expect(useChat.getState().chatId).toBe('chat-of-alex');
    expect(useImportQueue.getState().items).toHaveLength(1);
    // Sam signs in without a page reload: nothing of Alex's is left
    stopAlexAgain();
    const stopSam = startPrefsSync('user-sam', doc);
    expectEmpty();
    stopSam();
  });
});
