// Administration → Assistant (ADR 0035): the model servers users can pick, and per-model tuning.
// The connections file's are shown read only; those added here can be changed. An API key is
// entered once and never shown again; a new address needs it again.
//
// One thing is open at a time (Phase 26, U12): a connection's models under the list, or the dialog
// that adds or changes a connection. Adding starts with the provider (Anthropic, OpenAI, Google, a
// local server, other), which fills in the wire format and the address; after a save the new
// connection's models open, so adding and choosing a model is one flow.

import { useState } from 'react';

import {
  useConnections,
  useCreateConnection,
  useDeleteConnection,
  useModelAction,
  useModelServer,
  usePutConnectionControls,
  usePutTuning,
  useTestModelServer,
  useTuning,
  useUpdateConnection,
  type AdminConnection,
  type ModelServer,
  type ModelTuning,
} from '../api/hooks/admin';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtBytes, fmtInt } from '../lib/format';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput, type Option } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import {
  freeName,
  GOOGLE_LOCATION,
  GOOGLE_LOCATIONS,
  GOOGLE_PROJECT,
  googleBaseUrl,
  mayOffer,
  providerById,
  providerOf,
  PROVIDERS,
  providerTag,
  type ProviderId,
} from './providers';

const fail = (e: unknown) => {
  toast(errorMessage(e), { kind: 'error' });
};

function keyState(c: AdminConnection): string {
  if (c.auth === 'google') return `Google credentials, no key · ${c.project ?? ''} · ${c.location ?? ''}`;
  if (c.keyUnreadable) return 'API key unreadable (another instance key): enter it again';
  return c.hasKey ? 'API key stored' : 'no API key';
}

/** What is open under or over the list: one connection's models, or the dialog for one connection. */
type Open = { kind: 'models' | 'edit'; name: string } | { kind: 'add' } | null;

export function Connections() {
  const list = useConnections();
  const del = useDeleteConnection();
  const [open, setOpen] = useState<Open>(null);
  const named = open && open.kind !== 'add' ? list.data?.find((c) => c.name === open.name) : undefined;
  return (
    <div className="set-card">
      <div className="lbl">Connections</div>
      <p className="hint">
        The model servers people pick from. The configuration file's are shown here but changed only in the file. Local
        servers must be on a private address; a cloud service needs each person's agreement before it gets data.
      </p>
      {list.isError && <p className="set-err">{errorMessage(list.error)}</p>}
      <ul className="set-list" aria-label="Connections">
        {list.data?.map((c) => (
          <li key={c.name} className={open && open.kind !== 'add' && open.name === c.name ? 'on' : undefined}>
            <div className="set-li-main">
              <b>{c.label}</b>
              <span className="tag">{providerTag(c)}</span>
              {c.cloud && <span className="tag">cloud</span>}
              <span className="tag src">{c.source === 'file' ? 'configuration file' : 'added here'}</span>
              {!c.controls.enabled && <span className="tag off">off</span>}
              <span className="hint">
                {c.name} · {c.baseUrl} · {keyState(c)}
                {c.defaultModel ? ` · default ${c.defaultModel}` : ''}
              </span>
            </div>
            <Button
              size="sm"
              aria-expanded={open?.kind === 'models' && open.name === c.name}
              onClick={() => {
                setOpen(open?.kind === 'models' && open.name === c.name ? null : { kind: 'models', name: c.name });
              }}
            >
              Models
            </Button>
            {c.source === 'panel' && (
              <>
                <Button
                  size="sm"
                  onClick={() => {
                    setOpen({ kind: 'edit', name: c.name });
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
                        if (open && open.kind !== 'add' && open.name === c.name) setOpen(null);
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
      {open?.kind === 'models' && named && (
        <ConnectionControls
          // by name only: a save refetches the list with a new version, and remounting would drop
          // the save's confirmation (the version is read from the latest list when saving)
          key={named.name}
          connection={named}
          onDone={() => {
            setOpen(null);
          }}
        />
      )}
      <div className="set-actions">
        <Button
          variant="primary"
          onClick={() => {
            setOpen({ kind: 'add' });
          }}
        >
          Add a connection
        </Button>
      </div>
      {(open?.kind === 'add' || (open?.kind === 'edit' && named)) && (
        <ConnectionDialog
          connection={open.kind === 'edit' ? named : undefined}
          taken={list.data?.map((c) => c.name) ?? []}
          onClose={() => {
            setOpen(null);
          }}
          onSaved={(name, added) => {
            // a new connection goes on to its models: is the server there, which model answers
            setOpen(added ? { kind: 'models', name } : null);
          }}
        />
      )}
    </div>
  );
}

const UNLOAD: Option<string>[] = [
  { value: '', label: "The server's own setting" },
  ...[10, 15, 30, 45].map((m) => ({ value: String(m), label: `${String(m)} minutes idle` })),
  { value: '60', label: '1 hour idle' },
  { value: '120', label: '2 hours idle' },
  { value: '240', label: '4 hours idle' },
];

const SERVER: Record<ModelServer['server'], string> = {
  ollama: 'Ollama',
  lmstudio: 'LM Studio',
  other: 'OpenAI-compatible server',
};

/** What the last check of a connection's server found. */
/** `service` names a provider's own endpoint (Anthropic, OpenAI, Vertex AI), which is not a server somebody runs. */
export function serverLine(s: ModelServer, service?: string): { ok: boolean; text: string } {
  if (!s.reachable)
    return { ok: false, text: `Can't reach ${service ?? 'the server'}: ${s.error?.message ?? 'no answer'}` };
  const offered = s.models.filter((m) => m.offered).length;
  const parts = [
    `Connected to ${service ?? SERVER[s.server]}`,
    `${String(offered)} ${offered === 1 ? 'model' : 'models'}`,
  ];
  if (s.latencyMs !== undefined) parts.push(`${String(s.latencyMs)} ms`);
  return { ok: true, text: parts.join(' · ') };
}

/**
 * A connection's models and use (Phase 20 follow-ups): on or off, the model questions use and the
 * one for photos, how long the server keeps a model after the last question, and what it holds now
 * (Ollama can load and unload from here).
 */
function ConnectionControls({ connection, onDone }: { connection: AdminConnection; onDone: () => void }) {
  const ctl = connection.controls;
  const put = usePutConnectionControls();
  const server = useModelServer(connection.name);
  const test = useTestModelServer(connection.name);
  const act = useModelAction(connection.name);
  const [enabled, setEnabled] = useState(ctl.enabled);
  const [model, setModel] = useState(ctl.defaultModel);
  const [photo, setPhoto] = useState(ctl.photoModel);
  const [unload, setUnload] = useState(ctl.unloadAfterMinutes == null ? '' : String(ctl.unloadAfterMinutes));
  const [budget, setBudget] = useState(ctl.dailyTokens == null ? '' : String(ctl.dailyTokens));
  const budgetNumber = Number(budget.replace(/[\s,._]/g, ''));
  const budgetBad = budget.trim() !== '' && !(Number.isInteger(budgetNumber) && budgetNumber >= 1000);
  const s = server.data;
  const offered = s?.models.filter((m) => m.offered) ?? [];
  // a saved choice the server doesn't list now (unreachable, model removed) stays selectable
  const options = (list: string[], current: string, none: string): Option<string>[] => [
    { value: '', label: none },
    ...[...new Set([...list, ...(current ? [current] : [])])].map((id) => ({ value: id, label: id })),
  ];
  const timed = s?.server === 'ollama' || s?.server === 'lmstudio';
  const provider = providerOf(connection);
  const service =
    provider === 'google'
      ? 'Vertex AI'
      : provider === 'anthropic' || provider === 'openai'
        ? providerById(provider).label
        : undefined;
  const line = s ? serverLine(s, service) : undefined;
  const save = () => {
    put.mutate(
      {
        name: connection.name,
        enabled,
        unloadAfterMinutes: unload === '' ? null : Number(unload),
        defaultModel: model,
        photoModel: photo,
        dailyTokens: budget.trim() === '' ? null : budgetNumber,
        version: ctl.version,
      },
      {
        onSuccess: () => {
          toast(`Saved ${connection.label}${enabled ? '' : ': switched off, its models unloaded'}`);
        },
        onError: fail,
      },
    );
  };
  return (
    <form
      className="adm-form"
      aria-label={`Models and use of ${connection.label}`}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="lbl">{connection.label}: models and use</div>
      <Checkbox checked={enabled} onChange={setEnabled} label="On: people can use this connection" />
      <div className="conn-test" role="status">
        {server.isLoading || test.isPending ? (
          <span className="hint">Checking…</span>
        ) : server.isError ? (
          <span className="conn-bad">Couldn't check: {errorMessage(server.error)}</span>
        ) : line ? (
          <span className={line.ok ? 'conn-ok' : 'conn-bad'}>
            {line.ok ? '✓ ' : '✕ '}
            {line.text}
          </span>
        ) : null}
      </div>
      <div className="form-row">
        <label className="st-col">
          Default model
          <Select
            value={model}
            options={options(
              offered.map((m) => m.id),
              model,
              connection.defaultModel ? `The connection's own (${connection.defaultModel})` : "The server's first",
            )}
            onChange={setModel}
          />
        </label>
        <label className="st-col">
          Model for photos
          <Select
            value={photo}
            options={options(
              offered.filter((m) => m.vision).map((m) => m.id),
              photo,
              'None',
            )}
            onChange={setPhoto}
          />
        </label>
        {timed && (
          <label className="st-col">
            Unload a model after
            <Select value={unload} options={UNLOAD} onChange={setUnload} />
          </label>
        )}
      </div>
      <label className="st-col">
        Daily token budget <span className="hint">(for everyone together; empty: none)</span>
        <TextInput
          autoComplete="off"
          inputMode="numeric"
          value={budget}
          placeholder="e.g. 2,000,000"
          aria-invalid={budgetBad || undefined}
          onChange={(e) => {
            setBudget(e.target.value);
          }}
        />
      </label>
      <p className="hint">
        Used today (UTC): {fmtInt(ctl.tokensToday)} tokens
        {ctl.dailyTokens ? ` of ${fmtInt(ctl.dailyTokens)}` : ''}. Once a budget is used up, questions on this
        connection are refused until the next day; a cloud service bills whoever runs the instance.
        {budgetBad && ' A budget is a whole number of at least 1,000 tokens.'}
      </p>
      <p className="hint">
        Questions with photos go to the model for photos when the model in use can't read images.
        {timed &&
          ' The server unloads a model once nobody asked it anything for that long; the first answer after takes longer.'}
      </p>
      {s && s.models.some((m) => m.loaded !== undefined) && (
        <ul className="set-list" aria-label="Models on the server">
          {s.models.map((m) => (
            <li key={m.id}>
              <div className="set-li-main">
                <b>{m.id}</b>
                <span className={m.loaded ? 'tag on' : 'tag'}>{m.loaded ? 'loaded' : 'not loaded'}</span>
                {!m.offered && <span className="tag">not offered</span>}
                {m.loaded && (
                  <span className="hint">
                    {m.vramBytes ? `${fmtBytes(m.vramBytes)} on the GPU` : m.sizeBytes ? fmtBytes(m.sizeBytes) : ''}
                    {m.unloadsAt ? ` · unloads ${fmtDateTime(m.unloadsAt)} if unused` : ''}
                  </span>
                )}
              </div>
              {s.canManage && (m.loaded || m.offered) && (
                <Button
                  size="sm"
                  disabled={act.isPending}
                  onClick={() => {
                    act.mutate(
                      { action: m.loaded ? 'unload' : 'load', model: m.id },
                      {
                        onSuccess: () => {
                          toast(m.loaded ? `Unloaded ${m.id}` : `Loaded ${m.id}`);
                        },
                        onError: fail,
                      },
                    );
                  }}
                >
                  {m.loaded ? 'Unload' : 'Load'}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="set-actions">
        <Button
          icon="plug"
          disabled={test.isPending}
          onClick={() => {
            test.mutate(undefined, { onError: fail });
          }}
        >
          Test
        </Button>
        <Button onClick={onDone}>Close</Button>
        <Button type="submit" variant="primary" disabled={put.isPending || budgetBad}>
          Save
        </Button>
      </div>
    </form>
  );
}

const APIS: Option<'openai' | 'anthropic'>[] = [
  { value: 'openai', label: 'OpenAI-compatible (LM Studio, Ollama, vLLM, OpenAI…)' },
  { value: 'anthropic', label: 'Anthropic Messages' },
];

/** What a Google connection talks to: each family has its own wire format on Vertex AI. */
const GOOGLE_FAMILIES: Option<'openai' | 'anthropic'>[] = [
  { value: 'openai', label: 'Gemini and open models' },
  { value: 'anthropic', label: 'Claude (Anthropic on Vertex AI)' },
];

/** Step one of adding a connection: whose models. Each card says what it is in a line. */
function ProviderPicker({ onPick }: { onPick: (id: ProviderId) => void }) {
  return (
    <div className="adm-form" role="radiogroup" aria-label="Provider">
      <p className="hint">Whose models is this connection for? The choice fills in the address and the format.</p>
      <div className="prov-cards">
        {PROVIDERS.map((p) => (
          <button
            key={p.id}
            type="button"
            role="radio"
            aria-checked={false}
            aria-disabled={p.later ? true : undefined}
            className={p.later ? 'prov-card later' : 'prov-card'}
            title={p.later}
            onClick={() => {
              if (!p.later) onPick(p.id);
            }}
          >
            <b>{p.label}</b>
            <span className="hint">{p.about}</span>
            {p.later && <span className="tag">soon</span>}
          </button>
        ))}
      </div>
      {PROVIDERS.filter((p) => p.later).map((p) => (
        <p key={p.id} className="hint">
          {p.label}: {p.later}
        </p>
      ))}
    </div>
  );
}

type DialogProps = {
  connection: AdminConnection | undefined;
  /** The names in use, so a provider's suggested name is free. */
  taken: string[];
  onClose: () => void;
  onSaved: (name: string, added: boolean) => void;
};

/** The dialog that adds a connection (provider first) or changes one. */
function ConnectionDialog({ connection, taken, onClose, onSaved }: DialogProps) {
  const [provider, setProvider] = useState<ProviderId | null>(connection ? providerOf(connection) : null);
  const title = connection ? `Change ${connection.label}` : 'Add a connection';
  return (
    <Modal
      open
      onClose={onClose}
      title={title}
      sub={provider && !connection ? providerById(provider).label : undefined}
      size="sm"
      dismissOnBackdrop={false}
    >
      {provider === null ? (
        <ProviderPicker onPick={setProvider} />
      ) : (
        <ConnectionForm
          key={provider}
          provider={provider}
          connection={connection}
          taken={taken}
          onCancel={onClose}
          onBack={
            connection
              ? undefined
              : () => {
                  setProvider(null);
                }
          }
          onSaved={onSaved}
        />
      )}
    </Modal>
  );
}

type FormProps = {
  provider: ProviderId;
  connection: AdminConnection | undefined;
  taken: string[];
  onCancel: () => void;
  /** Back to the provider choice (adding only). */
  onBack: (() => void) | undefined;
  onSaved: (name: string, added: boolean) => void;
};

function ConnectionForm({ provider, connection, taken, onCancel, onBack, onSaved }: FormProps) {
  const create = useCreateConnection();
  const update = useUpdateConnection();
  const prov = providerById(provider);
  const [c, setC] = useState({
    name: connection?.name ?? freeName(prov.preset.name, taken),
    label: connection?.label ?? prov.preset.label,
    api: connection?.api ?? prov.preset.api,
    baseUrl: connection?.baseUrl ?? prov.preset.baseUrl,
    cloud: connection?.cloud ?? prov.preset.cloud,
    defaultModel: connection?.defaultModel ?? '',
    models: (connection?.models ?? []).join(', '),
    contextTokens: connection?.contextTokens ? String(connection.contextTokens) : '',
    project: connection?.project ?? '',
    location: connection?.location ?? '',
  });
  const google = provider === 'google';
  const [keyAction, setKeyAction] = useState<'keep' | 'replace' | 'remove'>(connection ? 'keep' : 'replace');
  const [apiKey, setApiKey] = useState('');
  const busy = create.isPending || update.isPending;
  const patterns = c.models
    .split(',')
    .map((m) => m.trim())
    .filter(Boolean);
  // a default model the patterns leave out would be a connection whose own default nobody may use
  const defaultLeftOut = c.defaultModel.trim() !== '' && !mayOffer(patterns, c.defaultModel.trim());
  // a provider's own service needs its key (Google has none: its credentials are the instance's
  // own); a local server usually has none
  const needsKey = !connection && prov.fixedAddress && !google && !apiKey.trim();
  // a Google connection is a project, a chosen location and the models to offer: Vertex AI's
  // endpoint has no model list of its own
  const projectOk = GOOGLE_PROJECT.test(c.project.trim());
  const locationOk = GOOGLE_LOCATION.test(c.location.trim());
  const modelIds = patterns.filter((m) => !/[*?[]/.test(m));
  const googleMissing = google && (!projectOk || !locationOk || modelIds.length === 0);
  const save = () => {
    const body = {
      label: c.label.trim(),
      api: c.api,
      // a Google connection's address follows from its project and location (the server derives it)
      baseUrl: google ? '' : c.baseUrl.trim(),
      cloud: c.cloud,
      defaultModel: c.defaultModel.trim(),
      models: patterns,
      contextTokens: Number(c.contextTokens) || 0,
      ...(google
        ? { auth: 'google' as const, project: c.project.trim(), location: c.location.trim() }
        : { auth: 'key' as const }),
    };
    if (connection) {
      update.mutate(
        {
          name: connection.name,
          version: connection.version,
          ...body,
          keyAction: google ? 'remove' : keyAction,
          ...(!google && keyAction === 'replace' ? { apiKey } : {}),
        },
        {
          onSuccess: () => {
            toast(`Saved the connection ${body.label}`);
            onSaved(connection.name, false);
          },
          onError: fail,
        },
      );
    } else {
      const name = c.name.trim();
      create.mutate(
        { name, ...body, ...(apiKey && !google ? { apiKey } : {}) },
        {
          onSuccess: () => {
            toast(`Added the connection ${body.label || name}. Now choose its model.`);
            onSaved(name, true);
          },
          onError: fail,
        },
      );
    }
  };
  const nameField = (
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
  );
  const labelField = (
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
  );
  const addressField = (
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
  );
  const keyField = (
    <>
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
    </>
  );
  const modelFields = (
    <>
      <div className="form-row">
        <label className="st-col">
          Default model
          <TextInput
            autoComplete="off"
            value={c.defaultModel}
            maxLength={200}
            aria-invalid={defaultLeftOut || undefined}
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
      {defaultLeftOut && (
        <p className="set-err" role="alert">
          The default model “{c.defaultModel.trim()}” fits none of the models offered. Add a pattern for it, or leave
          one of the two empty.
        </p>
      )}
    </>
  );
  return (
    <form
      className="adm-form"
      aria-label="Connection"
      autoComplete="off"
      onSubmit={(e) => {
        e.preventDefault();
        if (!defaultLeftOut && !needsKey && !googleMissing) save();
      }}
    >
      {google ? (
        // Vertex AI: which project pays, where the data is processed, which models; no key
        <>
          <div className="form-row">
            {labelField}
            <label className="st-col">
              Models
              <Select
                value={c.api}
                options={GOOGLE_FAMILIES}
                onChange={(api) => {
                  setC({ ...c, api });
                }}
              />
            </label>
          </div>
          <div className="form-row">
            <label className="st-col">
              Google Cloud project
              <TextInput
                autoComplete="off"
                mono
                value={c.project}
                maxLength={30}
                placeholder="my-project-123"
                aria-invalid={(c.project.trim() !== '' && !projectOk) || undefined}
                onChange={(e) => {
                  setC({ ...c, project: e.target.value });
                }}
              />
            </label>
            <label className="st-col">
              Location
              <TextInput
                autoComplete="off"
                mono
                list="google-locations"
                value={c.location}
                maxLength={40}
                placeholder="choose one: europe-west4, eu, …"
                aria-invalid={(c.location.trim() !== '' && !locationOk) || undefined}
                onChange={(e) => {
                  setC({ ...c, location: e.target.value });
                }}
              />
            </label>
          </div>

          <datalist id="google-locations">
            {GOOGLE_LOCATIONS.map((l) => (
              <option key={l.value} value={l.value}>
                {l.label}
              </option>
            ))}
          </datalist>
          <p className="hint">
            The location decides where questions are processed; there is no default. Inside the EU: <code>eu</code> or a
            region such as <code>europe-west4</code>. <code>global</code> gives no say over where.
          </p>
          {c.location.trim() === 'global' && (
            <p className="set-err" role="status">
              With “global” Google chooses where each request is processed. Pick a region or <code>eu</code> to keep it
              in one place.
            </p>
          )}
          <label className="st-col">
            Models offered{' '}
            <span className="hint">(their ids, comma-separated; Vertex AI has no list to read them from)</span>
            <TextInput
              autoComplete="off"
              value={c.models}
              placeholder={
                c.api === 'anthropic' ? 'claude-sonnet-5-5' : 'google/gemini-3.5-flash-lite, google/gemini-3.8-flash'
              }
              onChange={(e) => {
                setC({ ...c, models: e.target.value });
              }}
            />
          </label>
          <p className="hint">
            No key is stored: this instance asks Google for a short-lived token with its own credentials (workload
            identity on Google Cloud, or the credentials file its environment names), which need the role{' '}
            <code>roles/aiplatform.user</code> on the project. Each person agrees before their data is sent.
          </p>
          {projectOk && locationOk && (
            <p className="hint conn-addr">
              Address: <code>{googleBaseUrl(c.project.trim(), c.location.trim(), c.api)}</code>
            </p>
          )}
          <details className="conn-more">
            <summary>More options</summary>
            {!connection && nameField}
            <div className="form-row">
              <label className="st-col">
                Default model
                <TextInput
                  autoComplete="off"
                  value={c.defaultModel}
                  maxLength={200}
                  aria-invalid={defaultLeftOut || undefined}
                  onChange={(e) => {
                    setC({ ...c, defaultModel: e.target.value });
                  }}
                />
              </label>
              <label className="st-col">
                Context tokens <span className="hint">(empty: 8,192)</span>
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
            {defaultLeftOut && (
              <p className="set-err" role="alert">
                The default model “{c.defaultModel.trim()}” fits none of the models offered.
              </p>
            )}
          </details>
        </>
      ) : provider === 'other' ? (
        // everything, as typed: nothing about the server is known
        <>
          <div className="form-row">
            {nameField}
            {labelField}
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
          {addressField}
          <Checkbox
            label="A cloud service (public address; people agree before their data is sent; https only)"
            checked={c.cloud}
            onChange={(cloud) => {
              setC({ ...c, cloud });
            }}
          />
          {modelFields}
          {keyField}
        </>
      ) : provider === 'local' ? (
        <>
          <div className="form-row">
            {nameField}
            {labelField}
          </div>
          {addressField}
          <p className="hint">
            A private address on your network. Ollama answers at <code>http://&lt;host&gt;:11434/v1</code>, LM Studio at{' '}
            <code>http://&lt;host&gt;:1234/v1</code>.
          </p>
          <details className="conn-more">
            <summary>More options</summary>
            {modelFields}
            {keyField}
          </details>
        </>
      ) : (
        // the provider's own service: its label and key; the rest is known
        <>
          {labelField}
          {keyField}
          <p className="hint">
            {prov.label} is a cloud service: each person agrees before their data is sent there, and long numbers are
            shortened first.
          </p>
          <details className="conn-more">
            <summary>More options</summary>
            {!connection && nameField}
            {addressField}
            {modelFields}
          </details>
        </>
      )}
      <div className="set-actions">
        {onBack && (
          <Button variant="ghost" icon="left" onClick={onBack}>
            Provider
          </Button>
        )}
        <Button onClick={onCancel}>Cancel</Button>
        <Button
          type="submit"
          variant="primary"
          disabled={
            busy ||
            (!google && !c.baseUrl.trim()) ||
            (!connection && !c.name.trim()) ||
            defaultLeftOut ||
            needsKey ||
            googleMissing
          }
        >
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
