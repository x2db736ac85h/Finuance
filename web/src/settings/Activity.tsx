// Settings → Activity (PLAN 17.6): the account's activity log, newest first, with the address and
// browser of each action. The server keeps it for a year; nothing here can change it.

import { useActivity, type ActivityAction, type ActivityEvent } from '../api/hooks/activity';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, plural } from '../lib/format';
import { Button } from '../ui/Button';

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
};

export function describeActivity(e: ActivityEvent): string {
  return DESCRIBE[e.action](e.meta ?? {});
}

export function ActivitySettings() {
  const q = useActivity();
  const events = q.data?.pages.flatMap((p) => p.events) ?? [];
  return (
    <div className="set-card">
      <div className="lbl">Account activity</div>
      <p className="hint">
        Sign-ins, exports, deletions and other important changes, newest first, kept for one year. A failed sign-in you
        don't recognise is a reason to change your password.
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
  );
}
