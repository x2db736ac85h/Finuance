// The assistant's settings inside the panel (legacy connForm, PLAN task 13.7). The server address is
// instance config, so the user picks a connection and a model; the rest are the legacy run
// settings (saved in the preferences) and the chat retention (a server setting).

import { useState } from 'react';

import {
  useAssistantSettings,
  useRefreshStatus,
  useRequestDigest,
  useSaveAssistantSettings,
  type AssistantConnection,
  type AssistantSettings,
  type AssistantStatus,
} from '../api/hooks/assistant';
import { useMe, useMeta } from '../api/hooks/me';
import type { Schemas } from '../api/client';
import { errorMessage } from '../lib/errors';
import { fmtDateTime } from '../lib/dates';
import { toast } from '../ui/toastStore';
import { THINKING, type AiPrefs, type Thinking } from '../stores/prefs';
import { useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { Select, type Option } from '../ui/inputs';
import { fixFor } from './errors';
import { AUTO_APPLY_MAX_ROWS } from './Proposal';

/**
 * The connection and model the panel uses: the saved choice when offered, else the defaults. When
 * the administrator picks (status.choice admin), the default connection and its default model, the
 * only ones the server uses then.
 */
export function pick(status: AssistantStatus | undefined, ai: Pick<AiPrefs, 'connection' | 'model'>) {
  const conns = status?.connections ?? [];
  if (status?.choice === 'admin') {
    const conn = conns[0];
    return conn ? { conn, model: conn.defaultModel ?? '' } : { conn: undefined, model: '' };
  }
  const conn = conns.find((c) => c.name === ai.connection) ?? conns[0];
  if (!conn) return { conn: undefined, model: '' };
  const offered =
    conn.models.some((m) => m.id === ai.model) || (!conn.reachable && ai.model !== '' && conn.name === ai.connection);
  return { conn, model: offered ? ai.model : (conn.defaultModel ?? '') };
}

/** Whether questions with photos can be asked: the model reads images, or the connection's model for photos does. */
export function readsPhotos(conn: AssistantConnection | undefined, model: string): boolean {
  if (!conn) return false;
  const can = (id: string | undefined) => !!id && (conn.models.find((m) => m.id === id)?.vision ?? false);
  return can(model) || can(conn.photoModel);
}

/** What a check of the connection found, as a line under Test (also in Administration). */
export function testLine(conn: AssistantConnection): { ok: boolean; text: string } {
  if (!conn.reachable) {
    const why = conn.error ? `${conn.error.message} ${fixFor(conn.error.kind)}`.trim() : 'no answer';
    return { ok: false, text: `Can't reach ${conn.label}: ${why}` };
  }
  const parts = [
    `Connected to ${conn.label}`,
    `${String(conn.models.length)} ${conn.models.length === 1 ? 'model' : 'models'}`,
  ];
  if (conn.latencyMs !== undefined) parts.push(`${String(conn.latencyMs)} ms`);
  return { ok: true, text: parts.join(' · ') };
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
  ['create_goal', 'Set a goal'],
  ['update_goal', 'Change a goal'],
];

type Digest = Schemas['AssistantDigest'];
const SCHEDULES: Option<string>[] = [
  { value: 'off', label: 'Off' },
  { value: 'daily', label: 'Every day' },
  { value: 'weekly', label: 'Every week' },
];
const WEEKDAYS: Option<string>[] = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'].map(
  (d, i) => ({ value: String(i + 1), label: d }),
);
const HOURS: Option<string>[] = Array.from({ length: 24 }, (_, h) => ({
  value: String(h),
  label: `${String(h).padStart(2, '0')}:00`,
}));

/** The digest (ADR 0033): written in the background with the connection and model chosen here,
 * at an hour of the browser's time zone; its proposals wait in its chat. */
function DigestSettings({
  settings,
  connection,
  model,
  disabled,
  onSave,
}: {
  settings: AssistantSettings;
  connection: string;
  model: string;
  disabled: boolean;
  onSave: (s: AssistantSettings) => void;
}) {
  const me = useMe();
  const now = useRequestDigest();
  const d: Digest = settings.digest ?? {
    schedule: 'off',
    hour: 7,
    weekday: 1,
    timeZone: 'Europe/Warsaw',
    connection: '',
    model: '',
  };
  const save = (change: Partial<Digest>) => {
    onSave({
      ...settings,
      digest: { ...d, ...change, connection, model, timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone },
    });
  };
  const limit = me.data?.limits.assistantTokensPerDay ?? 0;
  return (
    <fieldset className="auto-apply digest" disabled={disabled}>
      <legend>Digest</legend>
      <p className="hint">
        A short summary written in the background: budgets and goals that need attention, payments due and changes worth
        making. Its proposals wait in its chat until you apply them.
      </p>
      <label>
        How often
        <Select
          value={d.schedule}
          options={SCHEDULES}
          onChange={(schedule) => {
            save({ schedule: schedule as Digest['schedule'] });
          }}
        />
      </label>
      {d.schedule === 'weekly' && (
        <label>
          On
          <Select
            value={String(d.weekday)}
            options={WEEKDAYS}
            onChange={(v) => {
              save({ weekday: Number(v) });
            }}
          />
        </label>
      )}
      {d.schedule !== 'off' && (
        <label>
          At
          <Select
            value={String(d.hour)}
            options={HOURS}
            onChange={(v) => {
              save({ hour: Number(v) });
            }}
          />
        </label>
      )}
      {settings.digestNextAt && (
        <p className="hint">
          Next one {fmtDateTime(settings.digestNextAt)}, with {d.model || 'the default model'}.
        </p>
      )}
      <div className="row2">
        <Button
          size="sm"
          icon="ai"
          disabled={now.isPending}
          onClick={() => {
            now.mutate(undefined, {
              onSuccess: () => {
                toast('A digest is being written. The bell tells you when it is ready.');
              },
              onError: (e) => {
                toast(errorMessage(e), { kind: 'error' });
              },
            });
          }}
        >
          Write one now
        </Button>
      </div>
      {limit > 0 && (
        <p className="hint">
          Used today: {(settings.tokensToday ?? 0).toLocaleString()} of {limit.toLocaleString()} tokens.
        </p>
      )}
    </fieldset>
  );
}

export function ChatSettings({ status, onCloud, onDone }: Props) {
  const ai = useUi((s) => s.ai);
  const setAi = useUi((s) => s.setAi);
  const settings = useAssistantSettings();
  const save = useSaveAssistantSettings();
  const refresh = useRefreshStatus();
  const meta = useMeta();
  const { conn, model } = pick(status, ai);
  const locked = status?.choice === 'admin';
  const [tested, setTested] = useState(false);
  const line = conn ? testLine(conn) : undefined;
  const consented = !!conn && (settings.data?.cloudConsent ?? []).includes(conn.name);
  const info = conn?.models.find((m) => m.id === model);
  const testButton = (
    <Button
      size="sm"
      icon="plug"
      title="Check the model servers again"
      disabled={refresh.isPending}
      onClick={() => {
        setTested(true);
        refresh.mutate();
      }}
    >
      Test
    </Button>
  );
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
      {locked ? (
        <div className="conn-locked">
          <span>
            <b>{conn?.label ?? 'No connection'}</b>
            {model ? ` · ${model}` : ''}
          </span>
          <span className="hint">The connection and model are set by your administrator.</span>
          <div>{testButton}</div>
        </div>
      ) : (
        <>
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
              {testButton}
            </div>
          </label>
        </>
      )}
      {/* the result of the last Test stays until the next one (role status: read out) */}
      <div className="conn-test" role="status">
        {tested &&
          (refresh.isPending ? (
            <span className="hint">Checking…</span>
          ) : refresh.isError ? (
            <span className="conn-bad">Couldn't check: {errorMessage(refresh.error)}</span>
          ) : conn ? (
            <span className={line?.ok ? 'conn-ok' : 'conn-bad'}>
              {line?.ok ? '✓ ' : '✕ '}
              {line?.text}
            </span>
          ) : null)}
      </div>
      {conn?.error && !tested && (
        <div className="conn-err">
          {conn.error.message} {fixFor(conn.error.kind)}
        </div>
      )}
      {info?.loaded === false && (
        <p className="hint">
          The model isn't loaded right now: the first answer takes a little longer while the server loads it.
        </p>
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
      {meta.data?.features.assistantDigests && settings.data && (
        <DigestSettings
          settings={settings.data}
          connection={conn?.name ?? ''}
          model={model}
          disabled={save.isPending}
          onSave={(s) => {
            save.mutate(s);
          }}
        />
      )}
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
