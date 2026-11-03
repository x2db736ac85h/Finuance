// "Choose a new password" (Phase 19, ADR 0025): after signing in with a one-time password from an
// administrator, nothing but this screen works until the user picks their own password (the API
// answers 403 password_change_required everywhere else).

import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { useChangePassword } from '../api/hooks/me';
import { keys } from '../api/keys';
import { errorMessage } from '../lib/errors';
import { useSignOut } from '../shell/useSignOut';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { AuthCard } from './AuthCard';
import { MIN_PASSWORD } from './Signup';

export function ChoosePassword() {
  const qc = useQueryClient();
  const change = useChangePassword();
  const signOut = useSignOut();
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
    if (next === current) {
      setError('Choose a password of your own, not the one-time password.');
      return;
    }
    setError('');
    change.mutate(
      { currentPassword: current, newPassword: next },
      {
        onSuccess: () => void qc.invalidateQueries({ queryKey: keys.me }),
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };

  return (
    <AuthCard
      title="Choose a new password"
      lead="You signed in with a one-time password from an administrator. Choose your own to continue."
      onSubmit={submit}
    >
      <input type="text" autoComplete="username" hidden readOnly />
      <TextInput
        type="password"
        placeholder="One-time password"
        aria-label="One-time password"
        autoComplete="current-password"
        value={current}
        onChange={(e) => {
          setCurrent(e.target.value);
        }}
      />
      <TextInput
        type="password"
        placeholder={`New password (${MIN_PASSWORD}+ characters)`}
        aria-label="New password"
        autoComplete="new-password"
        value={next}
        onChange={(e) => {
          setNext(e.target.value);
        }}
      />
      <TextInput
        type="password"
        placeholder="Repeat the new password"
        aria-label="Repeat the new password"
        autoComplete="new-password"
        value={repeat}
        onChange={(e) => {
          setRepeat(e.target.value);
        }}
      />
      <Button type="submit" variant="primary" disabled={!current || !next || change.isPending}>
        Save and continue
      </Button>
      <div className="lock-err" role="alert">
        {error}
      </div>
      <Button
        variant="ghost"
        onClick={() => {
          void signOut();
        }}
      >
        Sign out
      </Button>
    </AuthCard>
  );
}
