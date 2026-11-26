// The assistant's memory (Phase 27, ADR 0046): one note of yours that goes with every question while
// it is on. Kept here, in your own settings, so that what the assistant "knows" about you is
// always in front of you to read, change or clear.

import { useState } from 'react';

import {
  MEMORY_MAX_CHARS,
  useAssistantSettings,
  useSaveAssistantSettings,
  type AssistantSettings,
} from '../api/hooks/assistant';
import { errorMessage } from '../lib/errors';
import { fmtInt } from '../lib/format';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/inputs';
import { toast } from '../ui/toastStore';

/** About how many tokens a text is: four characters each, as a rule of thumb for European languages. */
export const approxTokens = (text: string): number => Math.ceil(text.length / 4);

export function MemoryCard() {
  const settings = useAssistantSettings();
  // nothing until the settings are read: the form starts from what is stored
  if (!settings.data) return null;
  return <MemoryForm key={settings.dataUpdatedAt} settings={settings.data} />;
}

function MemoryForm({ settings }: { settings: AssistantSettings }) {
  const save = useSaveAssistantSettings();
  const stored = settings.memory ?? { enabled: true, auto: false, text: '' };
  const [text, setText] = useState(stored.text);
  const over = text.length > MEMORY_MAX_CHARS;
  const put = (memory: NonNullable<AssistantSettings['memory']>, done: string) => {
    save.mutate(
      { ...settings, memory },
      {
        onSuccess: () => {
          toast(done);
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  return (
    <div className="set-card" id="memory">
      <div className="lbl">Assistant memory</div>
      <p className="hint">
        A note the assistant reads with every question: who you are for it, how you like answers, what to keep in mind
        (“I am paid on the 10th”, “answer in Polish”). It is sent to the model you use, a cloud one included, so leave
        out what a model should not see.
      </p>
      <Checkbox
        label="Use this note in my questions"
        checked={stored.enabled}
        disabled={save.isPending}
        onChange={(enabled) => {
          put({ ...stored, enabled }, enabled ? 'The assistant uses your note again' : 'The note is kept but not used');
        }}
      />
      <label className="st-col">
        <span>
          The note{' '}
          <span className={over ? 'set-err' : 'hint'}>
            {fmtInt(text.length)} of {fmtInt(MEMORY_MAX_CHARS)} characters, about {fmtInt(approxTokens(text))} tokens
          </span>
        </span>
        <textarea
          className="inp memory-text"
          rows={6}
          value={text}
          aria-invalid={over || undefined}
          placeholder="Nothing yet. Write a few lines, or let the assistant suggest them (its settings: Automatic memories)."
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
      </label>
      <div className="set-actions">
        <Button
          variant="danger"
          disabled={save.isPending || (stored.text === '' && text === '')}
          onClick={() => {
            setText('');
            put({ ...stored, text: '' }, 'Memory cleared');
          }}
        >
          Clear
        </Button>
        <Button
          variant="primary"
          disabled={save.isPending || over || text.trim() === stored.text}
          onClick={() => {
            put({ ...stored, text: text.trim() }, 'Saved your note');
          }}
        >
          Save
        </Button>
      </div>
    </div>
  );
}
