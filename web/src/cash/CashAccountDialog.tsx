// "New cash account" (ADR 0029): a wallet whose transactions are typed by hand instead of
// imported. The kind can't change later, so the dialog says what it is for.

import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useCreateCashAccount } from '../api/hooks/accounts';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { useCashDialogs } from './store';

const CURRENCIES = ['PLN', 'EUR', 'USD', 'GBP', 'CHF', 'CZK', 'SEK', 'NOK', 'DKK', 'HUF'] as const;

export function CashAccountDialog() {
  const open = useCashDialogs((s) => s.newAccount);
  const close = useCashDialogs((s) => s.close);
  if (!open) return null;
  return <Form onClose={close} />;
}

function Form({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('Cash');
  const [currency, setCurrency] = useState<string>('PLN');
  const [error, setError] = useState('');
  const create = useCreateCashAccount();
  const navigate = useNavigate();
  const submit = () => {
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), currency },
      {
        onSuccess: (a) => {
          toast(`Created the cash account ${a.name}`);
          onClose();
          void navigate(`/t/${a.id}`);
        },
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };
  return (
    <Modal
      open
      onClose={onClose}
      title="New cash account"
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!name.trim() || create.isPending} onClick={submit}>
            Create
          </Button>
        </>
      }
    >
      <form
        className="adm-form"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <p className="hint">
          For money that never reaches a bank file: you add its expenses by hand. Bank statements go into a bank
          account.
        </p>
        <label className="st-col">
          Name
          <TextInput
            value={name}
            maxLength={80}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />
        </label>
        <label className="st-col">
          Currency
          <Select value={currency} options={CURRENCIES.map((c) => ({ value: c, label: c }))} onChange={setCurrency} />
        </label>
        <p className="set-err" role="alert">
          {error}
        </p>
      </form>
    </Modal>
  );
}
