// Add or change an expense of a shared group (ADR 0028). A typed expense takes every field; a copy
// of one of the user's own transactions only changes its split (the rest follows the
// transaction). The split editor: equal between the members ticked (the default: everyone),
// shares by weight, or exact amounts that must add up; "for someone else" is one member taking
// all of it.

import { useState, type ReactNode } from 'react';

import {
  useAddExpense,
  useUpdateExpense,
  type SharedExpense,
  type SharedGroupDetail,
  type SharedMember,
} from '../api/hooks/shared';
import { money } from '../charts/lib/money';
import { todayISO } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { moneyInputText, parseMoneyInput } from '../lib/format';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { exactLeft, previewShares, toApiSplit, type SplitMode, type SplitState } from './split';

type Props = { detail: SharedGroupDetail; expense?: SharedExpense; onClose: () => void };

const MODES: { value: SplitMode; label: string }[] = [
  { value: 'equal', label: 'Equally' },
  { value: 'shares', label: 'By shares' },
  { value: 'exact', label: 'Exact amounts' },
];

function initialSplit(active: SharedMember[], e: SharedExpense | undefined): SplitState {
  const all = active.map((m) => m.id);
  if (!e) return { mode: 'equal', members: all, weights: {}, exact: {} };
  const members = e.shares.map((s) => s.memberId);
  const weights: Record<string, number> = {};
  const exact: Record<string, number> = {};
  for (const s of e.shares) {
    weights[s.memberId] = s.weight ?? 1;
    exact[s.memberId] = s.shareMinor;
  }
  return { mode: e.splitMode, members, weights, exact };
}

export function ExpenseDialog({ detail, expense, onClose }: Props) {
  const cur = detail.group.currency;
  const active = detail.members.filter((m) => m.state === 'active');
  const order = active.map((m) => m.id);
  const me = detail.members.find((m) => m.isMe);
  const copy = expense?.origin === 'transaction';
  const [amount, setAmount] = useState(expense ? moneyInputText(expense.amountMinor, cur) : '');
  const [bookedOn, setBookedOn] = useState(expense?.bookedOn ?? todayISO());
  const [payee, setPayee] = useState(expense?.payee ?? '');
  const [category, setCategory] = useState(expense?.category ?? '');
  const [note, setNote] = useState(expense?.note ?? '');
  const [paidBy, setPaidBy] = useState(expense?.paidBy ?? me?.id ?? '');
  const [split, setSplit] = useState<SplitState>(() => initialSplit(active, expense));
  const [error, setError] = useState('');
  const add = useAddExpense();
  const update = useUpdateExpense();
  const pending = add.isPending || update.isPending;

  const minor = parseMoneyInput(amount, cur);
  const preview = minor ? previewShares(split, minor, order) : null;
  const name = (id: string) => detail.members.find((m) => m.id === id)?.displayName ?? 'Former member';

  const submit = () => {
    if (!minor) {
      setError('Type the amount, like 24,50.');
      return;
    }
    if (!copy && !/^\d{4}-\d{2}-\d{2}$/.test(bookedOn)) {
      setError('Pick a date.');
      return;
    }
    if (!preview) {
      setError(
        split.mode === 'exact'
          ? `The amounts must add up to ${money(minor, cur)}.`
          : 'Tick at least one member to share it with.',
      );
      return;
    }
    const body = {
      bookedOn,
      amountMinor: minor,
      payee: payee.trim(),
      category: category.trim(),
      note: note.trim(),
      ...(paidBy ? { paidBy } : {}),
      split: toApiSplit(split, order),
    };
    const opts = {
      onSuccess: () => {
        toast(expense ? 'Saved' : `Added ${payee.trim() || 'the expense'}`);
        onClose();
      },
      onError: (e: unknown) => {
        setError(errorMessage(e));
      },
    };
    if (expense) update.mutate({ id: detail.group.id, expenseId: expense.id, body }, opts);
    else add.mutate({ id: detail.group.id, body }, opts);
  };

  const field = (label: string, input: ReactNode) => (
    <label className="st-col">
      {label}
      {input}
    </label>
  );
  return (
    <Modal
      open
      onClose={onClose}
      title={expense ? 'Change the expense' : 'Add an expense'}
      sub={detail.group.name}
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={pending} onClick={submit}>
            {expense ? 'Save' : 'Add'}
          </Button>
        </>
      }
    >
      <form
        className="adm-form cash-form"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        {copy && (
          <p className="hint">
            This is one of your transactions: its date, amount, payee, category and note follow it. Here you change only
            how it is split.
          </p>
        )}
        <div className="cash-row">
          {field(
            `Amount (${cur})`,
            <TextInput
              mono
              inputMode="decimal"
              autoFocus={!copy}
              disabled={copy}
              placeholder="24,50"
              value={amount}
              onChange={(e) => {
                setAmount(e.target.value);
              }}
            />,
          )}
          {field(
            'Date',
            <TextInput
              type="date"
              disabled={copy}
              value={bookedOn}
              onChange={(e) => {
                setBookedOn(e.target.value);
              }}
            />,
          )}
        </div>
        {field(
          'What for',
          <TextInput
            maxLength={200}
            disabled={copy}
            placeholder="Dinner, tickets, fuel…"
            value={payee}
            onChange={(e) => {
              setPayee(e.target.value);
            }}
          />,
        )}
        <div className="cash-row">
          {field(
            'Category',
            <TextInput
              maxLength={80}
              disabled={copy}
              value={category}
              onChange={(e) => {
                setCategory(e.target.value);
              }}
            />,
          )}
          {field(
            'Paid by',
            <Select
              value={paidBy}
              disabled={copy}
              options={active.map((m) => ({ value: m.id, label: m.isMe ? `${m.displayName} (you)` : m.displayName }))}
              onChange={setPaidBy}
            />,
          )}
        </div>
        {field(
          'Note',
          <TextInput
            maxLength={2000}
            disabled={copy}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />,
        )}
        <SplitEditor
          split={split}
          onChange={setSplit}
          members={active}
          amount={minor ?? 0}
          currency={cur}
          preview={preview}
        />
        {error && (
          <p className="set-err" role="alert">
            {error}
          </p>
        )}
        {preview && (
          <p className="hint" aria-live="polite">
            {order
              .filter((m) => preview[m])
              .map((m) => `${name(m)} ${money(preview[m] ?? 0, cur)}`)
              .join(' · ')}
          </p>
        )}
      </form>
    </Modal>
  );
}

type EditorProps = {
  split: SplitState;
  onChange: (s: SplitState) => void;
  members: SharedMember[];
  amount: number;
  currency: string;
  preview: Record<string, number> | null;
};

/** How an expense is shared: equally, by shares or as exact amounts. */
export function SplitEditor({ split, onChange, members, amount, currency, preview }: EditorProps) {
  const left = exactLeft(split, amount);
  // what was typed into each exact amount (formatted only when it came from the stored split)
  const [typed, setTyped] = useState<Record<string, string>>(() =>
    Object.fromEntries(Object.entries(split.exact).map(([m, v]) => [m, v ? moneyInputText(v, currency) : ''])),
  );
  return (
    <fieldset className="split-ed">
      <legend>Split</legend>
      <div className="cash-kind" role="radiogroup" aria-label="How to split">
        {MODES.map((m) => (
          <label key={m.value} className={split.mode === m.value ? 'on' : undefined}>
            <input
              type="radio"
              name="split-mode"
              checked={split.mode === m.value}
              onChange={() => {
                onChange({ ...split, mode: m.value });
              }}
            />
            {m.label}
          </label>
        ))}
      </div>
      <ul className="split-rows">
        {members.map((m) => {
          const on = split.members.includes(m.id);
          const label = m.isMe ? `${m.displayName} (you)` : m.displayName;
          return (
            <li key={m.id}>
              {split.mode === 'exact' ? (
                <label className="split-exact">
                  <span>{label}</span>
                  <TextInput
                    mono
                    inputMode="decimal"
                    aria-label={`${m.displayName}'s amount`}
                    value={typed[m.id] ?? ''}
                    onChange={(e) => {
                      const text = e.target.value;
                      setTyped((t) => ({ ...t, [m.id]: text }));
                      const v = parseMoneyInput(text, currency) ?? 0;
                      onChange({ ...split, exact: { ...split.exact, [m.id]: v } });
                    }}
                  />
                </label>
              ) : (
                <>
                  <Checkbox
                    label={label}
                    checked={on}
                    onChange={(c) => {
                      onChange({
                        ...split,
                        members: c ? [...split.members, m.id] : split.members.filter((x) => x !== m.id),
                      });
                    }}
                  />
                  {split.mode === 'shares' && on && (
                    <TextInput
                      mono
                      type="number"
                      min={1}
                      max={1000}
                      aria-label={`${m.displayName}'s shares`}
                      value={String(split.weights[m.id] ?? 1)}
                      onChange={(e) => {
                        onChange({ ...split, weights: { ...split.weights, [m.id]: Number(e.target.value) || 1 } });
                      }}
                    />
                  )}
                  <span className="split-v">{preview?.[m.id] ? money(preview[m.id] ?? 0, currency) : ''}</span>
                </>
              )}
            </li>
          );
        })}
      </ul>
      {split.mode === 'exact' && amount !== 0 && (
        <p className={left === 0 ? 'hint' : 'set-err'}>
          {left === 0
            ? 'Adds up.'
            : left > 0
              ? `${money(left, currency)} left to assign`
              : `${money(-left, currency)} too much`}
        </p>
      )}
    </fieldset>
  );
}
