// The report of a legacy migration (PLAN §4.10 step 5): what was imported, reused or failed, and
// which labels and duplicate decisions found their transactions.

import type { LegacyImport } from '../api/hooks/legacy';
import { fmtDateTime } from '../lib/dates';
import { fmtInt, fmtMoney, plural } from '../lib/format';

type Match = { matched: number; unmatched: number; examples: { what: string; key: string; reason: string }[] };

const STATUS: Record<string, string> = { imported: 'Imported', reused: 'Already imported', failed: 'Not imported' };

function Unmatched({ title, m }: { title: string; m: Match }) {
  if (m.unmatched === 0) return null;
  return (
    <details className="legacy-misses">
      <summary>
        {plural(m.unmatched, title)} without a transaction
        {m.unmatched > m.examples.length ? ` (the first ${String(m.examples.length)} below)` : ''}
      </summary>
      <ul>
        {m.examples.map((x, i) => (
          <li key={i}>
            <b>{x.what}</b> · <span className="mono">{x.key}</span> — {x.reason}
          </li>
        ))}
      </ul>
    </details>
  );
}

export function LegacyReportView({ run }: { run: LegacyImport }) {
  const r = run.report;
  if (run.status === 'failed' || !r) {
    return (
      <p className="set-err" role="alert">
        {run.error ?? 'The migration stopped with an error.'}
      </p>
    );
  }
  const failed = r.files.filter((f) => f.status === 'failed').length;
  return (
    <div className="legacy-report">
      <p>
        {failed === 0 ? 'Migration finished' : `Migration finished; ${plural(failed, 'file')} could not be imported`}
        {run.finishedAt ? ` (${fmtDateTime(run.finishedAt)})` : ''}: {fmtInt(r.transactions.new)} new transactions,{' '}
        {fmtInt(r.transactions.duplicates)} duplicates skipped.
      </p>
      <table className="legacy-files">
        <thead>
          <tr>
            <th>File</th>
            <th>Account</th>
            <th>Result</th>
            <th className="r">New</th>
            <th className="r">Duplicates</th>
          </tr>
        </thead>
        <tbody>
          {r.files.map((f) => (
            <tr key={f.legacyId} className={f.status === 'failed' ? 'neg' : undefined}>
              <td>
                {f.name}
                {f.message && <div className="hint">{f.message}</div>}
              </td>
              <td>{f.accountName}</td>
              <td>{STATUS[f.status] ?? f.status}</td>
              <td className="r">{fmtInt(f.new)}</td>
              <td className="r">{fmtInt(f.duplicates)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <ul className="legacy-lines">
        {r.accounts.some((a) => a.created) && (
          <li>
            New accounts:{' '}
            {r.accounts
              .filter((a) => a.created)
              .map((a) => a.name)
              .join(', ')}
          </li>
        )}
        {(r.groups.created > 0 || r.groups.reused > 0) && (
          <li>
            Groups: {fmtInt(r.groups.created)} created
            {r.groups.reused > 0 ? `, ${fmtInt(r.groups.reused)} already there` : ''}
          </li>
        )}
        {r.labels.matched + r.labels.unmatched > 0 && (
          <li>
            Labels: {fmtInt(r.labels.matched)} of {fmtInt(r.labels.matched + r.labels.unmatched)} found their
            transactions
            <Unmatched title="label" m={r.labels} />
          </li>
        )}
        {r.overrides.matched + r.overrides.unmatched > 0 && (
          <li>
            Duplicate decisions: {fmtInt(r.overrides.matched)} of {fmtInt(r.overrides.matched + r.overrides.unmatched)}{' '}
            applied
            <Unmatched title="decision" m={r.overrides} />
          </li>
        )}
        {(r.budgets.set > 0 || r.budgets.skipped.length > 0) && (
          <li>
            Budgets: {fmtInt(r.budgets.set)} set
            {r.budgets.skipped.length > 0 ? `; skipped (no amount): ${r.budgets.skipped.join(', ')}` : ''}
          </li>
        )}
        {r.opening && (
          <li>
            Opening balance {fmtMoney({ amountMinor: r.opening.amountMinor, currency: 'PLN' })} on{' '}
            {r.opening.accountName}
          </li>
        )}
        {r.investments !== 'none' && (
          <li>
            Investments: {r.investments === 'imported' ? 'imported' : 'kept the values you had already entered here'}
          </li>
        )}
        {r.chats.imported + r.chats.skipped > 0 && (
          <li>
            Chats: {fmtInt(r.chats.imported)} copied
            {r.chats.skipped > 0 ? `, ${fmtInt(r.chats.skipped)} skipped (already there, empty or over the limit)` : ''}
          </li>
        )}
        {r.unknownKeys.length > 0 && (
          <li>Ignored parts of the backup this version doesn't know: {r.unknownKeys.join(', ')}</li>
        )}
      </ul>
    </div>
  );
}
