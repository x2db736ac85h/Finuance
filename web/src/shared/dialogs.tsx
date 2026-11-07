// The Shared tab's smaller windows (Phase 19, ADR 0028): settle up (a payment between two members,
// pre-filled with the suggested transfer), share a group (a new shared group, from a personal group
// or empty, with the friends to invite) and the owner's group settings.

import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useFriends } from '../api/hooks/friends';
import {
  useCreateShared,
  useInvite,
  useSettle,
  useUpdateShared,
  type SharedGroup,
  type SharedGroupDetail,
  type SharedKind,
} from '../api/hooks/shared';
import { money } from '../charts/lib/money';
import { todayISO } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { moneyInputText, parseMoneyInput } from '../lib/format';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { KIND_LABEL } from './text';

const KINDS = (Object.keys(KIND_LABEL) as SharedKind[]).map((k) => ({ value: k, label: KIND_LABEL[k] }));

type SettleProps = { detail: SharedGroupDetail; onClose: () => void };

/** Record a payment. The caller must be the payer or the payee (the server checks too). */
export function SettleDialog({ detail, onClose }: SettleProps) {
  const cur = detail.group.currency;
  const me = detail.members.find((m) => m.isMe);
  const mine = detail.transfers.find((t) => t.from === me?.id || t.to === me?.id);
  const people = detail.members.filter((m) => m.state !== 'invited');
  const [from, setFrom] = useState(mine?.from ?? me?.id ?? '');
  const [to, setTo] = useState(mine?.to ?? people.find((m) => !m.isMe)?.id ?? '');
  const [amount, setAmount] = useState(mine ? moneyInputText(mine.amountMinor, cur) : '');
  const [settledOn, setSettledOn] = useState(todayISO());
  const [note, setNote] = useState('');
  const [error, setError] = useState('');
  const settle = useSettle();
  const options = people.map((m) => ({ value: m.id, label: m.isMe ? `${m.displayName} (you)` : m.displayName }));
  const submit = () => {
    const minor = parseMoneyInput(amount, cur);
    if (!minor || minor <= 0) {
      setError('Type the amount paid, like 24,50.');
      return;
    }
    if (from === to) {
      setError('Pick two different people.');
      return;
    }
    if (from !== me?.id && to !== me?.id) {
      setError('You can record a payment you made or received.');
      return;
    }
    settle.mutate(
      {
        id: detail.group.id,
        body: { from, to, amountMinor: minor, settledOn, ...(note.trim() ? { note: note.trim() } : {}) },
      },
      {
        onSuccess: () => {
          toast(`Recorded ${money(minor, cur)}`);
          onClose();
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
      title="Settle up"
      sub={detail.group.name}
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={settle.isPending} onClick={submit}>
            Record payment
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
        <div className="form-row">
          <label className="st-col">
            Paid by
            <Select value={from} options={options} onChange={setFrom} />
          </label>
          <label className="st-col">
            To
            <Select value={to} options={options} onChange={setTo} />
          </label>
        </div>
        <div className="form-row">
          <label className="st-col">
            Amount ({cur})
            <TextInput
              mono
              inputMode="decimal"
              autoFocus
              value={amount}
              onChange={(e) => {
                setAmount(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Date
            <TextInput
              type="date"
              value={settledOn}
              onChange={(e) => {
                setSettledOn(e.target.value);
              }}
            />
          </label>
        </div>
        <label className="st-col">
          Note
          <TextInput
            maxLength={500}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />
        </label>
        <p className="hint">Record the payment once the money has moved; Finuance doesn't send money.</p>
        {error && (
          <p className="set-err" role="alert">
            {error}
          </p>
        )}
      </form>
    </Modal>
  );
}

type ShareProps = {
  /** a personal group to share (its transactions are copied), or none for an empty group */
  from?: { id: string; name: string; color: number; currency: string };
  onClose: () => void;
};

/** Share… a personal group, or start an empty shared group, and invite friends. */
export function ShareDialog({ from, onClose }: ShareProps) {
  const navigate = useNavigate();
  const friends = useFriends().data ?? [];
  const create = useCreateShared();
  const invite = useInvite();
  const [name, setName] = useState(from?.name ?? '');
  const [kind, setKind] = useState<SharedKind>('trip');
  const [picked, setPicked] = useState<string[]>([]);
  const [error, setError] = useState('');
  const pending = create.isPending || invite.isPending;
  const submit = () => {
    if (!name.trim()) {
      setError('Give the group a name.');
      return;
    }
    create.mutate(
      {
        name: name.trim(),
        kind,
        ...(from ? { fromGroupId: from.id, color: from.color, currency: from.currency } : {}),
      },
      {
        onSuccess: (g) => {
          void inviteAll(g);
        },
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };
  const inviteAll = async (g: SharedGroup) => {
    let invited = 0;
    for (const friendId of picked) {
      try {
        await invite.mutateAsync({ id: g.id, friendId });
        invited++;
      } catch (e) {
        toast(errorMessage(e), { kind: 'error' });
      }
    }
    toast(invited ? `Shared ${g.name} with ${String(invited)} friend${invited === 1 ? '' : 's'}` : `Created ${g.name}`);
    onClose();
    void navigate(`/s/${g.id}`);
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={from ? `Share ${from.name}` : 'New shared group'}
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={pending} onClick={submit}>
            {from ? 'Share' : 'Create'}
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
        {from && (
          <p className="hint">
            Friends you invite see a copy of each transaction in this group — date, amount, payee, category and note —
            never your accounts or the bank's description. New transactions you put in the group are shared too.
          </p>
        )}
        <div className="form-row">
          <label className="st-col">
            Name
            <TextInput
              maxLength={60}
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Kind
            <Select value={kind} options={KINDS} onChange={setKind} />
          </label>
        </div>
        <fieldset className="split-ed">
          <legend>Invite friends</legend>
          {friends.length ? (
            <ul className="split-rows">
              {friends.map((f) => (
                <li key={f.id}>
                  <Checkbox
                    label={f.displayName}
                    checked={picked.includes(f.id)}
                    onChange={(c) => {
                      setPicked((p) => (c ? [...p, f.id] : p.filter((x) => x !== f.id)));
                    }}
                  />
                </li>
              ))}
            </ul>
          ) : (
            <p className="hint">No friends yet: add them in Settings → Friends, then invite them from the group.</p>
          )}
        </fieldset>
        {error && (
          <p className="set-err" role="alert">
            {error}
          </p>
        )}
      </form>
    </Modal>
  );
}

/** The owner's settings: name, kind, dates and budget. */
export function GroupSettingsDialog({ group, onClose }: { group: SharedGroup; onClose: () => void }) {
  const update = useUpdateShared();
  const [name, setName] = useState(group.name);
  const [kind, setKind] = useState<SharedKind>(group.kind);
  const [startsOn, setStartsOn] = useState(group.startsOn ?? '');
  const [endsOn, setEndsOn] = useState(group.endsOn ?? '');
  const [budget, setBudget] = useState(group.budgetMinor ? moneyInputText(group.budgetMinor, group.currency) : '');
  const [error, setError] = useState('');
  const submit = () => {
    const b = budget.trim() ? parseMoneyInput(budget, group.currency) : null;
    if (budget.trim() && (!b || b <= 0)) {
      setError('Type the budget as an amount, like 3000, or leave it empty.');
      return;
    }
    update.mutate(
      {
        id: group.id,
        body: {
          name: name.trim(),
          kind,
          color: group.color,
          archived: group.archived,
          ...(startsOn ? { startsOn } : {}),
          ...(endsOn ? { endsOn } : {}),
          ...(b ? { budgetMinor: b } : {}),
        },
      },
      {
        onSuccess: () => {
          toast('Saved');
          onClose();
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
      title="Group settings"
      sub={group.name}
      size="sm"
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={update.isPending} onClick={submit}>
            Save
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
        <div className="form-row">
          <label className="st-col">
            Name
            <TextInput
              maxLength={60}
              value={name}
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Kind
            <Select value={kind} options={KINDS} onChange={setKind} />
          </label>
        </div>
        <div className="form-row">
          <label className="st-col">
            From
            <TextInput
              type="date"
              value={startsOn}
              onChange={(e) => {
                setStartsOn(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            To
            <TextInput
              type="date"
              value={endsOn}
              onChange={(e) => {
                setEndsOn(e.target.value);
              }}
            />
          </label>
        </div>
        <label className="st-col">
          Budget ({group.currency})
          <TextInput
            mono
            inputMode="decimal"
            placeholder="none"
            value={budget}
            onChange={(e) => {
              setBudget(e.target.value);
            }}
          />
        </label>
        {error && (
          <p className="set-err" role="alert">
            {error}
          </p>
        )}
      </form>
    </Modal>
  );
}
