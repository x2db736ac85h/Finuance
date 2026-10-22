// The legacy `footer.statusbar`. Until the grid exists (Phase 10) it shows the ready message or
// the size of the current view; totals from the server's stats join with the grid.

import { fmtInt, plural } from '../lib/format';
import { Icon } from '../ui/Icon';

type Props = { hasData: boolean; rows: number; accounts: number };

export function StatusBar({ hasData, rows, accounts }: Props) {
  return (
    <footer className="statusbar">
      <div id="stL">
        {hasData ? (
          <>
            <span className="st">
              <Icon name="table" />
              <b>{fmtInt(rows)}</b> {rows === 1 ? 'transaction' : 'transactions'}
            </span>
            <span className="st">{plural(accounts, 'account')}</span>
          </>
        ) : (
          <span className="st">
            <Icon name="table" />
            Ready — drop CSV files anywhere to begin
          </span>
        )}
      </div>
      <div id="stR" />
    </footer>
  );
}
