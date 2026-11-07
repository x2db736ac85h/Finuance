// Settings → Access tokens (ADR 0036): personal access tokens for the fin CLI and scripts. A token
// is shown once, right after it is made; afterwards only its first characters.

import { useState } from 'react';

import { useAccessTokens, useCreateAccessToken, useRevokeAccessToken, type AccessToken } from '../api/hooks/tokens';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Select, TextInput, type Option } from '../ui/inputs';
import { toast } from '../ui/toastStore';

const LIFETIMES: Option<string>[] = [
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '365', label: 'A year' },
];
const SCOPES: Option<'read' | 'write'>[] = [
  { value: 'read', label: 'Read only' },
  { value: 'write', label: 'Read and change data' },
];

function state(t: AccessToken, now: number): string {
  if (t.revokedAt) return `revoked ${fmtDateTime(t.revokedAt)}`;
  if (Date.parse(t.expiresAt) <= now) return `expired ${fmtDateTime(t.expiresAt)}`;
  return `expires ${fmtDateTime(t.expiresAt)}`;
}

export function TokensSettings() {
  const tokens = useAccessTokens();
  const create = useCreateAccessToken();
  const revoke = useRevokeAccessToken();
  const [name, setName] = useState('');
  const [scope, setScope] = useState<'read' | 'write'>('read');
  const [days, setDays] = useState('90');
  const [secret, setSecret] = useState<string | null>(null);
  const [now] = useState(() => Date.now()); // for which tokens are still live (read once)
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };
  return (
    <>
      <form
        className="set-card"
        aria-label="New access token"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { name: name.trim(), scope, expiresInDays: Number(days) },
            {
              onSuccess: (r) => {
                setSecret(r.secret);
                setName('');
              },
              onError: fail,
            },
          );
        }}
      >
        <div className="lbl">Access tokens</div>
        <p className="hint">
          For the <code>fin</code> command line and your own scripts: send a token as{' '}
          <code>Authorization: Bearer fin_…</code>. A read-only token can look things up; neither kind can sign in,
          change the password, or reach Administration. A new password ends every token.
        </p>
        <div className="form-row">
          <label className="st-col">
            Name
            <TextInput
              value={name}
              maxLength={60}
              placeholder="laptop CLI"
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Allows
            <Select value={scope} options={SCOPES} onChange={setScope} />
          </label>
          <label className="st-col">
            Valid for
            <Select value={days} options={LIFETIMES} onChange={setDays} />
          </label>
          <Button type="submit" variant="primary" disabled={!name.trim() || create.isPending}>
            Make a token
          </Button>
        </div>
        {secret && (
          <div className="conn-err" role="status">
            Copy it now; it isn't shown again:{' '}
            <code className="token-secret" aria-label="The new token">
              {secret}
            </code>{' '}
            <Button
              size="sm"
              onClick={() => {
                void navigator.clipboard.writeText(secret).then(() => {
                  toast('Copied the token');
                });
              }}
            >
              Copy
            </Button>
          </div>
        )}
      </form>
      <div className="set-card">
        <div className="lbl">Your tokens</div>
        {tokens.isError && <p className="set-err">{errorMessage(tokens.error)}</p>}
        {tokens.data?.length === 0 && <p className="hint">None yet.</p>}
        <ul className="set-list">
          {tokens.data?.map((t) => (
            <li key={t.id}>
              <div className="set-li-main">
                <b>{t.name}</b>
                <span className="tag src">{t.scope === 'write' ? 'read and write' : 'read only'}</span>
                <span className="hint">
                  <code>{t.prefix}…</code> · {state(t, now)}
                  {t.lastUsedAt ? ` · last used ${fmtDateTime(t.lastUsedAt)}` : ' · never used'}
                </span>
              </div>
              {!t.revokedAt && Date.parse(t.expiresAt) > now && (
                <Button
                  size="sm"
                  disabled={revoke.isPending}
                  onClick={() => {
                    revoke.mutate(t.id, {
                      onSuccess: () => {
                        toast(`Revoked ${t.name}`);
                      },
                      onError: fail,
                    });
                  }}
                >
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      </div>
    </>
  );
}
