// Administration → Usage: how much the instance holds and how busy it was, counts only.

import { useInstanceUsage } from '../api/hooks/admin';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtBytes, fmtInt } from '../lib/format';

export function UsageAdmin() {
  const usage = useInstanceUsage(30);
  const u = usage.data;
  if (!u) return <div className="set-card">{usage.isError ? errorMessage(usage.error) : 'Loading…'}</div>;
  const peak = Math.max(1, ...u.days.map((d) => Math.max(d.imports, d.questions)));
  const tiles: [string, string][] = [
    ['Accounts', fmtInt(u.totals.users)],
    ['Active in 7 days', fmtInt(u.totals.active7)],
    ['Active in 30 days', fmtInt(u.totals.active30)],
    ['Transactions', fmtInt(u.transactions)],
    ['Imports', fmtInt(u.imports)],
    ['Stored files', fmtBytes(u.storedBytes)],
  ];
  return (
    <>
      <div className="set-card">
        <div className="lbl">The instance</div>
        <div className="adm-tiles">
          {tiles.map(([label, value]) => (
            <div key={label} className="adm-tile">
              <span className="hint">{label}</span>
              <b>{value}</b>
            </div>
          ))}
        </div>
      </div>
      <div className="set-card">
        <div className="lbl">The last 30 days</div>
        <table className="adm-table adm-days">
          <thead>
            <tr>
              <th scope="col">Day</th>
              <th scope="col" className="num">
                Imports
              </th>
              <th scope="col" className="num">
                Assistant questions
              </th>
            </tr>
          </thead>
          <tbody>
            {[...u.days].reverse().map((d) => (
              <tr key={d.day}>
                <td>{fmtDay(d.day)}</td>
                <td className="num">
                  <span className="adm-bar" style={{ width: `${(d.imports / peak) * 100}%` }} aria-hidden="true" />
                  {fmtInt(d.imports)}
                </td>
                <td className="num">
                  <span
                    className="adm-bar alt"
                    style={{ width: `${(d.questions / peak) * 100}%` }}
                    aria-hidden="true"
                  />
                  {fmtInt(d.questions)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
