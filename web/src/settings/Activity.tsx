// Settings → Activity (PLAN 17.6): the changes the assistant made, each with Undo (ADR 0030), and
// the account's activity log, newest first, with the address and browser of each action. The
// server keeps the log for a year; nothing here can change it.

import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useActivity, type ActivityAction, type ActivityEvent } from '../api/hooks/activity';
import {
  openChangesIn,
  useAIChanges,
  useAllAIChanges,
  useRevertAIChange,
  useRevertAIChanges,
  type AIChange,
} from '../api/hooks/assistant';
import { useViews } from '../stores/viewStore';
import { toast } from '../ui/toastStore';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { txPath } from '../lib/paths';

type Meta = NonNullable<ActivityEvent['meta']>;

const str = (m: Meta, k: string) => (typeof m[k] === 'string' ? m[k] : '');
const int = (m: Meta, k: string) => (typeof m[k] === 'number' ? m[k] : 0);

const PROPOSAL: Record<string, string> = {
  set_category: 'set a category',
  set_group: 'set a group',
  set_transfer: 'marked transfers',
  create_rule: 'created a rule',
};

/** What happened, in words. Every action of the API has an entry (the compiler checks). */
const DESCRIBE: Record<ActivityAction, (m: Meta) => string> = {
  'auth.login': () => 'Signed in',
  'auth.login_failed': () => 'Failed sign-in: wrong password',
  'auth.logout': () => 'Signed out',
  'auth.password_changed': () => 'Changed the password (other browsers were signed out)',
  'auth.session_revoked': () => 'Signed out another browser',
  'import.committed': (m) =>
    `Imported ${str(m, 'file') || 'a file'}: ${plural(int(m, 'new'), 'new transaction')}` +
    (int(m, 'duplicates') ? `, ${fmtInt(int(m, 'duplicates'))} already there` : ''),
  'import.deleted': (m) =>
    `Deleted the import ${str(m, 'file')}` +
    (int(m, 'transactionsRemoved') ? ` (${plural(int(m, 'transactionsRemoved'), 'transaction')} removed)` : ''),
  'account.deleted': (m) => `Deleted the account ${str(m, 'name')}`,
  'export.csv': (m) => `Exported transactions to ${str(m, 'file') || 'CSV'}`,
  'export.data': () => 'Downloaded all data',
  'rules.applied': () => 'Applied the rules to every transaction',
  'legacy.import_started': (m) => `Started a migration from the old app (${plural(int(m, 'files'), 'file')})`,
  'duplicates.decided': (m) =>
    str(m, 'mode') === 'keep_both' ? 'Kept both versions of a duplicate' : 'Chose a version of a duplicate',
  'duplicates.reset': () => 'Merged a duplicate again',
  'assistant.action_applied': (m) =>
    `Applied an assistant proposal: ${PROPOSAL[str(m, 'action')] ?? 'a change'}` +
    (str(m, 'action') === 'create_rule' ? '' : ` for ${plural(int(m, 'rows'), 'transaction')}`),
  'assistant.change_reverted': (m) =>
    `Undid an assistant change: ${PROPOSAL[str(m, 'action')] ?? 'a change'}` +
    (int(m, 'rows') > 0 ? ` for ${plural(int(m, 'rows'), 'transaction')}` : ''),
  // Administrators' actions: the administrator's own log names the account (`email`); the
  // account's log only says an administrator did it (`by`).
  'admin.user_created': (m) =>
    byAdmin(m) ? 'An administrator created this account' : `Created the account ${str(m, 'email')}`,
  'admin.role_changed': (m) =>
    byAdmin(m)
      ? 'An administrator changed your role'
      : `${m['admin'] === true ? 'Made' : 'Removed'} ${str(m, 'email')} ${m['admin'] === true ? 'an administrator' : 'as administrator'}`,
  'admin.user_disabled': (m) =>
    byAdmin(m) ? 'An administrator disabled this account' : `Disabled the account ${str(m, 'email')}`,
  'admin.user_enabled': (m) =>
    byAdmin(m) ? 'An administrator enabled this account again' : `Enabled the account ${str(m, 'email')}`,
  'admin.password_reset': (m) =>
    byAdmin(m)
      ? 'An administrator reset your password (all browsers were signed out)'
      : `Reset the password of ${str(m, 'email')}`,
  'admin.sessions_revoked': (m) =>
    byAdmin(m) ? 'An administrator signed you out everywhere' : `Signed ${str(m, 'email')} out everywhere`,
  'admin.limits_changed': (m) =>
    byAdmin(m) ? 'An administrator changed your limits' : `Changed the limits of ${str(m, 'email')}`,
  'admin.user_deleted': (m) => `Deleted the account ${str(m, 'email')} and all of its data`,
  'admin.settings_changed': () => 'Changed the instance settings',
  'admin.theme_approved': (m) => `Approved the theme ${str(m, 'theme')}`,
  'admin.theme_retired': (m) => `Retired the theme ${str(m, 'theme')}`,
  'admin.advice_changed': (m) =>
    typeof m['starter'] === 'number'
      ? `Added ${String(m['starter'])} starter articles to the advice library`
      : `Saved the advice article ${str(m, 'slug')} (${str(m, 'status')})`,
  'auth.token_created': (m) => `Made the access token ${str(m, 'name')} (${str(m, 'scope')})`,
  'auth.token_revoked': (m) => `Revoked the access token ${str(m, 'name')}`,
  'auth.second_factor_enabled': () => 'Switched two-step sign-in on',
  'auth.second_factor_disabled': () => 'Switched two-step sign-in off',
  'auth.second_step_failed': () => 'Sign-in failed at the second step (the password was right, the code was not)',
  'auth.recovery_code_used': (m) =>
    typeof m['left'] === 'number'
      ? `Signed in with a recovery code (${String(m['left'])} left)`
      : 'Signed in with a recovery code',
  'auth.recovery_codes_replaced': () => 'Made new recovery codes (the earlier ones stopped working)',
  'admin.second_factor_removed': (m) =>
    byAdmin(m)
      ? 'An administrator removed your two-step sign-in'
      : `Removed the two-step sign-in of ${str(m, 'email')}`,
  'admin.advice_deleted': (m) => `Deleted the advice article ${str(m, 'slug')}`,
  'admin.assistant_changed': (m) =>
    m['model'] !== undefined
      ? `Tuned the model ${str(m, 'model')} of the connection ${str(m, 'connection')}`
      : `Changed the assistant connection ${str(m, 'connection')} (${str(m, 'change')})`,
  // Transactions added by hand to a cash account: no amounts or payees in the log.
  'transaction.added': () => 'Added a transaction by hand',
  'transaction.edited': () => 'Changed a transaction added by hand',
  'transaction.deleted': () => 'Deleted a transaction added by hand',
  'transaction.merged': () => "Merged a typed card payment into the one from the bank's file",
  // Friends: the other side's log says the friend did it (`by`).
  'friend.added': (m) =>
    str(m, 'by') === 'friend'
      ? `${str(m, 'name') || 'Someone'} entered your friend code; you are friends now`
      : `Became friends with ${str(m, 'name') || 'someone'}`,
  'friend.removed': (m) => (str(m, 'by') === 'friend' ? 'A friend removed you' : 'Removed a friend'),
  'friend.code_failed': () => 'Entered a friend code that was wrong or had expired',
  // Shared groups: the group's name, never amounts or payees.
  'shared.created': (m) => `Created the shared group ${str(m, 'group') || ''}`.trim(),
  'shared.deleted': () => 'Deleted a shared group',
  'shared.archived': (m) => `Archived the shared group ${str(m, 'group') || ''}`.trim(),
  'shared.invited': (m) => `Invited ${str(m, 'name') || 'a friend'} to a shared group`,
  'shared.joined': (m) => `Joined the shared group ${str(m, 'group') || ''}`.trim(),
  'shared.declined': () => 'Declined an invitation to a shared group',
  'shared.left': () => 'Left a shared group',
  'shared.member_removed': () => 'Removed someone from a shared group',
  'shared.settled': () => 'Recorded a payment in a shared group',
};

function byAdmin(m: Meta): boolean {
  return str(m, 'by') === 'administrator';
}

export function describeActivity(e: ActivityEvent): string {
  return DESCRIBE[e.action](e.meta ?? {});
}

/** One AI change in words, e.g. "Set a category · 14 transactions and 1 more". */
function describeChange(c: AIChange): string {
  const what = PROPOSAL[c.action] ?? 'a change';
  return what.charAt(0).toUpperCase() + what.slice(1) + ` · ${plural(c.items, 'item')}`;
}

/**
 * Undo many of the assistant's changes at once (Phase 27): all of them, or the ones applied in a
 * period. It says how many before anything happens, and afterwards how many it had to leave.
 */
function UndoSeveral({ onClose }: { onClose: () => void }) {
  const all = useAllAIChanges();
  const revert = useRevertAIChanges();
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const chosen = openChangesIn(all.data ?? [], from, to);
  const period = from || to;
  return (
    <Modal
      open
      size="sm"
      title="Undo the assistant’s changes"
      onClose={onClose}
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="danger"
            disabled={revert.isPending || chosen.length === 0}
            onClick={() => {
              revert.mutate(chosen, {
                onSuccess: ({ undone, left }) => {
                  toast(
                    left
                      ? `Undone ${plural(undone, 'change')}. ${plural(left, 'change')} left as ${left === 1 ? 'it is' : 'they are'}: something ${left === 1 ? 'it' : 'they'} touched was changed since.`
                      : `Undone ${plural(undone, 'change')}`,
                    { ms: 6000 },
                  );
                  onClose();
                },
                onError: (e) => {
                  toast(errorMessage(e), { kind: 'error' });
                },
              });
            }}
          >
            {revert.isPending ? 'Undoing…' : `Undo ${plural(chosen.length, 'change')}`}
          </Button>
        </>
      }
    >
      <div className="adm-form">
        <p className="hint">
          Leave both days empty for everything the assistant ever changed, or give a period. Each change is put back as
          Undo does it one by one, the newest first; a change is left alone when something it touched was changed since.
        </p>
        <div className="form-row">
          <label className="st-col">
            From
            <TextInput
              type="date"
              value={from}
              max={to || undefined}
              onChange={(e) => {
                setFrom(e.target.value);
              }}
            />
          </label>
          <label className="st-col">
            To
            <TextInput
              type="date"
              value={to}
              min={from || undefined}
              onChange={(e) => {
                setTo(e.target.value);
              }}
            />
          </label>
        </div>
        <p role="status">
          {all.isError
            ? errorMessage(all.error)
            : all.isLoading
              ? 'Counting…'
              : chosen.length
                ? `${plural(chosen.length, 'change')} in effect ${period ? 'in this period' : 'in all'}.`
                : `Nothing to undo ${period ? 'in this period' : ''}`.trim() + '.'}
        </p>
      </div>
    </Modal>
  );
}

function AssistantChanges() {
  const q = useAIChanges();
  const revert = useRevertAIChange();
  const navigate = useNavigate();
  const patch = useViews((s) => s.patch);
  const [bulk, setBulk] = useState(false);
  const changes = q.data?.pages.flatMap((p) => p.changes) ?? [];
  // only once the assistant changed something (a failed read stays quiet: the log below works)
  if (changes.length === 0) return null;
  return (
    <div className="set-card">
      <div className="lbl">Assistant changes</div>
      <p className="hint">
        What the assistant's proposals changed, newest first. Undo puts back what was there, as long as nothing was
        changed since.
      </p>
      {q.isError && <p className="set-err">{errorMessage(q.error)}</p>}
      <ul className="set-list" aria-label="Assistant changes">
        {changes.map((c) => (
          <li key={c.id}>
            <div className="set-li-main">
              <b>{describeChange(c)}</b>
              {c.auto && <span className="tag">applied automatically</span>}
              {c.revertedAt && <span className="tag">undone</span>}
              <span className="hint">
                {fmtDateTime(c.appliedAt)}
                {c.revertedAt ? ` · undone ${fmtDateTime(c.revertedAt)}` : ''}
              </span>
            </div>
            {!c.revertedAt && (
              <Button
                size="sm"
                disabled={revert.isPending}
                onClick={() => {
                  revert.mutate(c.id, {
                    onSuccess: () => {
                      toast('Undone');
                    },
                    onError: (e) => {
                      toast(errorMessage(e), { kind: 'error' });
                    },
                  });
                }}
              >
                Undo
              </Button>
            )}
          </li>
        ))}
      </ul>
      <div className="set-row">
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            patch('all', { aiOnly: true });
            void navigate(txPath());
          }}
        >
          Show what the assistant changed
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            setBulk(true);
          }}
        >
          Undo several…
        </Button>
        {bulk && (
          <UndoSeveral
            onClose={() => {
              setBulk(false);
            }}
          />
        )}
        {q.hasNextPage && (
          <Button size="sm" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            {q.isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        )}
      </div>
    </div>
  );
}

export function ActivitySettings() {
  const q = useActivity();
  const events = q.data?.pages.flatMap((p) => p.events) ?? [];
  return (
    <>
      <AssistantChanges />
      <div className="set-card">
        <div className="lbl">Account activity</div>
        <p className="hint">
          Sign-ins, exports, deletions and other important changes, newest first, kept for one year. A failed sign-in
          you don't recognise is a reason to change your password.
        </p>
        {q.isError && <p className="set-err">{errorMessage(q.error)}</p>}
        {q.isSuccess && events.length === 0 && <p className="hint">Nothing recorded yet.</p>}
        <ul className="set-list" aria-label="Activity">
          {events.map((e) => (
            <li key={e.id}>
              <div className="set-li-main">
                <b>{describeActivity(e)}</b>
                {e.action === 'auth.login_failed' && <span className="tag warn">attention</span>}
                <span className="hint">
                  {fmtDateTime(e.at)}
                  {e.ip ? ` · ${e.ip}` : ' · in the background'}
                  {e.browser ? ` · ${e.browser}` : ''}
                </span>
              </div>
            </li>
          ))}
        </ul>
        {q.hasNextPage && (
          <Button size="sm" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            {q.isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        )}
      </div>
    </>
  );
}
