// The assistant panel (legacy #chat, PLAN §7.13): recent chats, connection state, the conversation
// with its lookups, and the question box. ⌘/Ctrl J opens and closes it (shell/keys); the left edge
// resizes it (double-click resets) and the header maximises it.

import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useNavigate } from 'react-router';

import { useAccounts } from '../api/hooks/accounts';
import {
  useAssistantSettings,
  useAssistantStatus,
  useChats,
  useDeleteChat,
  useSaveAssistantSettings,
  type AssistantConnection,
  type Step,
} from '../api/hooks/assistant';
import { keys } from '../api/keys';
import { fmtDateTime, fmtDay } from '../lib/dates';
import { fmtInt, plural } from '../lib/format';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { uiScale } from '../styles/scale';
import { contextOf, withoutTransfers } from '../table/context';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { useChat, type AiTurn } from './chatStore';
import { ChatSettings, pick } from './ChatSettings';
import { TurnView } from './Turn';

const SUGGEST = [
  'How much did I spend last month, by category?',
  'Find my Amazon purchases from the last 30 days',
  'Which subscriptions or regular bills do I pay?',
  'What were my 5 biggest expenses this year?',
  'Compare my restaurant spending this month vs last month',
];

const MIN_W = 300;

export function ChatPanel({ viewId }: { viewId: string }) {
  const ai = useUi((s) => s.ai);
  const setAi = useUi((s) => s.setAi);
  const status = useAssistantStatus(ai.open);
  const settings = useAssistantSettings(ai.open);
  const saveSettings = useSaveAssistantSettings();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { turns, busy, send, stop, newChat, chatId } = useChat();
  const [showCfg, setShowCfg] = useState(false);
  const [consentFor, setConsentFor] = useState<AssistantConnection | null>(null);
  const [text, setText] = useState('');
  const body = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const near = useRef(true);
  const accounts = useAccounts().data ?? [];

  const { conn, model } = pick(status.data, ai);
  const enabled = status.data?.enabled ?? false;
  const dot = status.isLoading ? 'busy' : conn?.reachable ? 'on' : conn ? 'err' : '';

  // follow the answer while it streams, unless the user scrolled up
  useEffect(() => {
    const el = body.current;
    if (el && near.current) el.scrollTop = el.scrollHeight;
  }, [turns]);
  useEffect(() => {
    if (ai.open && !showCfg) input.current?.focus();
  }, [ai.open, showCfg]);

  const scoped = viewId === 'all' ? accounts : accounts.filter((a) => a.id === viewId);
  const rows = (ai.scope === 'view' ? scoped : accounts).reduce((n, a) => n + a.txCount, 0);
  const first = accounts
    .map((a) => a.firstOn)
    .filter(Boolean)
    .sort()[0];
  const lastOn = accounts
    .map((a) => a.lastOn)
    .filter(Boolean)
    .sort()
    .at(-1);

  const ask = (q: string) => {
    if (!q.trim() || busy) return;
    if (!conn) {
      setShowCfg(true);
      return;
    }
    const view = ai.scope === 'view';
    const v = viewOf(useViews.getState().views, viewId);
    setText('');
    near.current = true;
    void send(
      q,
      {
        scope: view ? 'view' : 'all',
        ...(view ? { context: withoutTransfers(contextOf(viewId, v)) } : {}),
        timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        settings: {
          connection: conn.name,
          ...(model ? { model } : {}),
          mode: ai.mode === 'tools' || ai.mode === 'plan' ? ai.mode : 'auto',
          think: ai.thinking,
          maxRows: Math.max(1, Math.min(50, ai.maxRows)),
          temperature: ai.temperature,
        },
      },
      { view, viewId: view ? viewId : 'all' },
      {
        onConsent: () => {
          setConsentFor(conn);
        },
        onChanged: () => void qc.invalidateQueries({ queryKey: keys.chats }),
      },
    );
  };

  // legacy showAiSelection: the lookup's filter in the table, as a removable "✦" chip
  const showInTable = (s: Step, t: AiTurn) => {
    if (!s.toolFilter) return;
    const target = t.scope.view ? t.scope.viewId : 'all';
    useViews.getState().patch(target, { toolFilter: s.toolFilter, period: 'all', custom: null, search: '' });
    useUi.getState().setMode('table');
    void navigate(`/t/${target}`);
    if (s.matched !== undefined) toast(`Showing ${plural(s.matched, 'matching row')}`);
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      ask(text);
    }
  };

  // resizing from the left edge (legacy chatRz): the width is logical px, scaled with the UI
  const startResize = (e: PointerEvent<HTMLDivElement>) => {
    const panel = e.currentTarget.parentElement;
    if (!panel) return;
    e.preventDefault();
    const startX = e.clientX;
    const startW = panel.getBoundingClientRect().width;
    const handle = e.currentTarget;
    handle.classList.add('on');
    const move = (ev: globalThis.PointerEvent) => {
      const main = panel.parentElement?.clientWidth ?? window.innerWidth;
      const w = Math.max(MIN_W * uiScale(), Math.min(main * 0.85, startW + startX - ev.clientX));
      panel.style.width = `${String(Math.round(w))}px`;
    };
    const up = () => {
      handle.classList.remove('on');
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      setAi({ width: Math.round(panel.getBoundingClientRect().width / uiScale()) });
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };

  const width = ai.width && !ai.max ? `${String(Math.round(ai.width * uiScale()))}px` : undefined;
  const cfgShown = showCfg || (enabled && !conn);

  return (
    <aside className="chat" id="chat" aria-label="Assistant" style={width ? { width } : undefined}>
      <div
        className="chat-rz"
        title="Drag to resize · double-click to reset"
        onPointerDown={startResize}
        onDoubleClick={() => {
          setAi({ width: 0 });
        }}
      />
      <div className="chat-h">
        <ChatMenu enabled={enabled} />
        <span
          className={`dot ${dot}`}
          title={conn?.reachable ? 'Connected' : conn ? 'Not connected' : 'Not configured'}
        />
        <span className="chat-model">
          {conn ? model || conn.label : enabled ? '' : 'not configured'}
          {conn?.cloud && <span className="badge cloud">cloud</span>}
        </span>
        <span className="spacer" />
        <IconButton
          icon="plus"
          title="New chat"
          disabled={!enabled}
          onClick={() => {
            void newChat();
            setShowCfg(false);
          }}
        />
        <IconButton
          icon={ai.max ? 'shrink' : 'expand'}
          title={ai.max ? 'Restore panel size' : 'Maximise'}
          on={ai.max}
          onClick={() => {
            setAi({ max: !ai.max });
          }}
        />
        <IconButton
          icon="cog"
          title="Assistant settings"
          on={showCfg}
          disabled={!enabled}
          onClick={() => {
            setShowCfg(!showCfg);
          }}
        />
        <IconButton
          icon="x"
          title="Close (⌘J)"
          onClick={() => {
            setAi({ open: false });
          }}
        />
      </div>
      <div
        className="chat-b"
        ref={body}
        onScroll={(e) => {
          const el = e.currentTarget;
          near.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        }}
      >
        {!status.isLoading && !enabled ? (
          <div className="chat-empty">
            <Icon name="ai" />
            <h3>The assistant isn’t set up</h3>
            <p className="hint">
              Your admin can connect a model server (LM Studio, Ollama or a cloud service) with FINUANCE_AI_BASE_URL or
              a connections file. See docs/assistant.md.
            </p>
          </div>
        ) : cfgShown ? (
          <ChatSettings
            status={status.data}
            onCloud={setConsentFor}
            onDone={() => {
              setShowCfg(false);
            }}
          />
        ) : turns.length === 0 ? (
          <div className="chat-empty">
            <Icon name="ai" />
            <h3>Ask about your money</h3>
            <p className="hint">
              {rows > 0
                ? `${fmtInt(rows)} transactions${first && lastOn ? ` · ${fmtDay(first)} – ${fmtDay(lastOn)}` : ''}. The model only receives what it looks up — never a full dump.`
                : 'Import CSV files first.'}
            </p>
            {conn && !conn.reachable && conn.error && (
              <div className="conn-err">
                {conn.error.message}{' '}
                <button
                  type="button"
                  className="link"
                  onClick={() => {
                    setShowCfg(true);
                  }}
                >
                  Assistant settings
                </button>
              </div>
            )}
            <div className="sugg">
              {SUGGEST.map((q) => (
                <button
                  type="button"
                  key={q}
                  onClick={() => {
                    ask(q);
                  }}
                >
                  {q}
                </button>
              ))}
            </div>
          </div>
        ) : (
          turns.map((t, i) => (
            <TurnView key={i} turn={t} last={i === turns.length - 1} chatId={chatId} onShow={showInTable} />
          ))
        )}
      </div>
      <form
        className="chat-f"
        hidden={cfgShown || !enabled}
        autoComplete="off"
        onSubmit={(e) => {
          e.preventDefault();
          if (busy) void stop();
          else ask(text);
        }}
      >
        <div className="chat-in">
          <textarea
            ref={input}
            rows={1}
            value={text}
            placeholder="Ask about your transactions…"
            aria-label="Question"
            onChange={(e) => {
              setText(e.target.value);
            }}
            onKeyDown={onKey}
          />
          <IconButton icon={busy ? 'stop' : 'send'} title={busy ? 'Stop' : 'Send (Enter)'} type="submit" />
        </div>
        <div className="chat-hint">
          {`${ai.scope === 'view' ? 'Current view' : 'All data'} · ${plural(rows, 'row')} · Enter to send, Shift+Enter for a new line`}
          {chatId && settings.data?.retentionDays != null
            ? ` · kept ${plural(settings.data.retentionDays, 'day')}`
            : ''}
        </div>
      </form>
      <Modal
        open={!!consentFor}
        size="sm"
        title="Send data to a cloud service?"
        onClose={() => {
          setConsentFor(null);
        }}
      >
        {consentFor && (
          <div className="conn">
            <p>
              <b>{consentFor.label}</b> runs outside this server. Your questions and the transactions the model looks up
              (payees, amounts, dates, titles) will be sent there. Long numbers such as account numbers are shortened
              first.
            </p>
            <p className="hint">You can withdraw this in the assistant settings at any time.</p>
            <div className="row2">
              <Button
                variant="primary"
                disabled={!settings.data || saveSettings.isPending}
                onClick={() => {
                  const s = settings.data;
                  if (!s) return;
                  saveSettings.mutate(
                    { ...s, cloudConsent: [...s.cloudConsent.filter((n) => n !== consentFor.name), consentFor.name] },
                    {
                      onSuccess: () => {
                        setConsentFor(null);
                      },
                    },
                  );
                }}
              >
                Allow {consentFor.label}
              </Button>
              <Button
                variant="ghost"
                onClick={() => {
                  setConsentFor(null);
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </aside>
  );
}

/** Recent chats (legacy renderChatMenu): opens on hover or click; each chat can be deleted. */
function ChatMenu({ enabled }: { enabled: boolean }) {
  const chats = useChats(enabled);
  const del = useDeleteChat();
  const { chatId, open, newChat, dropped } = useChat();
  const [state, setState] = useState<'' | 'open' | 'shut'>('');
  const list = chats.data?.chats ?? [];
  const max = chats.data?.maxChats ?? 25;
  const title = list.find((c) => c.id === chatId)?.title;
  return (
    <div
      className={`chat-hist ${state}`}
      onPointerLeave={() => {
        setState('');
      }}
    >
      <button
        type="button"
        className="chat-ttl"
        title="Recent chats"
        aria-expanded={state === 'open'}
        onClick={() => {
          setState(state === 'open' ? 'shut' : 'open');
        }}
      >
        <Icon name="ai" />
        <span>{title ? title : 'Assistant'}</span>
        <Icon name="chev" className="chev" />
      </button>
      <div className="chat-menu">
        <div className="cm">
          <div className="cm-h">
            Recent chats{' '}
            <span>
              {list.length}/{max}
            </span>
          </div>
          {list.length ? (
            list.map((c) => (
              <div
                key={c.id}
                className={c.id === chatId ? 'cm-i on' : 'cm-i'}
                title={c.title}
                role="button"
                tabIndex={0}
                onClick={() => {
                  setState('shut');
                  void open(c.id).catch(() => {
                    toast('This chat is gone (expired or deleted)', { kind: 'error' });
                  });
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') e.currentTarget.click();
                }}
              >
                <div className="cm-t">{fmtDateTime(c.updatedAt)}</div>
                <IconButton
                  icon="x"
                  size="sm"
                  title="Delete this chat"
                  onClick={(e) => {
                    e.stopPropagation();
                    del.mutate(c.id, {
                      onSuccess: () => {
                        dropped(c.id);
                      },
                    });
                  }}
                />
                <div className="cm-s">{c.title || '…'}</div>
              </div>
            ))
          ) : (
            <div className="cm-e">No chats yet — they appear here after the first question.</div>
          )}
          <button
            type="button"
            className="cm-new"
            onClick={() => {
              setState('shut');
              void newChat();
            }}
          >
            <Icon name="plus" />
            New chat
          </button>
        </div>
      </div>
    </div>
  );
}
