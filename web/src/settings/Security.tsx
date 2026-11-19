// Settings → Security (ADR 0040): two-step sign-in. After the password, signing in asks for a code
// from an authenticator app. Setting it up shows the secret once (as a QR code and as text) and
// then ten recovery codes, once.

import { useState } from 'react';

import {
  useConfirmSecondFactor,
  useDisableSecondFactor,
  useNewRecoveryCodes,
  useSecondFactor,
  useStartSecondFactor,
} from '../api/hooks/secondFactor';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { toast } from '../ui/toastStore';
import { QrCode } from './QrCode';

/** The secret in groups of four, as authenticator apps show it. */
const grouped = (secret: string) => secret.replace(/(.{4})(?=.)/g, '$1 ');

function CodeInput({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  return (
    <TextInput
      value={value}
      inputMode="numeric"
      autoComplete="one-time-code"
      maxLength={32}
      placeholder="123456"
      aria-label={label}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    />
  );
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const text = codes.join('\n');
  return (
    <div className="set-card" role="status" aria-label="Recovery codes">
      <div className="lbl">Your recovery codes</div>
      <p className="hint">
        Each code signs you in once if you don't have your phone. They aren't shown again: copy or print them now and
        keep them somewhere other than your phone.
      </p>
      <ul className="recovery-codes">
        {codes.map((c) => (
          <li key={c}>
            <code>{c}</code>
          </li>
        ))}
      </ul>
      <div className="set-actions">
        <Button
          onClick={() => {
            void navigator.clipboard.writeText(text).then(() => {
              toast('Copied the recovery codes');
            });
          }}
        >
          Copy
        </Button>
        <Button variant="primary" onClick={onDone}>
          I have saved them
        </Button>
      </div>
    </div>
  );
}

export function SecuritySettings() {
  const factor = useSecondFactor();
  const start = useStartSecondFactor();
  const confirm = useConfirmSecondFactor();
  const fresh = useNewRecoveryCodes();
  const disable = useDisableSecondFactor();
  const [enrolment, setEnrolment] = useState<{ secret: string; uri: string } | null>(null);
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState<string[] | null>(null);
  const [mode, setMode] = useState<'none' | 'codes' | 'off'>('none');
  const [password, setPassword] = useState('');
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };
  const reset = () => {
    setCode('');
    setPassword('');
    setMode('none');
  };

  if (!factor.data) return <div className="set-card">{factor.isError ? errorMessage(factor.error) : 'Loading…'}</div>;
  const f = factor.data;

  if (codes)
    return (
      <RecoveryCodes
        codes={codes}
        onDone={() => {
          setCodes(null);
        }}
      />
    );

  if (!f.enabled) {
    return (
      <div className="set-card">
        <div className="lbl">Two-step sign-in</div>
        <p className="hint">
          After your password, signing in asks for a six-digit code from an authenticator app on your phone (any app for
          one-time codes works). Someone who learns your password still can't sign in.
        </p>
        {f.requiredForAdmins && (
          <p className="set-err" role="status">
            This instance requires two-step sign-in for administrators. Administration opens once you have set it up.
          </p>
        )}
        {!f.available && (
          <p className="hint">
            It can't be set up here: demo accounts don't have it, and an instance needs its instance key (
            <code>FINUANCE_INSTANCE_KEY</code>) to keep the secret.
          </p>
        )}
        {f.available && !enrolment && (
          <div className="set-actions">
            <Button
              variant="primary"
              disabled={start.isPending}
              onClick={() => {
                start.mutate(undefined, { onSuccess: setEnrolment, onError: fail });
              }}
            >
              Set up two-step sign-in
            </Button>
          </div>
        )}
        {enrolment && (
          <form
            aria-label="Set up two-step sign-in"
            onSubmit={(e) => {
              e.preventDefault();
              confirm.mutate(code, {
                onSuccess: (r) => {
                  setEnrolment(null);
                  setCode('');
                  setCodes(r.recoveryCodes);
                },
                onError: fail,
              });
            }}
          >
            <p>
              <b>1.</b> Scan this with your authenticator app, or type the key into it.
            </p>
            <div className="qr-row">
              <QrCode text={enrolment.uri} label="QR code for your authenticator app" />
              <div>
                <div className="hint">The key (time-based, six digits):</div>
                <code className="token-secret" aria-label="The key for your authenticator app">
                  {grouped(enrolment.secret)}
                </code>{' '}
                <Button
                  size="sm"
                  type="button"
                  onClick={() => {
                    void navigator.clipboard.writeText(enrolment.secret).then(() => {
                      toast('Copied the key');
                    });
                  }}
                >
                  Copy
                </Button>
                <p className="hint">
                  On this device? <a href={enrolment.uri}>Open it in your authenticator app</a>.
                </p>
              </div>
            </div>
            <p>
              <b>2.</b> Type the code the app shows now.
            </p>
            <div className="form-row">
              <CodeInput value={code} onChange={setCode} label="Code from your authenticator app" />
              <Button type="submit" variant="primary" disabled={code.trim().length < 6 || confirm.isPending}>
                Switch it on
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setEnrolment(null);
                  setCode('');
                }}
              >
                Cancel
              </Button>
            </div>
            <p className="hint">Nothing changes for signing in until the code is accepted.</p>
          </form>
        )}
      </div>
    );
  }

  return (
    <div className="set-card">
      <div className="lbl">Two-step sign-in</div>
      <p>
        <span className="tag src">on</span>{' '}
        <span className="hint">
          {f.confirmedAt ? `since ${fmtDateTime(f.confirmedAt)} · ` : ''}
          {f.recoveryCodes === 1 ? '1 recovery code left' : `${String(f.recoveryCodes)} recovery codes left`}
        </span>
      </p>
      <p className="hint">
        Signing in asks for a code from your authenticator app after the password. Without your phone, a recovery code
        works once; without both, an administrator can remove two-step sign-in for you.
      </p>
      {mode === 'none' && (
        <div className="set-actions">
          <Button
            onClick={() => {
              setMode('codes');
            }}
          >
            New recovery codes
          </Button>
          <Button
            variant="danger"
            onClick={() => {
              setMode('off');
            }}
          >
            Switch it off
          </Button>
        </div>
      )}
      {mode === 'codes' && (
        <form
          aria-label="New recovery codes"
          onSubmit={(e) => {
            e.preventDefault();
            fresh.mutate(code, {
              onSuccess: (r) => {
                reset();
                setCodes(r.recoveryCodes);
              },
              onError: fail,
            });
          }}
        >
          <p className="hint">The codes you have now stop working. Type the code your authenticator app shows.</p>
          <div className="form-row">
            <CodeInput value={code} onChange={setCode} label="Code from your authenticator app" />
            <Button type="submit" variant="primary" disabled={code.trim().length < 6 || fresh.isPending}>
              Make new codes
            </Button>
            <Button type="button" onClick={reset}>
              Cancel
            </Button>
          </div>
        </form>
      )}
      {mode === 'off' && (
        <form
          aria-label="Switch two-step sign-in off"
          onSubmit={(e) => {
            e.preventDefault();
            disable.mutate(
              { password, code },
              {
                onSuccess: () => {
                  reset();
                  toast('Two-step sign-in is off');
                },
                onError: fail,
              },
            );
          }}
        >
          <p className="hint">Your password, and a code from the app or a recovery code.</p>
          <div className="form-row">
            <TextInput
              type="password"
              autoComplete="current-password"
              placeholder="Password"
              aria-label="Password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
              }}
            />
            <CodeInput value={code} onChange={setCode} label="Code or recovery code" />
            <Button type="submit" variant="danger" disabled={!password || code.trim().length < 6 || disable.isPending}>
              Switch it off
            </Button>
            <Button type="button" onClick={reset}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
