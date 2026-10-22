import { useState } from 'react';
import { Link, useNavigate } from 'react-router';

import { useMeta, useSignup } from '../api/hooks/me';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { AuthCard } from './AuthCard';

/** The server's minimum (PLAN §6.1); checked here too so the message comes before the request. */
export const MIN_PASSWORD = 12;

export function Signup() {
  const navigate = useNavigate();
  const meta = useMeta();
  const signup = useSignup();
  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  if (meta.data && !meta.data.signupEnabled) {
    return (
      <AuthCard
        title="Sign-up is turned off"
        lead="This Finuance server doesn't accept new accounts. Ask its administrator to create one for you."
      >
        <p className="auth-alt">
          <Link to="/login">Back to sign in</Link>
        </p>
      </AuthCard>
    );
  }

  const submit = () => {
    if (!email.trim()) {
      setError('Enter your email address.');
      return;
    }
    if (password.length < MIN_PASSWORD) {
      setError(`Use a password of at least ${MIN_PASSWORD} characters.`);
      return;
    }
    setError('');
    const name = displayName.trim();
    signup.mutate(
      { email: email.trim(), password, ...(name ? { displayName: name } : {}) },
      {
        onSuccess: () => void navigate('/', { replace: true }),
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };

  return (
    <AuthCard title="Create an account" lead="Your transactions stay on this Finuance server." onSubmit={submit}>
      <TextInput
        type="email"
        name="email"
        placeholder="Email"
        aria-label="Email"
        autoComplete="username"
        autoFocus
        value={email}
        onChange={(e) => {
          setEmail(e.target.value);
        }}
      />
      <TextInput
        name="displayName"
        placeholder="Name (optional)"
        aria-label="Name"
        autoComplete="name"
        value={displayName}
        onChange={(e) => {
          setDisplayName(e.target.value);
        }}
      />
      <TextInput
        type="password"
        name="password"
        placeholder={`Password (${MIN_PASSWORD}+ characters)`}
        aria-label="Password"
        autoComplete="new-password"
        value={password}
        onChange={(e) => {
          setPassword(e.target.value);
        }}
      />
      <Button type="submit" variant="primary" disabled={signup.isPending}>
        {signup.isPending ? 'Creating account…' : 'Create account'}
      </Button>
      <div className="lock-err" role="alert">
        {error}
      </div>
      <p className="auth-alt">
        Already have an account? <Link to="/login">Sign in</Link>
      </p>
    </AuthCard>
  );
}
