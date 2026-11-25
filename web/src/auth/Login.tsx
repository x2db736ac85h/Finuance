import { useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';

import { useLogin, useLoginSecondStep, useMeta, useStartDemo } from '../api/hooks/me';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { AuthCard } from './AuthCard';
import { safeNext } from './next';
import { txPath } from '../lib/paths';

export function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const meta = useMeta();
  const login = useLogin();
  const demo = useStartDemo();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  // two-step sign-in (ADR 0040): the password was right, and the server waits for a code
  const second = useLoginSecondStep();
  const [challenge, setChallenge] = useState('');
  const [code, setCode] = useState('');
  const done = () => void navigate(safeNext(params.get('next')), { replace: true });

  const submitCode = () => {
    if (!code.trim()) {
      setError('Enter the code from your authenticator app.');
      return;
    }
    setError('');
    second.mutate(
      { challenge, code: code.trim() },
      {
        onSuccess: done,
        onError: (e) => {
          setCode('');
          setError(errorMessage(e));
        },
      },
    );
  };

  const submit = () => {
    if (!email.trim() || !password) {
      setError('Enter your email and password.');
      return;
    }
    setError('');
    login.mutate(
      { email: email.trim(), password },
      {
        onSuccess: (r) => {
          if ('challenge' in r) {
            setChallenge(r.challenge);
            setPassword('');
            return;
          }
          done();
        },
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };

  const tryDemo = () => {
    setError('');
    demo.mutate(undefined, {
      onSuccess: () => void navigate(txPath(), { replace: true }),
      onError: (e) => {
        setError(errorMessage(e));
      },
    });
  };

  if (challenge)
    return (
      <AuthCard
        title="Enter your code"
        lead="Type the six-digit code from your authenticator app, or one of your recovery codes."
        onSubmit={submitCode}
      >
        <TextInput
          name="code"
          inputMode="numeric"
          placeholder="123456"
          aria-label="Code"
          autoComplete="one-time-code"
          autoFocus
          maxLength={32}
          value={code}
          onChange={(e) => {
            setCode(e.target.value);
          }}
        />
        <Button type="submit" variant="primary" disabled={second.isPending}>
          {second.isPending ? 'Checking…' : 'Sign in'}
        </Button>
        <div className="lock-err" role="alert">
          {error}
        </div>
        <p className="auth-alt">
          <button
            type="button"
            className="linklike"
            onClick={() => {
              setChallenge('');
              setCode('');
              setError('');
            }}
          >
            Start again with your password
          </button>
        </p>
      </AuthCard>
    );

  return (
    <AuthCard title="Sign in" lead="Sign in to see your accounts and transactions." onSubmit={submit}>
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
        type="password"
        name="password"
        placeholder="Password"
        aria-label="Password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => {
          setPassword(e.target.value);
        }}
      />
      <Button type="submit" variant="primary" disabled={login.isPending}>
        {login.isPending ? 'Signing in…' : 'Sign in'}
      </Button>
      <div className="lock-err" role="alert">
        {error}
      </div>
      {meta.data?.demoMode && (
        <div className="auth-demo">
          <Button type="button" onClick={tryDemo} disabled={demo.isPending}>
            {demo.isPending ? 'Preparing the demo…' : 'Try the demo'}
          </Button>
          <p className="auth-alt">A year of sample transactions, yours alone and deleted after 24 hours.</p>
        </div>
      )}
      {meta.data?.signupEnabled && (
        <p className="auth-alt">
          New here? <Link to="/signup">Create an account</Link>
        </p>
      )}
    </AuthCard>
  );
}
