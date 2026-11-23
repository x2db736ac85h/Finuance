// The tuning page (PLAN 25.6): the target, the state and the intensity (in load mode the target
// rate, ADR 0044); the presets; load mode; one card per persona (per animal beyond twenty); the
// feature areas and the actions' weights; schedule and limits; randomness; "act now"; the feed.
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
  useRebuildHistory,
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
        {cfg.mode === 'load' ? (
          <span className="sim-intensity" aria-label="Load now">
            Load <b>{Math.round(st.rate)} a second</b>
            {st.loadEndsAt && <span className="hint">until {new Date(st.loadEndsAt).toLocaleTimeString()}</span>}
          </span>
        ) : (
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
        )}
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

      <Load catalogue={cat} cfg={cfg} set={set} />
      <History client={client} catalogue={cat} cfg={cfg} state={st} set={set} />

      <section aria-label="Personas" className="sim-personas">
        <h2>Personas</h2>
        <div className="sim-cards">
          {st.personas.length > GROUP_ABOVE
            ? byAnimal(st.personas).map((g) => <AnimalCard key={g.animal} group={g} />)
            : st.personas.map((p) => <PersonaCard key={p.slug} p={p} cfg={cfg} set={set} />)}
        </div>
      </section>

      <div className="sim-cols">
        <Matrix catalogue={cat} cfg={cfg} set={set} />
        <div>
          <Limits catalogue={cat} cfg={cfg} set={set} />
          <Randomness cfg={cfg} set={set} />
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
      {p.history && <p className="hint">history {p.history}</p>}
      <div
        className="sim-bar"
        role="img"
        aria-label={`${String(p.counts.ok)} ok, ${String(p.counts.error)} errors, ${String(p.counts.skipped)} skipped`}
      >
        <span className="ok" style={{ width: share(p.counts.ok) }} />
        <span className="err" style={{ width: share(p.counts.error) }} />
        <span className="skip" style={{ width: share(p.counts.skipped) }} />
      </div>
      {p.missing ? (
        <p className="hint">not in the lab: make more animals (SIM_ANIMALS) or check the seed</p>
      ) : (
        !p.signedIn && cfg.personas > 0 && <p className="hint">not signed in yet</p>
      )}
    </article>
  );
}

/** Beyond this many personas the cards show one animal each, with its copies counted together. */
const GROUP_ABOVE = 20;

type AnimalGroup = {
  animal: string;
  says: string;
  playing: number;
  signedIn: number;
  missing: number;
  counts: Schemas['Counts'];
};

function byAnimal(personas: Schemas['PersonaState'][]): AnimalGroup[] {
  const groups = new Map<string, AnimalGroup>();
  for (const p of personas) {
    const g = groups.get(p.animal) ?? {
      animal: p.animal,
      says: p.says,
      playing: 0,
      signedIn: 0,
      missing: 0,
      counts: { ok: 0, error: 0, skipped: 0 },
    };
    g.playing += p.on ? 1 : 0;
    g.signedIn += p.signedIn ? 1 : 0;
    g.missing += p.missing ? 1 : 0;
    g.counts = {
      ok: g.counts.ok + p.counts.ok,
      error: g.counts.error + p.counts.error,
      skipped: g.counts.skipped + p.counts.skipped,
    };
    groups.set(p.animal, g);
  }
  return [...groups.values()];
}

function AnimalCard({ group: g }: { group: AnimalGroup }) {
  const name = g.animal.charAt(0).toUpperCase() + g.animal.slice(1);
  const total = g.counts.ok + g.counts.error + g.counts.skipped;
  const share = (n: number) => (total ? `${String(Math.round((n / total) * 100))}%` : '0%');
  return (
    <article className={g.playing ? 'sim-card' : 'sim-card off'} aria-label={name}>
      <header>
        <b>{name}</b>
        <span className="tag">×{g.playing}</span>
      </header>
      <p className="hint">{g.says}</p>
      <p className="sim-doing">
        {g.signedIn} signed in{g.missing > 0 && `, ${String(g.missing)} not in the lab`}
      </p>
      <div
        className="sim-bar"
        role="img"
        aria-label={`${String(g.counts.ok)} ok, ${String(g.counts.error)} errors, ${String(g.counts.skipped)} skipped`}
      >
        <span className="ok" style={{ width: share(g.counts.ok) }} />
        <span className="err" style={{ width: share(g.counts.error) }} />
        <span className="skip" style={{ width: share(g.counts.skipped) }} />
      </div>
    </article>
  );
}

type Setter = (f: (c: Config) => Config) => void;

/** A number field held to [0, max] by the simulator; the hint names the ceiling. */
function NumberField({
  label,
  value,
  max,
  min = 0,
  apply,
  set,
}: {
  label: string;
  value: number;
  max: number;
  min?: number;
  apply: (c: Config, v: number) => Config;
  set: Setter;
}) {
  return (
    <label className="sim-num">
      {label}
      <input
        className="inp"
        type="number"
        min={min}
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
}

/** Load mode (ADR 0044): one lab-wide target instead of the characters' pace. */
function Load({ catalogue, cfg, set }: { catalogue: Schemas['Catalogue']; cfg: Config; set: Setter }) {
  const ceil = catalogue.ceilings;
  const load = cfg.mode === 'load';
  const needed = Math.ceil(cfg.rate / ceil.loadActionsPerSecond);
  return (
    <section aria-label="Load" className="sim-load">
      <h2>Load</h2>
      <div className="sim-modes" role="radiogroup" aria-label="Mode">
        {(['realistic', 'load'] as const).map((mode) => (
          <label key={mode} className="sim-mode">
            <input
              type="radio"
              name="sim-mode"
              value={mode}
              checked={cfg.mode === mode}
              onChange={() => {
                set((c) => ({ ...c, mode }));
              }}
            />
            {mode === 'realistic' ? 'Realistic: the characters at their own pace' : 'Load: one target for the lab'}
          </label>
        ))}
      </div>
      {load && (
        <>
          <div className="sim-row">
            <NumberField
              label="Actions a second"
              value={cfg.rate}
              min={1}
              max={ceil.rate}
              apply={(c, v) => ({ ...c, rate: v })}
              set={set}
            />
            <NumberField
              label="Ramp, seconds"
              value={cfg.rampSeconds}
              max={ceil.rampSeconds}
              apply={(c, v) => ({ ...c, rampSeconds: v })}
              set={set}
            />
            <NumberField
              label="Then stop after, minutes"
              value={cfg.loadMinutes}
              min={1}
              max={ceil.loadMinutes}
              apply={(c, v) => ({ ...c, loadMinutes: v })}
              set={set}
            />
          </div>
          <Checkbox
            label="Push through server errors (to find the breaking point)"
            checked={cfg.pushThrough}
            onChange={(v) => {
              set((c) => ({ ...c, pushThrough: v }));
            }}
          />
          <p className={needed > cfg.personas ? 'hint warn' : 'hint'}>
            An animal acts at most {ceil.loadActionsPerSecond} times a second, so {cfg.rate} a second needs at least{' '}
            {needed} animals; {cfg.personas} play. Load mode returns to realistic mode by itself.
          </p>
        </>
      )}
    </section>
  );
}

/** The history profile (ADR 0044): how much a new or rebuilt history holds, and Rebuild history. */
function History({
  client,
  catalogue,
  cfg,
  state,
  set,
}: {
  client: Client;
  catalogue: Schemas['Catalogue'];
  cfg: Config;
  state: Schemas['State'];
  set: Setter;
}) {
  const ceil = catalogue.ceilings.history;
  const rebuild = useRebuildHistory(client);
  const field = (label: string, key: keyof Config['history'], min: number, max: number) => (
    <NumberField
      label={label}
      value={cfg.history[key]}
      min={min}
      max={max}
      apply={(c, v) => ({ ...c, history: { ...c.history, [key]: v } })}
      set={set}
    />
  );
  return (
    <section aria-label="History" className="sim-load">
      <h2>History</h2>
      <div className="sim-row">
        {field('Spending a day, ×', 'density', 1, ceil.density)}
        {field('Amount spread, %', 'spread', 50, ceil.spread)}
        {field('Shops per merchant', 'variety', 1, ceil.variety)}
        {field('Years back', 'years', 1, ceil.years)}
      </div>
      <p className="hint">
        About {state.historyEstimate.toLocaleString()} transactions per animal. New animals start with this profile; the
        others keep theirs until their history is rebuilt.
      </p>
      <div className="sim-seed">
        <Button
          onClick={() => {
            rebuild.mutate();
          }}
        >
          Rebuild history
        </Button>
        {state.rebuild.pending > 0 && (
          <span className="hint" role="status">
            {state.rebuild.pending} animals still to rebuild
          </span>
        )}
      </div>
      <p className="hint">
        Each animal that plays deletes its two bank accounts, makes them again and imports the history through the
        normal upload, a few at a time.
      </p>
    </section>
  );
}

/** The run seed, how much the timing varies, bursts and the share of mistakes. */
function Randomness({ cfg, set }: { cfg: Config; set: Setter }) {
  const load = cfg.mode === 'load';
  return (
    <section aria-label="Randomness" className="sim-limits">
      <h2>Randomness</h2>
      <div className="sim-seed">
        <NumberField
          label="Run seed"
          value={cfg.runSeed}
          min={1}
          max={Number.MAX_SAFE_INTEGER}
          apply={(c, v) => ({ ...c, runSeed: v })}
          set={set}
        />
        <Button
          size="sm"
          onClick={() => {
            set((c) => ({ ...c, runSeed: 0 }));
          }}
        >
          New seed
        </Button>
      </div>
      <p className="hint">The same seed makes the same choices again; the statements stay the same either way.</p>
      <label className="sim-num">
        Timing jitter <b>{cfg.jitter}%</b>
        <input
          type="range"
          aria-label="Timing jitter"
          min={0}
          max={100}
          step={5}
          value={cfg.jitter}
          onChange={(e) => {
            const v = Number(e.target.value);
            set((c) => ({ ...c, jitter: v }));
          }}
        />
        <span className="hint">
          {load
            ? 'how much the gaps between actions vary'
            : "100% is the characters' own range, 0 always their average"}
        </span>
      </label>
      <label className="sim-num">
        Bursts <b>{cfg.bursts}‰</b>
        <input
          type="range"
          aria-label="Bursts"
          min={0}
          max={1000}
          step={10}
          value={cfg.bursts}
          onChange={(e) => {
            const v = Number(e.target.value);
            set((c) => ({ ...c, bursts: v }));
          }}
        />
        <span className="hint">
          {load
            ? 'how much may come at once after a quiet moment (1000‰: a second’s worth)'
            : 'the chance an action starts a few quick ones in a row'}
        </span>
      </label>
      <NumberField
        label="Mistakes ‰"
        value={cfg.mistakes}
        max={1000}
        apply={(c, v) => ({ ...c, mistakes: v })}
        set={set}
      />
    </section>
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

function Limits({ catalogue, cfg, set }: { catalogue: Schemas['Catalogue']; cfg: Config; set: Setter }) {
  const ceil = catalogue.ceilings;
  const load = cfg.mode === 'load';
  const num = (label: string, value: number, max: number, apply: (c: Config, v: number) => Config) => (
    <NumberField label={label} value={value} max={max} apply={apply} set={set} />
  );
  return (
    <section aria-label="Schedule and limits" className="sim-limits">
      <h2>Schedule and limits</h2>
      {num('Personas', cfg.personas, load ? ceil.loadPersonas : ceil.personas, (c, v) => ({ ...c, personas: v }))}
      {num('Data days per real day', cfg.clockRate, ceil.clockRate, (c, v) => ({ ...c, clockRate: v }))}
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
        {load
          ? `In load mode each persona acts ${String(ceil.loadActionsPerSecond)} times a second at most; the daily hours don't apply.`
          : `Each persona acts ${String(ceil.actionsPerMinute)} times a minute at most.`}{' '}
        One assistant question runs at a time.
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
