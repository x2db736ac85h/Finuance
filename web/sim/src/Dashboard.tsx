// The tuning page (PLAN 25.6): the target, the state and the intensity; one card per persona; the
// feature areas and the actions' weights; schedule and limits; the presets; "act now"; the feed.
import { useState } from 'react';

import { Button } from '../../src/ui/Button';
import { Checkbox, Select } from '../../src/ui/inputs';
import type { Client, Schemas } from './api';
import {
  type Config,
  type PresetName,
  useActNow,
  useCatalogue,
  useChangeConfig,
  useConfig,
  useConfigAction,
  useSimState,
} from './hooks';
import { logsLink, useFeed } from './useFeed';

type Props = { client: Client; token: string };

export function Dashboard({ client, token }: Props) {
  const state = useSimState(client);
  const catalogue = useCatalogue(client);
  const config = useConfig(client);
  const change = useChangeConfig(client);
  const action = useConfigAction(client);
  const error = change.error ?? action.error;
  if (!state.data || !catalogue.data || !config.data) {
    return (
      <p className="sim-wait">
        {state.error || catalogue.error || config.error ? 'The simulator does not answer.' : 'Loading…'}
      </p>
    );
  }
  const st = state.data;
  const cat = catalogue.data;
  const cfg = config.data.config;
  const set = (f: (c: Config) => Config) => {
    change.mutate(f);
  };
  return (
    <div className="sim">
      <header className="sim-head">
        <h1>Simulator</h1>
        <span className="sim-target" title="The lab the personas use">
          {st.target}
        </span>
        <span className={st.paused ? 'tag sim-state paused' : 'tag sim-state'}>{st.paused ? 'Paused' : 'Running'}</span>
        <span className="sim-totals">
          {st.totals.ok} ok · {st.totals.error} errors · {st.totals.skipped} skipped
        </span>
        <label className="sim-intensity">
          Intensity <b>{(cfg.intensity / 1000).toFixed(1)}×</b>
          <input
            type="range"
            aria-label="Intensity"
            min={0}
            max={cat.ceilings.intensity}
            step={100}
            value={cfg.intensity}
            onChange={(e) => {
              const v = Number(e.target.value);
              set((c) => ({ ...c, intensity: v }));
            }}
          />
        </label>
        <Button
          {...(st.paused ? { variant: 'primary' as const } : {})}
          onClick={() => {
            action.mutate({ kind: st.paused ? 'resume' : 'pause' });
          }}
        >
          {st.paused ? 'Resume' : 'Pause'}
        </Button>
      </header>
      {error && (
        <p className="sim-error" role="alert">
          {error.message}
        </p>
      )}

      <section aria-label="Presets" className="sim-presets">
        <h2>Presets</h2>
        {cat.presets.map((name) => (
          <Button
            key={name}
            size="sm"
            onClick={() => {
              action.mutate({ kind: 'preset', name: name as PresetName });
            }}
          >
            {name.replace('-', ' ')}
          </Button>
        ))}
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            action.mutate({ kind: 'reset' });
          }}
        >
          Back to the defaults
        </Button>
      </section>

      <section aria-label="Personas" className="sim-personas">
        <h2>Personas</h2>
        <div className="sim-cards">
          {st.personas.map((p) => (
            <PersonaCard key={p.slug} p={p} cfg={cfg} set={set} />
          ))}
        </div>
      </section>

      <div className="sim-cols">
        <Matrix catalogue={cat} cfg={cfg} set={set} />
        <div>
          <Limits catalogue={cat} cfg={cfg} set={set} />
          <ActNow client={client} catalogue={cat} personas={st.personas} grafana={st.grafana} />
        </div>
      </div>

      <Feed token={token} grafana={st.grafana} />
    </div>
  );
}

function PersonaCard({
  p,
  cfg,
  set,
}: {
  p: Schemas['PersonaState'];
  cfg: Config;
  set: (f: (c: Config) => Config) => void;
}) {
  const doing = p.action ?? (p.last ? `${p.last.action} (${p.last.result})` : 'nothing yet');
  const total = p.counts.ok + p.counts.error + p.counts.skipped;
  const share = (n: number) => (total ? `${String(Math.round((n / total) * 100))}%` : '0%');
  return (
    <article className={p.on ? 'sim-card' : 'sim-card off'} aria-label={p.name}>
      <header>
        <b>{p.name}</b>
        <Checkbox
          label="On"
          checked={p.on}
          onChange={(on) => {
            set((c) => ({ ...c, off: on ? (c.off ?? []).filter((s) => s !== p.slug) : [...(c.off ?? []), p.slug] }));
          }}
        />
      </header>
      <p className="hint">{p.says}</p>
      <p className="sim-doing">
        {p.action ? 'Doing ' : 'Last: '}
        <span className="mono">{doing}</span>
      </p>
      <div
        className="sim-bar"
        aria-label={`${String(p.counts.ok)} ok, ${String(p.counts.error)} errors, ${String(p.counts.skipped)} skipped`}
      >
        <span className="ok" style={{ width: share(p.counts.ok) }} />
        <span className="err" style={{ width: share(p.counts.error) }} />
        <span className="skip" style={{ width: share(p.counts.skipped) }} />
      </div>
      {!p.signedIn && cfg.personas > 0 && <p className="hint">not signed in yet</p>}
    </article>
  );
}

function Matrix({
  catalogue,
  cfg,
  set,
}: {
  catalogue: Schemas['Catalogue'];
  cfg: Config;
  set: (f: (c: Config) => Config) => void;
}) {
  const own = (name: string) => Math.max(0, ...catalogue.characters.map((c) => c.weights[name] ?? 0));
  return (
    <section aria-label="Areas and weights" className="sim-matrix">
      <h2>Areas and weights</h2>
      <p className="hint">
        A weight set here replaces every character's own for that action; empty keeps theirs (the highest shown).
      </p>
      {catalogue.areas.map((area) => {
        const on = !(cfg.areasOff ?? []).includes(area);
        return (
          <fieldset key={area} className={on ? '' : 'off'}>
            <legend>
              <Checkbox
                label={area}
                checked={on}
                onChange={(v) => {
                  set((c) => ({
                    ...c,
                    areasOff: v ? (c.areasOff ?? []).filter((a) => a !== area) : [...(c.areasOff ?? []), area],
                  }));
                }}
              />
            </legend>
            {catalogue.actions
              .filter((a) => a.area === area)
              .map((a) => {
                const w = cfg.weights?.[a.name];
                return (
                  <label key={a.name} className="sim-weight">
                    <span className="mono">{a.name.slice(area.length + 1)}</span>
                    {a.mistake && <span className="tag">mistake</span>}
                    <input
                      className="inp"
                      type="number"
                      min={0}
                      max={1000}
                      aria-label={`Weight of ${a.name}`}
                      placeholder={String(own(a.name))}
                      value={w ?? ''}
                      onChange={(e) => {
                        const raw = e.target.value;
                        set((c) => {
                          const weights = Object.fromEntries(
                            Object.entries(c.weights ?? {}).filter(([k]) => k !== a.name),
                          );
                          if (raw !== '') weights[a.name] = Number(raw);
                          return { ...c, weights };
                        });
                      }}
                    />
                  </label>
                );
              })}
          </fieldset>
        );
      })}
    </section>
  );
}

function Limits({
  catalogue,
  cfg,
  set,
}: {
  catalogue: Schemas['Catalogue'];
  cfg: Config;
  set: (f: (c: Config) => Config) => void;
}) {
  const ceil = catalogue.ceilings;
  const num = (label: string, value: number, max: number, apply: (c: Config, v: number) => Config) => (
    <label className="sim-num">
      {label}
      <input
        className="inp"
        type="number"
        min={0}
        max={max}
        value={value}
        onChange={(e) => {
          const v = Number(e.target.value);
          set((c) => apply(c, v));
        }}
      />
      <span className="hint">at most {max}</span>
    </label>
  );
  return (
    <section aria-label="Schedule and limits" className="sim-limits">
      <h2>Schedule and limits</h2>
      {num('Personas', cfg.personas, ceil.personas, (c, v) => ({ ...c, personas: v }))}
      {num('Data days per real day', cfg.clockRate, ceil.clockRate, (c, v) => ({ ...c, clockRate: v }))}
      {num('Mistakes ‰', cfg.mistakes, 1000, (c, v) => ({ ...c, mistakes: v }))}
      {num('Questions to the assistant a day', cfg.assistantPerDay, ceil.assistantPerDay, (c, v) => ({
        ...c,
        assistantPerDay: v,
      }))}
      <Checkbox
        label="Follow the characters' daily hours"
        checked={cfg.activeHours}
        onChange={(v) => {
          set((c) => ({ ...c, activeHours: v }));
        }}
      />
      <p className="hint">
        Each persona acts {ceil.actionsPerMinute} times a minute at most; one assistant question runs at a time.
      </p>
    </section>
  );
}

function ActNow({
  client,
  catalogue,
  personas,
  grafana,
}: {
  client: Client;
  catalogue: Schemas['Catalogue'];
  personas: Schemas['PersonaState'][];
  grafana: string | undefined;
}) {
  const [persona, setPersona] = useState(personas.find((p) => p.slug === 'fox')?.slug ?? personas[0]?.slug ?? 'fox');
  const [act, setAct] = useState(
    catalogue.actions.find((a) => a.name === 'assistant.ask')?.name ?? catalogue.actions[0]?.name ?? '',
  );
  const now = useActNow(client);
  const ev = now.data;
  return (
    <section aria-label="Act now" className="sim-act">
      <h2>Act now</h2>
      <Select
        aria-label="Persona"
        value={persona}
        options={personas.map((p) => ({ value: p.slug, label: p.name }))}
        onChange={setPersona}
      />
      <Select
        aria-label="Action"
        value={act}
        options={catalogue.actions.map((a) => ({ value: a.name, label: a.name }))}
        onChange={setAct}
      />
      <Button
        variant="primary"
        disabled={now.isPending}
        onClick={() => {
          now.mutate({ persona, action: act });
        }}
      >
        {now.isPending ? 'Acting…' : 'Act now'}
      </Button>
      {now.error && <p className="sim-error">{now.error.message}</p>}
      {ev && (
        <p className="sim-result" aria-label="Result">
          {ev.persona} {ev.action}: <b>{ev.result}</b>
          {ev.requestId && <RequestId id={ev.requestId} grafana={grafana} />}
        </p>
      )}
    </section>
  );
}

function RequestId({ id, grafana }: { id: string; grafana: string | undefined }) {
  return (
    <span className="sim-rid">
      <code>{id}</code>
      <Button
        size="sm"
        variant="ghost"
        aria-label={`Copy ${id}`}
        onClick={() => {
          void navigator.clipboard.writeText(id);
        }}
      >
        Copy
      </Button>
      {grafana && (
        <a href={logsLink(grafana, id)} target="_blank" rel="noreferrer">
          Logs and trace
        </a>
      )}
    </span>
  );
}

function Feed({ token, grafana }: { token: string; grafana: string | undefined }) {
  const { events, live } = useFeed(token);
  return (
    <section aria-label="Feed" className="sim-feed">
      <h2>
        Feed <span className={live ? 'tag sim-live' : 'tag'}>{live ? 'live' : 'reconnecting'}</span>
      </h2>
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Persona</th>
            <th>Action</th>
            <th>Result</th>
            <th>Took</th>
            <th>Request</th>
          </tr>
        </thead>
        <tbody>
          {events.map((ev, i) => (
            <tr key={`${ev.time}-${ev.persona}-${String(i)}`} className={ev.result}>
              <td className="mono">{new Date(ev.time).toLocaleTimeString()}</td>
              <td>{ev.persona}</td>
              <td className="mono">{ev.action}</td>
              <td title={ev.error}>
                {ev.result}
                {ev.status ? ` ${String(ev.status)}` : ''}
              </td>
              <td className="mono">{ev.durationMs} ms</td>
              <td>{ev.requestId && <RequestId id={ev.requestId} grafana={grafana} />}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
