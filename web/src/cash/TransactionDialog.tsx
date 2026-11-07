// Add or change a transaction typed into a cash account (ADR 0029). Money out is the default; the
// amount is typed as a positive number and the kind decides the sign. The category is a name: a
// new one is created, like typing one into the inspector.

import { useState, type ReactNode } from 'react';

import { useGroups } from '../api/hooks/groups';
import {
  useAddTransaction,
  useCategories,
  useEditTransaction,
  useTransaction,
  type TransactionDetail,
} from '../api/hooks/transactions';
import { todayISO } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { moneyInputText, parseMoneyInput } from '../lib/format';
import { useViews } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { useCashDialogs, type TransactionDialog as Dialog } from './store';

export function TransactionDialog() {
  const d = useCashDialogs((s) => s.transaction);
  const close = useCashDialogs((s) => s.close);
  const detail = useTransaction(d?.editId ?? null);
  if (!d) return null;
  if (d.editId && !detail.data) return null; // the form starts from the stored values
  return <Form key={d.editId ?? 'new'} dialog={d} detail={d.editId ? detail.data : undefined} onClose={close} />;
}

type Kind = 'out' | 'in';

function Form({
  dialog,
  detail,
  onClose,
}: {
  dialog: Dialog;
  detail: TransactionDetail | undefined;
  onClose: () => void;
}) {
  const initialAmount = detail?.amount.amountMinor ?? 0;
  const [kind, setKind] = useState<Kind>(initialAmount > 0 ? 'in' : 'out');
  const [amount, setAmount] = useState(detail ? moneyInputText(Math.abs(initialAmount), dialog.currency) : '');
  const [bookedOn, setBookedOn] = useState(detail?.bookedOn ?? todayISO());
  const [payee, setPayee] = useState(detail?.payee ?? '');
  const [description, setDescription] = useState(detail?.description ?? '');
  const [category, setCategory] = useState(detail?.category?.name ?? '');
  const [groupId, setGroupId] = useState(detail?.group?.id ?? '');
  const [note, setNote] = useState(detail?.note ?? '');
  const [error, setError] = useState('');
  const cats = useCategories().data ?? [];
  const groups = useGroups().data ?? [];
  const add = useAddTransaction();
  const edit = useEditTransaction();
  const pending = add.isPending || edit.isPending;

  const parsed = parseMoneyInput(amount, dialog.currency);
  const minor = parsed === null ? null : Math.abs(parsed) * (kind === 'out' ? -1 : 1);
  const valid = minor !== null && minor !== 0 && payee.trim() !== '' && /^\d{4}-\d{2}-\d{2}$/.test(bookedOn);

  const done = (verb: string) => {
    toast(`${verb} ${payee.trim()}`);
    onClose();
  };
  const fail = (e: unknown) => {
    setError(errorMessage(e));
  };
  const submit = () => {
    if (!valid) {
      setError(
        minor === null || minor === 0
          ? 'Type the amount, like 24,50.'
          : payee.trim()
            ? 'Pick a date.'
            : 'Say who was paid (or who paid you).',
      );
      return;
    }
    const cat = category.trim();
    if (!detail) {
      add.mutate(
        {
          accountId: dialog.accountId,
          bookedOn,
          amountMinor: minor,
          payee: payee.trim(),
          ...(description.trim() ? { description: description.trim() } : {}),
          ...(cat ? { categoryName: cat } : {}),
          ...(groupId ? { groupId } : {}),
          ...(note.trim() ? { note: note.trim() } : {}),
        },
        {
          onSuccess: (t) => {
            useViews.getState().patch(dialog.accountId, { selectedId: t.id });
            done('Added');
          },
          onError: fail,
        },
      );
      return;
    }
    edit.mutate(
      {
        id: detail.id,
        body: {
          bookedOn,
          amountMinor: minor,
          payee: payee.trim(),
          description: description.trim(),
          note: note.trim(),
          ...(cat ? { categoryName: cat } : detail.category ? { resetCategory: true } : {}),
        },
      },
      {
        onSuccess: () => {
          done('Saved');
        },
        onError: fail,
      },
    );
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
      title={detail ? 'Change the transaction' : 'Add a transaction'}
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={pending} onClick={submit}>
            {detail ? 'Save' : 'Add'}
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
        <div className="cash-kind" role="radiogroup" aria-label="Kind">
          {(['out', 'in'] as const).map((k) => (
            <label key={k} className={kind === k ? 'on' : undefined}>
              <input
                type="radio"
                name="cash-kind"
                checked={kind === k}
                onChange={() => {
                  setKind(k);
                }}
              />
              {k === 'out' ? 'Money out' : 'Money in'}
            </label>
          ))}
        </div>
        <div className="form-row">
          {field(
            `Amount (${dialog.currency})`,
            <TextInput
              mono
              inputMode="decimal"
              autoFocus
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
              value={bookedOn}
              onChange={(e) => {
                setBookedOn(e.target.value);
              }}
            />,
          )}
        </div>
        {field(
          kind === 'out' ? 'Paid to' : 'Received from',
          <TextInput
            maxLength={200}
            value={payee}
            onChange={(e) => {
              setPayee(e.target.value);
            }}
          />,
        )}
        {field(
          'Description',
          <TextInput
            maxLength={500}
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
            }}
          />,
        )}
        <div className="form-row">
          {field(
            'Category',
            <TextInput
              list="cash-categories"
              maxLength={80}
              placeholder="Uncategorised"
              value={category}
              onChange={(e) => {
                setCategory(e.target.value);
              }}
            />,
          )}
          {!detail && // a group is changed with the group tools afterwards
            groups.length > 0 &&
            field(
              'Group',
              <Select
                value={groupId}
                options={[{ value: '', label: 'No group' }, ...groups.map((g) => ({ value: g.id, label: g.name }))]}
                onChange={setGroupId}
              />,
            )}
        </div>
        <datalist id="cash-categories">
          {cats.map((c) => (
            <option key={c.id} value={c.name} />
          ))}
        </datalist>
        {field(
          'Note',
          <TextInput
            maxLength={2000}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />,
        )}
        <p className="set-err" role="alert">
          {error}
        </p>
      </form>
    </Modal>
  );
}
