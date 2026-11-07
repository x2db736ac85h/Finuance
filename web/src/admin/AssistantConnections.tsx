// Administration → Assistant (ADR 0035): the model servers users can pick, and per-model tuning.
// The connections file's are shown read only; those added here can be changed. An API key is
// entered once and never shown again; a new address needs it again.

import { useState } from 'react';

import {
  useConnections,
  useCreateConnection,
  useDeleteConnection,
  usePutTuning,
  useTuning,
  useUpdateConnection,
  type AdminConnection,
  type ModelTuning,
} from '../api/hooks/admin';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput, type Option } from '../ui/inputs';
import { toast } from '../ui/toastStore';

const fail = (e: unknown) => {
  toast(errorMessage(e), { kind: 'error' });
};

function keyState(c: AdminConnection): string {
  if (c.keyUnreadable) return 'API key unreadable (another instance key): enter it again';
  return c.hasKey ? 'API key stored' : 'no API key';
}

export function Connections() {
  const list = useConnections();
  const del = useDeleteConnection();
  const [editing, setEditing] = useState<string | null>(null);
  const current = list.data?.find((c) => c.name === editing);
  return (
    <div className="set-card">
      <div className="lbl">Connections</div>
      <p className="hint">
        The model servers people pick from. The configuration file's are shown here but changed only in the file. Local
        servers must be on a private address; a cloud service needs each person's agreement before it gets data.
      </p>
      {list.isError && <p className="set-err">{errorMessage(list.error)}</p>}
      <ul className="set-list">
        {list.data?.map((c) => (
          <li key={c.name} className={editing === c.name ? 'on' : undefined}>
            <div className="set-li-main">
              <b>{c.label}</b>
              <span className="tag">{c.api}</span>
              {c.cloud && <span className="tag">cloud</span>}
              <span className="tag src">{c.source === 'file' ? 'configuration file' : 'added here'}</span>
              <span className="hint">
                {c.name} · {c.baseUrl} · {keyState(c)}
                {c.defaultModel ? ` · default ${c.defaultModel}` : ''}
              </span>
            </div>
            {c.source === 'panel' && (
              <>
                <Button
                  size="sm"
                  onClick={() => {
                    setEditing(c.name);
                  }}
                >
                  Edit
                </Button>
                <Button
                  size="sm"
                  variant="danger"
                  disabled={del.isPending}
                  onClick={() => {
                    del.mutate(c.name, {
                      onSuccess: () => {
                        toast(`Deleted the connection ${c.label}`);
                        if (editing === c.name) setEditing(null);
                      },
                      onError: fail,
                    });
                  }}
                >
                  Delete
                </Button>
              </>
            )}
          </li>
        ))}
      </ul>
      {editing === null ? (
        <div className="set-actions">
          <Button
            variant="primary"
            onClick={() => {
              setEditing('');
            }}
          >
            Add a connection
          </Button>
        </div>
      ) : (
        <ConnectionForm
          key={editing}
          connection={current}
          onDone={() => {
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

const APIS: Option<'openai' | 'anthropic'>[] = [
  { value: 'openai', label: 'OpenAI-compatible (LM Studio, Ollama, vLLM, OpenAI…)' },
  { value: 'anthropic', label: 'Anthropic Messages' },
];

function ConnectionForm({ connection, onDone }: { connection: AdminConnection | undefined; onDone: () => void }) {
  const create = useCreateConnection();
  const update = useUpdateConnection();
  const [c, setC] = useState({
    name: connection?.name ?? '',
    label: connection?.label ?? '',
    api: connection?.api ?? ('openai' as const),
    baseUrl: connection?.baseUrl ?? '',
    cloud: connection?.cloud ?? false,
    defaultModel: connection?.defaultModel ?? '',
    models: (connection?.models ?? []).join(', '),
    contextTokens: connection?.contextTokens ? String(connection.contextTokens) : '',
  });
  const [keyAction, setKeyAction] = useState<'keep' | 'replace' | 'remove'>(connection ? 'keep' : 'replace');
  const [apiKey, setApiKey] = useState('');
  const busy = create.isPending || update.isPending;
  const save = () => {
    const body = {
      label: c.label.trim(),
      api: c.api,
      baseUrl: c.baseUrl.trim(),
      cloud: c.cloud,
      defaultModel: c.defaultModel.trim(),
      models: c.models
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean),
      contextTokens: Number(c.contextTokens) || 0,
    };
    if (connection) {
      update.mutate(
        {
          name: connection.name,
          version: connection.version,
          ...body,
          keyAction,
          ...(keyAction === 'replace' ? { apiKey } : {}),
        },
        {
          onSuccess: () => {
            toast(`Saved the connection ${body.label}`);
            onDone();
          },
          onError: fail,
        },
      );
    } else {
      create.mutate(
        { name: c.name.trim(), ...body, ...(apiKey ? { apiKey } : {}) },
        {
          onSuccess: () => {
            toast(`Added the connection ${body.label || c.name}`);
            onDone();
          },
          onError: fail,
        },
      );
    }
  };
  return (
    <form
      className="adm-form"
      aria-label="Connection"
      autoComplete="off"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="form-row">
        <label className="st-col">
          Name
          <TextInput
            autoComplete="off"
            mono
            value={c.name}
            disabled={!!connection}
            maxLength={40}
            placeholder="gpu-box"
            onChange={(e) => {
              setC({ ...c, name: e.target.value });
            }}
          />
        </label>
        <label className="st-col">
          Label
          <TextInput
            autoComplete="off"
            value={c.label}
            maxLength={80}
            onChange={(e) => {
              setC({ ...c, label: e.target.value });
            }}
          />
        </label>
      </div>
      <label className="st-col">
        API
        <Select
          value={c.api}
          options={APIS}
          onChange={(api) => {
            setC({ ...c, api });
          }}
        />
      </label>
      <label className="st-col">
        Address
        <TextInput
          autoComplete="off"
          mono
          value={c.baseUrl}
          maxLength={500}
          placeholder="http://192.168.1.20:11434/v1"
          onChange={(e) => {
            setC({ ...c, baseUrl: e.target.value });
          }}
        />
      </label>
      <Checkbox
        label="A cloud service (public address; people agree before their data is sent; https only)"
        checked={c.cloud}
        onChange={(cloud) => {
          setC({ ...c, cloud });
        }}
      />
      <div className="form-row">
        <label className="st-col">
          Default model
          <TextInput
            autoComplete="off"
            value={c.defaultModel}
            maxLength={200}
            onChange={(e) => {
              setC({ ...c, defaultModel: e.target.value });
            }}
          />
        </label>
        <label className="st-col">
          Context tokens <span className="hint">(empty: the server's or 8,192)</span>
          <TextInput
            autoComplete="off"
            inputMode="numeric"
            value={c.contextTokens}
            onChange={(e) => {
              setC({ ...c, contextTokens: e.target.value });
            }}
          />
        </label>
      </div>
      <label className="st-col">
        Models offered <span className="hint">(comma-separated patterns such as qwen3*; empty: every chat model)</span>
        <TextInput
          autoComplete="off"
          value={c.models}
          onChange={(e) => {
            setC({ ...c, models: e.target.value });
          }}
        />
      </label>
      {connection && (
        <label className="st-col">
          API key <span className="hint">({keyState(connection)})</span>
          <Select
            value={keyAction}
            options={[
              { value: 'keep', label: 'Keep it (a new address drops it)' },
              { value: 'replace', label: 'Enter a new one' },
              { value: 'remove', label: 'Remove it' },
            ]}
            onChange={setKeyAction}
          />
        </label>
      )}
      {keyAction === 'replace' && (
        <label className="st-col">
          {connection ? 'New API key' : 'API key'} <span className="hint">(stored sealed, never shown again)</span>
          {/* new-password and the password managers' own flags: "off" is ignored on password fields,
              and the browser then fills a saved password here and the account e-mail into the field before */}
          <TextInput
            type="password"
            autoComplete="new-password"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore
            value={apiKey}
            maxLength={1000}
            onChange={(e) => {
              setApiKey(e.target.value);
            }}
          />
        </label>
      )}
      <div className="set-actions">
        <Button onClick={onDone}>Cancel</Button>
        <Button type="submit" variant="primary" disabled={busy || !c.baseUrl.trim() || (!connection && !c.name.trim())}>
          Save
        </Button>
      </div>
    </form>
  );
}

const THINK: Option<string>[] = [
  { value: '', label: "The question's" },
  { value: 'off', label: 'Off' },
  { value: 'auto', label: 'Model default' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
];
const TOOLSETS: Option<string>[] = [
  { value: '', label: "The question's" },
  { value: 'full', label: 'Full' },
  { value: 'compact', label: 'Compact (three tools)' },
];

function describe(t: ModelTuning): string {
  const parts: string[] = [];
  if (t.temperature !== undefined) parts.push(`temperature ${String(t.temperature)}`);
  if (t.contextTokens !== undefined) parts.push(`${t.contextTokens.toLocaleString()} tokens of context`);
  if (t.think) parts.push(`thinking ${t.think}`);
  if (t.toolset) parts.push(`${t.toolset} tools`);
  if (t.extraPrompt) parts.push('a note');
  return parts.join(' · ');
}

export function Tuning() {
  const list = useTuning();
  const conns = useConnections();
  const [editing, setEditing] = useState<ModelTuning | null>(null);
  return (
    <div className="set-card">
      <div className="lbl">Tuning per model</div>
      <p className="hint">
        Defaults for one model when a question doesn't set them, the context size to plan with, and a short note added
        after the instructions. No model is trained: these are settings.
      </p>
      {list.isError && <p className="set-err">{errorMessage(list.error)}</p>}
      <ul className="set-list">
        {list.data?.map((t) => (
          <li key={`${t.connection}/${t.model}`}>
            <div className="set-li-main">
              <b>{t.model}</b>
              <span className="tag">{t.connection}</span>
              <span className="hint">{describe(t)}</span>
            </div>
            <Button
              size="sm"
              onClick={() => {
                setEditing(t);
              }}
            >
              Edit
            </Button>
          </li>
        ))}
      </ul>
      {editing ? (
        <TuningForm
          key={`${editing.connection}/${editing.model}`}
          tuning={editing}
          connections={(conns.data ?? []).map((c) => c.name)}
          onDone={() => {
            setEditing(null);
          }}
        />
      ) : (
        <div className="set-actions">
          <Button
            variant="primary"
            onClick={() => {
              setEditing({ connection: conns.data?.[0]?.name ?? '', model: '', extraPrompt: '' });
            }}
          >
            Tune a model
          </Button>
        </div>
      )}
    </div>
  );
}

function TuningForm({
  tuning,
  connections,
  onDone,
}: {
  tuning: ModelTuning;
  connections: string[];
  onDone: () => void;
}) {
  const put = usePutTuning();
  const [t, setT] = useState({
    connection: tuning.connection,
    model: tuning.model,
    temperature: tuning.temperature === undefined ? '' : String(tuning.temperature),
    contextTokens: tuning.contextTokens === undefined ? '' : String(tuning.contextTokens),
    think: tuning.think ?? '',
    toolset: tuning.toolset ?? '',
    extraPrompt: tuning.extraPrompt,
  });
  const send = (body: ModelTuning, done: string) => {
    put.mutate(body, {
      onSuccess: () => {
        toast(done);
        onDone();
      },
      onError: fail,
    });
  };
  const save = () => {
    const body: ModelTuning = { connection: t.connection, model: t.model.trim(), extraPrompt: t.extraPrompt };
    if (t.temperature.trim()) body.temperature = Number(t.temperature.replace(',', '.'));
    if (t.contextTokens.trim()) body.contextTokens = Number(t.contextTokens);
    if (t.think) body.think = t.think as NonNullable<ModelTuning['think']>;
    if (t.toolset) body.toolset = t.toolset as NonNullable<ModelTuning['toolset']>;
    send(body, `Saved the tuning of ${body.model}`);
  };
  return (
    <form
      className="adm-form"
      aria-label="Tuning"
      autoComplete="off"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="form-row">
        <label className="st-col">
          Connection
          <Select
            value={t.connection}
            options={connections.map((c) => ({ value: c, label: c }))}
            onChange={(connection) => {
              setT({ ...t, connection });
            }}
          />
        </label>
        <label className="st-col">
          Model
          <TextInput
            autoComplete="off"
            value={t.model}
            maxLength={200}
            placeholder="qwen3:4b-thinking-2507"
            onChange={(e) => {
              setT({ ...t, model: e.target.value });
            }}
          />
        </label>
      </div>
      <div className="form-row">
        <label className="st-col">
          Temperature <span className="hint">(0–2; empty: 0.2)</span>
          <TextInput
            autoComplete="off"
            inputMode="decimal"
            value={t.temperature}
            onChange={(e) => {
              setT({ ...t, temperature: e.target.value });
            }}
          />
        </label>
        <label className="st-col">
          Context tokens
          <TextInput
            autoComplete="off"
            inputMode="numeric"
            value={t.contextTokens}
            onChange={(e) => {
              setT({ ...t, contextTokens: e.target.value });
            }}
          />
        </label>
      </div>
      <div className="form-row">
        <label className="st-col">
          Thinking
          <Select
            value={t.think}
            options={THINK}
            onChange={(think) => {
              setT({ ...t, think });
            }}
          />
        </label>
        <label className="st-col">
          Tools
          <Select
            value={t.toolset}
            options={TOOLSETS}
            onChange={(toolset) => {
              setT({ ...t, toolset });
            }}
          />
        </label>
      </div>
      <label className="st-col">
        Note for this model <span className="hint">(after the instructions, at most 1,000 characters)</span>
        <textarea
          className="adm-text"
          rows={3}
          maxLength={1000}
          value={t.extraPrompt}
          onChange={(e) => {
            setT({ ...t, extraPrompt: e.target.value });
          }}
        />
      </label>
      <div className="set-actions">
        {tuning.model && (
          <Button
            variant="danger"
            disabled={put.isPending}
            onClick={() => {
              send(
                { connection: tuning.connection, model: tuning.model, extraPrompt: '' },
                `Removed the tuning of ${tuning.model}`,
              );
            }}
          >
            Remove
          </Button>
        )}
        <Button onClick={onDone}>Cancel</Button>
        <Button type="submit" variant="primary" disabled={put.isPending || !t.model.trim() || !t.connection}>
          Save
        </Button>
      </div>
    </form>
  );
}
