// Administration → Defaults and features: one versioned document (ADR 0025).
// - Defaults are where a new user's view starts; anyone can change their own view afterwards.
// - Features are switches nobody can override (a switched-off feature disappears for everyone).
// - Limits are the defaults for every account; an account's own limits replace them.

import { useState } from 'react';

import { usePutInstanceSettings, useInstanceSettings, type InstanceSettings as Doc } from '../api/hooks/admin';
import { useMeta } from '../api/hooks/me';
import { errorMessage } from '../lib/errors';
import { CARD_KEYS } from '../stores/prefs';
import { BUILTIN_THEMES } from '../styles/themes';
import { Button } from '../ui/Button';
import { Checkbox, Select, type Option } from '../ui/inputs';
import { toast } from '../ui/toastStore';
import { LimitFields, parseLimits } from './Users';

const CARD_LABEL: Record<(typeof CARD_KEYS)[number], string> = {
  sum: 'Summary',
  cat: 'Spending by category',
  flow: 'Cash flow by month',
  pace: 'Month pace',
  payee: 'Top payees',
  heat: 'Daily spending',
};

const FEATURES: { key: 'overview' | 'assistant' | 'friends' | 'sharedGroups'; label: string; hint: string }[] = [
  { key: 'overview', label: 'Overview', hint: 'the monthly overview page' },
  { key: 'assistant', label: 'Assistant', hint: 'the AI assistant panel (also needs a connection)' },
  { key: 'friends', label: 'Friends', hint: 'friend codes and the friends list' },
  { key: 'sharedGroups', label: 'Shared groups', hint: 'sharing expenses with friends' },
];

/** obj without key (an unset default or switch is absent, not undefined). */
function without<T extends object, K extends keyof T>(obj: T, key: K): Omit<T, K> {
  return Object.fromEntries(Object.entries(obj).filter(([k]) => k !== key)) as Omit<T, K>;
}

export function InstanceSettings() {
  const settings = useInstanceSettings();
  if (!settings.data)
    return <div className="set-card">{settings.isError ? errorMessage(settings.error) : 'Loading…'}</div>;
  return <SettingsForm key={settings.data.version} doc={settings.data} />;
}

function SettingsForm({ doc }: { doc: Doc }) {
  const put = usePutInstanceSettings();
  const meta = useMeta().data;
  const [d, setD] = useState(doc.defaults);
  const [f, setF] = useState(doc.features);
  const [limits, setLimits] = useState<Record<string, string>>(() => {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(doc.limits))
      out[k] = k === 'maxUploadBytes' ? String(v / (1 << 20)) : String(v);
    return out;
  });
  const cards = d.cards ?? [...CARD_KEYS];
  const themes: Option<string>[] = [
    { value: '', label: 'Indigo (built in)' },
    ...BUILTIN_THEMES.filter((t) => t.id !== 'indigo').map((t) => ({ value: t.id, label: `${t.name} (built in)` })),
    ...(meta?.themes ?? []).map((t) => ({ value: t.id, label: t.name })),
  ];
  const save = () => {
    const parsed = parseLimits(limits);
    if (typeof parsed === 'string') {
      toast(parsed, { kind: 'error' });
      return;
    }
    // mutateAsync: the saved version remounts this form, and mutate()'s callbacks would be dropped
    void put.mutateAsync({ version: doc.version, defaults: d, features: f, limits: parsed }).then(
      () => {
        toast('Saved the instance settings');
      },
      (e: unknown) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    );
  };
  return (
    <>
      <div className="set-card">
        <div className="lbl">Where a new user's view starts</div>
        <p className="hint">People can change all of this for themselves; their own choices always win.</p>
        <label className="st-col">
          Colour theme
          <Select
            value={d.themeId ?? ''}
            options={themes}
            onChange={(v) => {
              setD(v ? { ...d, themeId: v } : without(d, 'themeId'));
            }}
          />
        </label>
        <label className="st-col">
          Mode
          <Select
            value={d.theme ?? 'system'}
            options={[
              { value: 'system', label: 'Follow the system' },
              { value: 'dark', label: 'Dark' },
              { value: 'light', label: 'Light' },
            ]}
            onChange={(v) => {
              setD({ ...d, theme: v });
            }}
          />
        </label>
        <Checkbox
          label="Show the charts above the table"
          checked={d.noViz !== true}
          onChange={(on) => {
            setD({ ...d, noViz: !on });
          }}
        />
        <Checkbox
          label="Show the sidebar"
          checked={d.sbOpen === true}
          onChange={(on) => {
            setD({ ...d, sbOpen: on });
          }}
        />
        <fieldset className="adm-cards">
          <legend>Charts shown</legend>
          {CARD_KEYS.map((k) => (
            <Checkbox
              key={k}
              label={CARD_LABEL[k]}
              checked={cards.includes(k)}
              onChange={(on) => {
                setD({
                  ...d,
                  cards: on ? CARD_KEYS.filter((c) => c === k || cards.includes(c)) : cards.filter((c) => c !== k),
                });
              }}
            />
          ))}
        </fieldset>
      </div>
      <div className="set-card">
        <div className="lbl">Features</div>
        <p className="hint">A feature that is off disappears for everyone, and its parts of the API answer 404.</p>
        {FEATURES.map((x) => (
          <Checkbox
            key={x.key}
            label={
              <>
                {x.label} <span className="hint">— {x.hint}</span>
              </>
            }
            checked={f[x.key] !== false}
            onChange={(on) => {
              setF(on ? without(f, x.key) : { ...f, [x.key]: false });
            }}
          />
        ))}
      </div>
      <div className="set-card">
        <div className="lbl">Default limits</div>
        <p className="hint">
          For every account without its own limits. Empty means no limit, or the server's own limit for file size and
          assistant questions, which an administrator can only lower.
        </p>
        <LimitFields text={limits} onChange={setLimits} placeholder="no limit" />
      </div>
      <div className="set-actions">
        <Button variant="primary" disabled={put.isPending} onClick={save}>
          Save
        </Button>
      </div>
    </>
  );
}
