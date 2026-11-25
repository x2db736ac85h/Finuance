// Which of a provider's models a connection offers (Phase 26): its known models to tick, a field
// for any other id or pattern, and the default among them. The form keeps what it always kept, a
// comma-separated list and a default, so nothing about saving changes.
import { useId, useState } from 'react';

import { Checkbox, Select, TextInput } from '../ui/inputs';

import { isPattern, splitModels, type KnownModel } from './providers';

type Props = {
  known: readonly KnownModel[];
  /** The comma-separated list of ids and patterns. */
  models: string;
  defaultModel: string;
  onChange: (next: { models: string; defaultModel: string }) => void;
  /** The provider has a model list of its own, so a pattern can pick from it (Vertex AI has none). */
  patterns: boolean;
  /** Said under the list: what an empty one means for this kind of connection. */
  emptyHint: string;
};

export function ModelPicker({ known, models, defaultModel, onChange, patterns, emptyHint }: Props) {
  const otherId = useId();
  const all = splitModels(models);
  const knownIds = known.map((k) => k.id);
  const ticked = all.filter((m) => knownIds.includes(m));
  const other = all.filter((m) => !knownIds.includes(m));
  // the field's own text while typing (a comma just typed is not a model yet)
  const [typedOther, setTypedOther] = useState(other.join(', '));
  // one model's id can be a default; a pattern can't
  const ids = all.filter((m) => !isPattern(m));
  const set = (nextTicked: string[], nextOther: string[]) => {
    // the provider's order, then whatever was typed
    const next = [...knownIds.filter((id) => nextTicked.includes(id)), ...nextOther];
    const stillThere = next.includes(defaultModel) || next.some(isPattern) || next.length === 0;
    onChange({ models: next.join(', '), defaultModel: stillThere ? defaultModel : '' });
  };
  const defaults = [
    { value: '', label: 'Choose later' },
    ...ids.map((id) => ({ value: id, label: known.find((k) => k.id === id)?.label ?? id })),
    ...(defaultModel && !ids.includes(defaultModel) ? [{ value: defaultModel, label: defaultModel }] : []),
  ];
  return (
    <>
      <fieldset className="model-pick">
        <legend>Models offered</legend>
        {known.map((k) => (
          <Checkbox
            key={k.id}
            checked={ticked.includes(k.id)}
            onChange={(on) => {
              set(on ? [...ticked, k.id] : ticked.filter((id) => id !== k.id), other);
            }}
            label={
              <>
                <b>{k.label}</b> <span className="hint">{k.about}</span>
              </>
            }
          />
        ))}
        <label className="st-col" htmlFor={otherId}>
          <span>
            Other models{' '}
            <span className="hint">
              {patterns ? '(ids or patterns such as name-*, comma-separated)' : '(their ids, comma-separated)'}
            </span>
          </span>
          <TextInput
            id={otherId}
            autoComplete="off"
            mono
            value={typedOther}
            onChange={(e) => {
              setTypedOther(e.target.value);
              const typed = e.target.value.split(',').map((m) => m.trim());
              const kept = typed.filter((m) => m !== '' && !knownIds.includes(m));
              set([...ticked, ...typed.filter((m) => knownIds.includes(m))], kept);
            }}
          />
        </label>
        {all.length === 0 && <p className="hint">{emptyHint}</p>}
      </fieldset>
      <label className="st-col">
        Default model
        <Select
          value={defaultModel}
          options={defaults}
          onChange={(v) => {
            onChange({ models, defaultModel: v });
          }}
        />
      </label>
    </>
  );
}
