// Administration → Users: every account with counts of its data (never the data itself), and the
// actions an administrator has: create, role, disable, reset password, sign out everywhere, limits
// and delete. Each action is recorded in the administrator's Activity and the account's own.

import { useState } from 'react';

import {
  useAdminUser,
  useAdminUsers,
  useCreateUser,
  useDeleteUser,
  useResetPassword,
  useRevokeUserSessions,
  useUpdateUser,
  type AdminUser,
  type LimitOverrides,
} from '../api/hooks/admin';
import { MIN_PASSWORD } from '../auth/Signup';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtBytes, fmtInt } from '../lib/format';
import { Button } from '../ui/Button';
import { Checkbox, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

function status(u: AdminUser): string {
  if (u.disabled) return 'disabled';
  if (u.mustChangePassword) return 'one-time password';
  return 'active';
}

export function UsersAdmin() {
  const [search, setSearch] = useState('');
  const [cursors, setCursors] = useState<string[]>(['']);
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const page = useAdminUsers(search, cursors[cursors.length - 1] ?? '');
  const t = page.data?.totals;
  return (
    <>
      <div className="set-card">
        <div className="lbl">Accounts</div>
        {t && (
          <p className="hint">
            {fmtInt(t.users)} accounts · {fmtInt(t.admins)} administrators · {fmtInt(t.disabled)} disabled ·{' '}
            {fmtInt(t.active30)} signed in during the last 30 days
          </p>
        )}
        <div className="set-actions adm-toolbar">
          <TextInput
            type="search"
            placeholder="Search by e-mail"
            aria-label="Search by e-mail"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setCursors(['']);
            }}
          />
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setCreating(true);
            }}
          >
            Create account
          </Button>
        </div>
        {page.isError && <p className="set-err">{errorMessage(page.error)}</p>}
        <table className="adm-table">
          <thead>
            <tr>
              <th scope="col">Account</th>
              <th scope="col">Role</th>
              <th scope="col">Status</th>
              <th scope="col">Last sign-in</th>
              <th scope="col" className="num">
                Transactions
              </th>
              <th scope="col" className="num">
                Files
              </th>
            </tr>
          </thead>
          <tbody>
            {page.data?.users.map((u) => (
              <tr key={u.id} className={selected === u.id ? 'on' : undefined}>
                <td>
                  <button
                    type="button"
                    className="adm-link"
                    onClick={() => {
                      setSelected(selected === u.id ? null : u.id);
                    }}
                  >
                    {u.email}
                  </button>
                  {u.displayName && <span className="hint"> {u.displayName}</span>}
                </td>
                <td>{u.isAdmin ? 'Administrator' : u.isDemo ? 'Demo' : 'User'}</td>
                <td>{status(u)}</td>
                <td>{u.lastLoginAt ? fmtDateTime(u.lastLoginAt) : 'never'}</td>
                <td className="num">{fmtInt(u.usage.transactions)}</td>
                <td className="num">{fmtBytes(u.usage.storedBytes)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="set-actions">
          <Button
            size="sm"
            disabled={cursors.length < 2}
            onClick={() => {
              setCursors(cursors.slice(0, -1));
            }}
          >
            Newer
          </Button>
          <Button
            size="sm"
            disabled={!page.data?.next}
            onClick={() => {
              if (page.data?.next) setCursors([...cursors, page.data.next]);
            }}
          >
            Older
          </Button>
        </div>
      </div>
      {selected && (
        <UserDetail
          key={selected}
          id={selected}
          onGone={() => {
            setSelected(null);
          }}
        />
      )}
      <CreateUser
        open={creating}
        onClose={() => {
          setCreating(false);
        }}
        onCreated={setSelected}
      />
    </>
  );
}

/** A one-time password, shown once with a copy button. */
function OneTime({ password }: { password: string }) {
  return (
    <div className="adm-onetime" role="status">
      <p>
        Give this one-time password to the person; they choose their own at the first sign-in. It isn't shown again.
      </p>
      <code className="mono">{password}</code>
      <Button
        size="sm"
        onClick={() => {
          void navigator.clipboard.writeText(password).then(() => {
            toast('Copied the one-time password');
          });
        }}
      >
        Copy
      </Button>
    </div>
  );
}

function CreateUser({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const create = useCreateUser();
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [admin, setAdmin] = useState(false);
  const [oneTime, setOneTime] = useState('');
  const [error, setError] = useState('');
  const close = () => {
    setEmail('');
    setName('');
    setPassword('');
    setAdmin(false);
    setOneTime('');
    setError('');
    onClose();
  };
  const submit = () => {
    if (password && password.length < MIN_PASSWORD) {
      setError(`Use a password of at least ${MIN_PASSWORD} characters, or leave it empty for a one-time password.`);
      return;
    }
    setError('');
    create.mutate(
      { email: email.trim(), displayName: name.trim(), ...(password ? { password } : {}), admin },
      {
        onSuccess: (r) => {
          onCreated(r.user.id);
          toast(`Created the account ${r.user.email}`);
          if (r.oneTimePassword) setOneTime(r.oneTimePassword);
          else close();
        },
        onError: (e) => {
          setError(errorMessage(e));
        },
      },
    );
  };
  return (
    <Modal
      open={open}
      onClose={close}
      title="Create account"
      size="sm"
      actions={
        oneTime ? (
          <Button variant="primary" onClick={close}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" disabled={!email.trim() || create.isPending} onClick={submit}>
              Create account
            </Button>
          </>
        )
      }
    >
      {oneTime ? (
        <OneTime password={oneTime} />
      ) : (
        <form
          className="adm-form"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <label className="st-col">
            E-mail
            <TextInput
              type="email"
              value={email}
              onChange={(e) => {
                setEmail(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Display name
            <TextInput
              value={name}
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            Password
            <TextInput
              type="password"
              autoComplete="new-password"
              placeholder="Empty: a one-time password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
              }}
            />
          </label>
          <Checkbox label="Administrator" checked={admin} onChange={setAdmin} />
          <p className="set-err" role="alert">
            {error}
          </p>
        </form>
      )}
    </Modal>
  );
}

const LIMIT_FIELDS: { key: keyof LimitOverrides; label: string; hint: string; scale?: number }[] = [
  { key: 'maxAccounts', label: 'Accounts', hint: 'at most' },
  { key: 'maxGroups', label: 'Groups', hint: 'at most' },
  { key: 'maxSharedGroups', label: 'Shared groups', hint: 'owned, at most' },
  { key: 'importsPerDay', label: 'Imports', hint: 'per 24 hours' },
  { key: 'maxUploadBytes', label: 'Largest file', hint: 'MiB', scale: 1 << 20 },
  { key: 'assistantPerHour', label: 'Assistant questions', hint: 'per hour' },
  { key: 'assistantTokensPerDay', label: 'Assistant tokens', hint: 'per day, questions and digests' },
];

/** The limit overrides as text fields ('' = the instance default). */
function limitText(l: LimitOverrides): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of LIMIT_FIELDS) {
    const v = l[f.key];
    out[f.key] = v === undefined ? '' : String(f.scale ? Math.round((v / f.scale) * 100) / 100 : v);
  }
  return out;
}

/** Text fields back to overrides; an invalid entry names its field. */
export function parseLimits(text: Record<string, string>): LimitOverrides | string {
  const out: LimitOverrides = {};
  for (const f of LIMIT_FIELDS) {
    const raw = (text[f.key] ?? '').trim();
    if (!raw) continue;
    const n = Number(raw.replace(',', '.'));
    if (!Number.isFinite(n) || n <= 0) return `${f.label}: enter a positive number, or leave it empty for the default.`;
    out[f.key] = Math.round(f.scale ? n * f.scale : n);
  }
  return out;
}

export function LimitFields({
  text,
  onChange,
  placeholder,
}: {
  text: Record<string, string>;
  onChange: (t: Record<string, string>) => void;
  placeholder: string;
}) {
  return (
    <div className="adm-limits">
      {LIMIT_FIELDS.map((f) => (
        <label key={f.key} className="st-col">
          {f.label} <span className="hint">({f.hint})</span>
          <TextInput
            inputMode="decimal"
            placeholder={placeholder}
            value={text[f.key] ?? ''}
            onChange={(e) => {
              onChange({ ...text, [f.key]: e.target.value });
            }}
          />
        </label>
      ))}
    </div>
  );
}

function UserDetail({ id, onGone }: { id: string; onGone: () => void }) {
  const user = useAdminUser(id);
  const update = useUpdateUser();
  const reset = useResetPassword();
  const revoke = useRevokeUserSessions();
  const del = useDeleteUser();
  const [limits, setLimits] = useState<Record<string, string> | null>(null);
  const [oneTime, setOneTime] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [confirm, setConfirm] = useState('');
  const u = user.data;
  if (!u) return <div className="set-card">{user.isError ? errorMessage(user.error) : 'Loading…'}</div>;
  const text = limits ?? limitText(u.limits);
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };
  const patch = (body: Parameters<typeof update.mutate>[0]['body'], done: string) => {
    update.mutate(
      { id, body },
      {
        onSuccess: () => {
          toast(done);
        },
        onError: fail,
      },
    );
  };
  return (
    <div className="set-card" aria-label={`Account ${u.email}`}>
      <div className="lbl">{u.email}</div>
      <p className="hint">
        Created {fmtDateTime(u.createdAt)} · {fmtInt(u.usage.accounts)} accounts · {fmtInt(u.usage.transactions)}{' '}
        transactions · {fmtInt(u.usage.imports)} imports · {fmtBytes(u.usage.storedBytes)} of files ·{' '}
        {fmtInt(u.usage.chats)} chats · the assistant in 30 days: {fmtInt(u.usage.assistantRuns)} runs,{' '}
        {fmtInt(u.usage.assistantTokens)} tokens
        {u.usage.lastActivity ? ` · last activity ${fmtDateTime(u.usage.lastActivity)}` : ''}
      </p>
      <div className="set-actions adm-actions">
        {!u.isDemo && (
          <Button
            size="sm"
            onClick={() => {
              patch(
                { admin: !u.isAdmin },
                u.isAdmin ? `${u.email} is no longer an administrator` : `${u.email} is now an administrator`,
              );
            }}
          >
            {u.isAdmin ? 'Remove administrator' : 'Make administrator'}
          </Button>
        )}
        <Button
          size="sm"
          onClick={() => {
            patch(
              { disabled: !u.disabled },
              u.disabled ? `Enabled ${u.email}` : `Disabled ${u.email} and signed it out`,
            );
          }}
        >
          {u.disabled ? 'Enable account' : 'Disable account'}
        </Button>
        <Button
          size="sm"
          onClick={() => {
            reset.mutate(id, {
              onSuccess: (r) => {
                setOneTime(r.oneTimePassword);
              },
              onError: fail,
            });
          }}
        >
          Reset password
        </Button>
        <Button
          size="sm"
          onClick={() => {
            revoke.mutate(id, {
              onSuccess: (r) => {
                toast(`Signed ${u.email} out of ${fmtInt(r.revoked)} browsers`);
              },
              onError: fail,
            });
          }}
        >
          Sign out everywhere
        </Button>
        <Button
          size="sm"
          variant="danger"
          onClick={() => {
            setDeleting(true);
          }}
        >
          Delete account
        </Button>
      </div>
      {oneTime && <OneTime password={oneTime} />}
      <div className="lbl sub">Limits</div>
      <p className="hint">Empty fields use the instance default (Defaults and features).</p>
      <LimitFields text={text} onChange={setLimits} placeholder="default" />
      <div className="set-actions">
        <Button
          size="sm"
          variant="primary"
          disabled={limits === null || update.isPending}
          onClick={() => {
            const parsed = parseLimits(text);
            if (typeof parsed === 'string') {
              toast(parsed, { kind: 'error' });
              return;
            }
            update.mutate(
              { id, body: { limits: parsed } },
              {
                onSuccess: () => {
                  setLimits(null);
                  toast(`Saved the limits of ${u.email}`);
                },
                onError: fail,
              },
            );
          }}
        >
          Save limits
        </Button>
      </div>
      <Modal
        open={deleting}
        onClose={() => {
          setDeleting(false);
        }}
        title={`Delete ${u.email}?`}
        size="sm"
        actions={
          <>
            <Button
              onClick={() => {
                setDeleting(false);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={confirm.trim().toLowerCase() !== u.email.toLowerCase() || del.isPending}
              onClick={() => {
                del.mutate(
                  { id, confirmEmail: confirm },
                  {
                    onSuccess: () => {
                      toast(`Deleted ${u.email} and all of its data`);
                      onGone();
                    },
                    onError: fail,
                  },
                );
              }}
            >
              Delete account
            </Button>
          </>
        }
      >
        <p>
          Every account, import, transaction, chat and setting of this person is deleted. This can't be undone. Type the
          e-mail address to confirm.
        </p>
        <TextInput
          aria-label="E-mail address to confirm"
          value={confirm}
          onChange={(e) => {
            setConfirm(e.target.value);
          }}
        />
      </Modal>
    </div>
  );
}
