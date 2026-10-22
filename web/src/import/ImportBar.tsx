// The settings bar of the import window (legacy #impBar): account, encoding, delimiter, header row,
// skip rows, date order, amount mode and the three switches.

import { useState } from 'react';

import { useAccounts, useCreateAccount } from '../api/hooks/accounts';
import type { ImportChanges, ImportOptions, ImportPreview } from '../api/hooks/imports';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput, type Option } from '../ui/inputs';
import { toast } from '../ui/toastStore';
import type { Effective } from './editor';

type Props = {
  preview: ImportPreview;
  eff: Effective;
  /** The account can't change (a committed import keeps its account). */
  lockAccount?: boolean;
  /** Nothing can change (a failed import: only Try again or Cancel). */
  readOnly?: boolean;
  onChange: (ch: ImportChanges) => void;
};

type Enc = NonNullable<ImportOptions['encoding']>;
type Delim = NonNullable<ImportOptions['delimiter']>;

const ENCODINGS: Option<Enc>[] = [
  { value: 'auto', label: 'Auto (UTF-8 → Windows-1250)' },
  { value: 'utf-8', label: 'UTF-8' },
  { value: 'windows-1250', label: 'Windows-1250 (Central EU)' },
  { value: 'iso-8859-2', label: 'ISO-8859-2' },
  { value: 'windows-1252', label: 'Windows-1252 (Western)' },
];

export const DELIMITERS: Option<Delim>[] = [
  { value: 'auto', label: 'Auto-detect' },
  { value: ';', label: 'Semicolon ;' },
  { value: ',', label: 'Comma ,' },
  { value: '\\t', label: 'Tab' },
  { value: '|', label: 'Pipe |' },
];

/** "Semicolon ;" for a delimiter as the API names it (";", "\t" …). */
export function delimiterLabel(d: string): string {
  return DELIMITERS.find((o) => o.value === d || (d === '\t' && o.value === '\\t'))?.label ?? d;
}

export function ImportBar({ preview, eff, lockAccount, readOnly, onChange }: Props) {
  const o = eff.options;
  const opt = (options: ImportOptions) => {
    onChange({ options });
  };
  const locked = !!readOnly;
  const header =
    o.headerRow === 'none' ? '0' : o.headerRow === 'auto' || o.headerRow === undefined ? '' : String(o.headerRow);
  return (
    <div className="imp-bar">
      <AccountPicker
        value={eff.accountId}
        suggested={preview.suggestedAccount}
        disabled={!!readOnly || !!lockAccount}
        onChange={(accountId) => {
          onChange({ accountId });
        }}
      />
      <label>
        Encoding
        <Select<Enc>
          value={o.encoding ?? 'auto'}
          options={ENCODINGS}
          disabled={locked}
          onChange={(encoding) => {
            opt({ encoding });
          }}
        />
      </label>
      <label>
        Delimiter
        <Select<Delim>
          value={o.delimiter ?? 'auto'}
          options={DELIMITERS}
          disabled={locked}
          onChange={(delimiter) => {
            opt({ delimiter });
          }}
        />
      </label>
      <label>
        Header row
        <TextInput
          mono
          type="number"
          min={0}
          step={1}
          placeholder="auto"
          value={header}
          disabled={locked}
          title="Row number of the column names. Empty = automatic, 0 = the file has no header row"
          onChange={(e) => {
            const v = e.target.value.trim();
            const n = Number(v);
            if (v === '') opt({ headerRow: 'auto' });
            else if (n === 0) opt({ headerRow: 'none', skipRows: Math.max(1, o.skipRows ?? 1) });
            else if (Number.isInteger(n) && n > 0) opt({ headerRow: n });
          }}
        />
      </label>
      {o.headerRow === 'none' && (
        <label>
          Data starts at row
          <TextInput
            mono
            type="number"
            min={1}
            step={1}
            value={String(Math.max(1, o.skipRows ?? 1))}
            disabled={locked}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (Number.isInteger(n) && n >= 1) opt({ skipRows: n });
            }}
          />
        </label>
      )}
      <label>
        Dates
        <Select
          value={o.dateOrder ?? 'dmy'}
          options={[
            { value: 'dmy', label: 'Day first — 31.12.2025' },
            { value: 'mdy', label: 'Month first — 12/31/2025' },
          ]}
          disabled={locked}
          onChange={(dateOrder) => {
            opt({ dateOrder });
          }}
        />
      </label>
      <label>
        Amount
        <Select
          value={o.amountMode ?? 'single'}
          options={[
            { value: 'single', label: 'One signed column' },
            { value: 'debit_credit', label: 'Debit and credit columns' },
          ]}
          disabled={locked}
          onChange={(amountMode) => {
            opt({ amountMode });
          }}
        />
      </label>
      <div className="imp-chk">
        <Checkbox
          checked={!!o.invertSign}
          disabled={locked}
          onChange={(invertSign) => {
            opt({ invertSign });
          }}
          label={
            <>
              Invert amount sign <span className="muted">(bank exports expenses as positive)</span>
            </>
          }
        />
        <Checkbox
          checked={o.dropShort !== false}
          disabled={locked}
          onChange={(dropShort) => {
            opt({ dropShort });
          }}
          label="Skip summary / footer rows"
        />
        <Checkbox
          checked={o.stripQuote !== false}
          disabled={locked}
          onChange={(stripQuote) => {
            opt({ stripQuote });
          }}
          label={
            <>
              Strip leading <code>&apos;</code> (Excel text marker)
            </>
          }
        />
      </div>
    </div>
  );
}

const NEW = '__new';

function AccountPicker({
  value,
  suggested,
  disabled,
  onChange,
}: {
  value: string | undefined;
  suggested: ImportPreview['suggestedAccount'];
  disabled: boolean;
  onChange: (id: string) => void;
}) {
  const accounts = useAccounts();
  const create = useCreateAccount();
  const [creating, setCreating] = useState(!value && !!suggested.createName);
  const [name, setName] = useState(suggested.createName ?? '');
  if (creating && !disabled) {
    const submit = () => {
      const n = name.trim();
      if (!n) return;
      create.mutate(n, {
        onSuccess: (a) => {
          onChange(a.id);
          setCreating(false);
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      });
    };
    return (
      <label className="imp-acc">
        New account
        <span className="row2">
          <TextInput
            value={name}
            placeholder="Account name"
            aria-label="New account name"
            autoFocus
            onChange={(e) => {
              setName(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                submit();
              }
            }}
          />
          <Button size="sm" variant="primary" disabled={!name.trim() || create.isPending} onClick={submit}>
            Create
          </Button>
          {(accounts.data?.length ?? 0) > 0 && (
            <button
              type="button"
              className="link"
              onClick={() => {
                setCreating(false);
              }}
            >
              Pick existing
            </button>
          )}
        </span>
      </label>
    );
  }
  const options: Option<string>[] = [
    ...(value ? [] : [{ value: '', label: 'Choose an account…' }]),
    ...(accounts.data ?? []).map((a) => ({
      value: a.id,
      label: a.id === suggested.id ? `${a.name} (matches the file)` : a.name,
    })),
    { value: NEW, label: 'New account…' },
  ];
  return (
    <label className="imp-acc">
      Account
      <Select
        value={value ?? ''}
        options={options}
        disabled={disabled}
        onChange={(v) => {
          if (v === NEW) setCreating(true);
          else if (v) onChange(v);
        }}
      />
    </label>
  );
}
