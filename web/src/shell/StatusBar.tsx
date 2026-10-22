// The legacy `footer.statusbar`. The table mode fills it with its own parts (table/TableStatus);
// otherwise it shows the ready message or the size of the current view.

import type { ReactNode } from 'react';

import { fmtInt, plural } from '../lib/format';
import { Icon } from '../ui/Icon';

type Props = { hasData: boolean; rows: number; accounts: number; children?: ReactNode };

export function StatusBar({ hasData, rows, accounts, children }: Props) {
  return (
    <footer className="statusbar">
      {children ?? (
        <>
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
        </>
      )}
    </footer>
  );
}
