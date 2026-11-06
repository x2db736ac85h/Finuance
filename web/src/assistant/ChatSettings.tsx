// The assistant's settings inside the panel (legacy connForm, PLAN task 13.7). The server address is
// instance config, so the user picks a connection and a model; the rest are the legacy run
// settings (saved in the preferences) and the chat retention (a server setting).

import {
  useAssistantSettings,
  useRefreshStatus,
  useSaveAssistantSettings,
  type AssistantConnection,
  type AssistantStatus,
} from '../api/hooks/assistant';
import type { Schemas } from '../api/client';
import { THINKING, type AiPrefs, type Thinking } from '../stores/prefs';
import { useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { Select, type Option } from '../ui/inputs';
import { fixFor } from './errors';
import { AUTO_APPLY_MAX_ROWS } from './Proposal';

/** The connection and model the panel uses: the saved choice when offered, else the defaults. */
export function pick(status: AssistantStatus | undefined, ai: Pick<AiPrefs, 'connection' | 'model'>) {
  const conns = status?.connections ?? [];
  const conn = conns.find((c) => c.name === ai.connection) ?? conns[0];
  if (!conn) return { conn: undefined, model: '' };
  const offered =
    conn.models.some((m) => m.id === ai.model) || (!conn.reachable && ai.model !== '' && conn.name === ai.connection);
  return { conn, model: offered ? ai.model : (conn.defaultModel ?? '') };
}

const MODES: Option<string>[] = [
  { value: 'auto', label: 'Auto — tool calling, falls back to plan mode' },
  { value: 'tools', label: 'Tool calling' },
  { value: 'plan', label: 'Plan & answer (any model)' },
];
const THINK_LABEL: Record<Thinking, string> = {
  off: 'Off — fastest',
  auto: 'Model default',
  low: 'Low',
  medium: 'Medium',
  high: 'High — slowest, best on hard questions',
};
const SCOPES: Option<string>[] = [
  { value: 'all', label: 'All imported data' },
  { value: 'view', label: 'Only the current table view (filters & period)' },
];
const ROWS: Option<string>[] = ['10', '25', '50'].map((v) => ({ value: v, label: v }));
const RETENTION: Option<string>[] = [
  ...[1, 2, 3, 4, 5, 6, 7].map((d) => ({ value: String(d), label: d === 1 ? '1 day' : `${String(d)} days` })),
  { value: 'forever', label: 'Until I delete them' },
];

type Props = {
  status: AssistantStatus | undefined;
  /** Asks for consent before a cloud connection is used. */
  onCloud: (c: AssistantConnection) => void;
  onDone: () => void;
};

/** The kinds of proposals a user can let the panel apply by itself (ADR 0030). */
const AUTO_KINDS: [NonNullable<Schemas['AssistantSettings']['autoApply']>[number], string][] = [
  ['set_category', 'Set a category'],
  ['set_group', 'Put transactions in a group'],
  ['create_rule', 'Create a rule'],
  ['set_budget', 'Set or remove a budget'],
  ['add_expense', 'Add a cash expense'],
  ['add_to_shared_group', 'Share with a shared group'],
  ['split_expense', 'Change how a shared expense is split'],
];

export function ChatSettings({ status, onCloud, onDone }: Props) {
  const ai = useUi((s) => s.ai);
  const setAi = useUi((s) => s.setAi);
  const settings = useAssistantSettings();
  const save = useSaveAssistantSettings();
  const refresh = useRefreshStatus();
  const { conn, model } = pick(status, ai);
  const consented = !!conn && (settings.data?.cloudConsent ?? []).includes(conn.name);
  const info = conn?.models.find((m) => m.id === model);
  const retention = settings.data
    ? settings.data.retentionDays === null
      ? 'forever'
      : String(settings.data.retentionDays)
    : '2';

  return (
    <div className="conn">
      <h3>
        <Icon name="plug" />
        Assistant settings
      </h3>
      <label>
        Connection
        <Select
          value={conn?.name ?? ''}
          options={(status?.connections ?? []).map((c) => ({
            value: c.name,
            label: c.cloud ? `${c.label} (cloud)` : c.label,
          }))}
          onChange={(name) => {
            const c = status?.connections.find((x) => x.name === name);
            setAi({ connection: name, model: '' });
            if (c?.cloud && !(settings.data?.cloudConsent ?? []).includes(c.name)) onCloud(c);
          }}
        />
      </label>
      <label>
        Model
        <div className="row2 nowrap">
          {conn && conn.models.length > 0 ? (
            <Select
              value={model}
              options={conn.models.map((m) => ({ value: m.id, label: m.tools ? m.id : `${m.id} (plan mode)` }))}
              onChange={(id) => {
                setAi({ connection: conn.name, model: id });
              }}
            />
          ) : (
            <span className="hint">
              {conn?.reachable ? 'This server offers no chat model. Load one first.' : 'Not connected.'}
            </span>
          )}
          <Button
            size="sm"
            icon="plug"
            title="Check the model servers again"
            disabled={refresh.isPending}
            onClick={() => {
              refresh.mutate();
            }}
          >
            Test
          </Button>
        </div>
      </label>
      {conn?.error && (
        <div className="conn-err">
          {conn.error.message} {fixFor(conn.error.kind)}
        </div>
      )}
      <label>
        How the model gets data
        <Select
          value={ai.mode}
          options={MODES}
          onChange={(mode) => {
            setAi({ mode });
          }}
        />
      </label>
      <label>
        Thinking
        <Select
          value={ai.thinking}
          options={THINKING.map((t) => ({ value: t, label: THINK_LABEL[t] }))}
          onChange={(thinking) => {
            setAi({ thinking });
          }}
        />
      </label>
      {info?.reasoning === 'none' && ai.thinking !== 'auto' && (
        <p className="hint">This model has no thinking control; if it reasons anyway, its reasoning is shown.</p>
      )}
      <label>
        Data the assistant may query
        <Select
          value={ai.scope}
          options={SCOPES}
          onChange={(scope) => {
            setAi({ scope });
          }}
        />
      </label>
      <label>
        Max rows per lookup
        <Select
          value={String(Math.min(ai.maxRows, 50))}
          options={ROWS}
          onChange={(v) => {
            setAi({ maxRows: Number(v) });
          }}
        />
      </label>
      <label>
        Keep chats
        <Select
          value={retention}
          options={RETENTION}
          disabled={!settings.data || save.isPending}
          onChange={(v) => {
            if (!settings.data) return;
            save.mutate({ ...settings.data, retentionDays: v === 'forever' ? null : Number(v) });
          }}
        />
      </label>
      <fieldset className="auto-apply" disabled={!settings.data || save.isPending}>
        <legend>Apply by itself</legend>
        <p className="hint">
          Proposals of these kinds are applied as soon as the answer is complete, at most {AUTO_APPLY_MAX_ROWS}{' '}
          transactions at a time. Each can be undone on its card or in Settings → Activity.
        </p>
        {AUTO_KINDS.map(([kind, label]) => (
          <label key={kind} className="chk">
            <input
              type="checkbox"
              checked={settings.data?.autoApply?.includes(kind) ?? false}
              onChange={(e) => {
                if (!settings.data) return;
                const cur = settings.data.autoApply ?? [];
                save.mutate({
                  ...settings.data,
                  autoApply: e.target.checked ? [...cur, kind] : cur.filter((k) => k !== kind),
                });
              }}
            />
            {label}
          </label>
        ))}
      </fieldset>
      {retention === 'forever' ? (
        <div className="conn-err" role="note">
          Chats are kept until you delete them. They hold your questions and the transactions the assistant looked up.
        </div>
      ) : (
        <p className="hint">
          Chats are deleted {retention === '1' ? 'a day' : `${retention} days`} after their last message.
        </p>
      )}
      {conn?.cloud && (
        <p className="hint">
          <Icon name="lock" />
          {consented ? (
            <>
              You agreed to send your questions and the transactions the model looks up to <b>{conn.label}</b>.{' '}
              <button
                type="button"
                className="link"
                onClick={() => {
                  if (settings.data)
                    save.mutate({
                      ...settings.data,
                      cloudConsent: settings.data.cloudConsent.filter((n) => n !== conn.name),
                    });
                }}
              >
                Withdraw
              </button>
            </>
          ) : (
            <>
              <b>{conn.label}</b> is a cloud service; it needs your agreement before it gets any data.
            </>
          )}
        </p>
      )}
      {!conn?.cloud && (
        <p className="hint">
          <Icon name="lock" /> The model runs on a server your admin set up. It only receives what it looks up, never a
          full dump of your data.
        </p>
      )}
      <div className="row2">
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </div>
    </div>
  );
}
