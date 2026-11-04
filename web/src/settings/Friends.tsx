// Settings → Friends (Phase 19, ADR 0027): show your code to someone, enter theirs, and the list.
// A code is 8 characters, works once, and only until the end of this clock hour plus 10 minutes;
// the server keeps only its hash, so it is shown here once (in memory) and a new one replaces it.
// Friends see your display name, never your e-mail address.

import { useEffect, useState } from 'react';
import { Link } from 'react-router';

import {
  useAddFriend,
  useFriends,
  useNewFriendCode,
  useRemoveFriend,
  type Friend,
  type FriendCode,
} from '../api/hooks/friends';
import { useMe } from '../api/hooks/me';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

export function FriendsSettings() {
  const me = useMe().data;
  if (me && !me.displayName.trim())
    return (
      <div className="set-card">
        <div className="lbl">A display name first</div>
        <p className="hint">
          Friends see each other by display name, never by e-mail address.{' '}
          <Link to="/settings/profile">Set yours in Profile</Link>, then come back.
        </p>
      </div>
    );
  return (
    <>
      <MyCode />
      <AddFriend />
      <FriendList />
    </>
  );
}

/** Minutes and seconds until the instant, refreshed every second; "" once it has passed. */
function useCountdown(until: string | undefined): string {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!until) return;
    const t = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      clearInterval(t);
    };
  }, [until]);
  if (!until) return '';
  const left = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));
  if (!left) return '';
  return `${String(Math.floor(left / 60))}:${String(left % 60).padStart(2, '0')}`;
}

function MyCode() {
  const make = useNewFriendCode();
  const [code, setCode] = useState<FriendCode | null>(null);
  const left = useCountdown(code?.expiresAt);
  const expired = code !== null && !left;
  const show = () => {
    make.mutate(undefined, {
      onSuccess: setCode,
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  return (
    <div className="set-card">
      <div className="lbl">Your code</div>
      <p className="hint">
        Give it to someone you want as a friend. It works once, until the end of this hour plus 10 minutes. A new code
        replaces the previous one.
      </p>
      {code && !expired ? (
        <div className="fr-code">
          <code aria-label="Your friend code">{code.code}</code>
          <span className="hint" role="timer" aria-live="off">
            {left} left
          </span>
          <Button
            icon="copy"
            onClick={() => {
              void navigator.clipboard.writeText(code.code).then(
                () => {
                  toast('Copied the code');
                },
                () => {
                  toast('Copying failed; select the code instead', { kind: 'error' });
                },
              );
            }}
          >
            Copy
          </Button>
        </div>
      ) : (
        <div className="set-actions">
          <Button variant="primary" disabled={make.isPending} onClick={show}>
            {expired ? 'Show a new code' : 'Show my code'}
          </Button>
        </div>
      )}
    </div>
  );
}

function AddFriend() {
  const add = useAddFriend();
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  return (
    <form
      className="set-card"
      onSubmit={(e) => {
        e.preventDefault();
        setError('');
        add.mutate(code, {
          onSuccess: (f) => {
            setCode('');
            toast(`You and ${f.displayName} are friends now`);
          },
          onError: (err) => {
            setError(errorMessage(err));
          },
        });
      }}
    >
      <div className="lbl">Add a friend</div>
      <label className="st-col">
        Their code
        <TextInput
          mono
          autoComplete="off"
          placeholder="K7Q4-9MZD"
          maxLength={20}
          value={code}
          onChange={(e) => {
            setCode(e.target.value);
          }}
        />
      </label>
      <p className="set-err" role="alert">
        {error}
      </p>
      <div className="set-actions">
        <Button variant="primary" type="submit" disabled={!code.trim() || add.isPending}>
          Add friend
        </Button>
      </div>
    </form>
  );
}

function FriendList() {
  const friends = useFriends();
  const [removing, setRemoving] = useState<Friend | null>(null);
  const remove = useRemoveFriend();
  const list = friends.data ?? [];
  return (
    <div className="set-card">
      <div className="lbl">Friends</div>
      {friends.isError ? (
        <p className="set-err">{errorMessage(friends.error)}</p>
      ) : !list.length ? (
        <p className="hint">{friends.isLoading ? 'Loading…' : 'No friends yet.'}</p>
      ) : (
        <ul className="set-list" aria-label="Friends">
          {list.map((f) => (
            <li key={f.id}>
              <span className="fr-initial" aria-hidden="true">
                {(f.displayName.trim()[0] ?? '?').toUpperCase()}
              </span>
              <span className="set-li-main">
                <b>{f.displayName}</b>
                <span className="hint">Friends since {fmtDateTime(f.since)}</span>
              </span>
              <Button
                onClick={() => {
                  setRemoving(f);
                }}
              >
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Modal
        open={removing !== null}
        onClose={() => {
          setRemoving(null);
        }}
        title={`Remove ${removing?.displayName ?? ''}?`}
        size="sm"
        actions={
          <>
            <Button
              onClick={() => {
                setRemoving(null);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="primary"
              disabled={remove.isPending}
              onClick={() => {
                if (!removing) return;
                const name = removing.displayName;
                remove.mutate(removing.id, {
                  onSuccess: () => {
                    setRemoving(null);
                    toast(`Removed ${name}`);
                  },
                  onError: (e) => {
                    toast(errorMessage(e), { kind: 'error' });
                  },
                });
              }}
            >
              Remove
            </Button>
          </>
        }
      >
        <p>They'll get a notification. To be friends again, one of you shows a new code.</p>
      </Modal>
    </div>
  );
}
