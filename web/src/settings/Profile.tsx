import { useState } from 'react';

import { useChangePassword, useMe, useUpdateMe } from '../api/hooks/me';
import { MIN_PASSWORD } from '../auth/Signup';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { toast } from '../ui/toastStore';

export function ProfileSettings() {
  return (
    <>
      <NameForm />
      <PasswordForm />
    </>
  );
}

function NameForm() {
  const me = useMe();
  const update = useUpdateMe();
  const [name, setName] = useState(me.data?.displayName ?? '');
  const [error, setError] = useState('');
  const unchanged = name.trim() === (me.data?.displayName ?? '');
  return (
    <form
      className="set-card"
      onSubmit={(e) => {
        e.preventDefault();
        setError('');
        update.mutate(name.trim(), {
          onSuccess: () => {
            toast('Saved your name');
          },
          onError: (err) => {
            setError(errorMessage(err));
          },
        });
      }}
    >
      <div className="lbl">Profile</div>
      <label className="st-col">
        Email
        <TextInput value={me.data?.email ?? ''} readOnly disabled />
      </label>
      <label className="st-col">
        Name
        <TextInput
          value={name}
          maxLength={80}
          autoComplete="name"
          placeholder="How Finuance greets you"
          onChange={(e) => {
            setName(e.target.value);
          }}
        />
      </label>
      <div className="set-actions">
        <span className="set-err" role="alert">
          {error}
        </span>
        <Button type="submit" variant="primary" size="sm" disabled={unchanged || update.isPending}>
          Save name
        </Button>
      </div>
    </form>
  );
}

function PasswordForm() {
  const change = useChangePassword();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [error, setError] = useState('');
  const submit = () => {
    if (next.length < MIN_PASSWORD) {
      setError(`Use a new password of at least ${MIN_PASSWORD} characters.`);
      return;
    }
    if (next !== repeat) {
      setError("The new passwords don't match. Type the same password twice.");
      return;
    }
    setError('');
    change.mutate(
      { currentPassword: current, newPassword: next },
      {
        onSuccess: () => {
          setCurrent('');
          setNext('');
          setRepeat('');
          toast('Changed your password. Your other sessions are signed out.');
        },
        onError: (err) => {
          setError(errorMessage(err));
        },
      },
    );
  };
  return (
    <form
      className="set-card"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="lbl">Password</div>
      {/* lets password managers pair the new password with the account */}
      <input type="text" autoComplete="username" hidden readOnly />
      <label className="st-col">
        Current password
        <TextInput
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => {
            setCurrent(e.target.value);
          }}
        />
      </label>
      <label className="st-col">
        New password
        <TextInput
          type="password"
          autoComplete="new-password"
          placeholder={`${MIN_PASSWORD}+ characters`}
          value={next}
          onChange={(e) => {
            setNext(e.target.value);
          }}
        />
      </label>
      <label className="st-col">
        Repeat the new password
        <TextInput
          type="password"
          autoComplete="new-password"
          value={repeat}
          onChange={(e) => {
            setRepeat(e.target.value);
          }}
        />
      </label>
      <p className="hint">Changing the password signs out every other browser and device.</p>
      <div className="set-actions">
        <span className="set-err" role="alert">
          {error}
        </span>
        <Button type="submit" variant="primary" size="sm" disabled={!current || !next || change.isPending}>
          Change password
        </Button>
      </div>
    </form>
  );
}
