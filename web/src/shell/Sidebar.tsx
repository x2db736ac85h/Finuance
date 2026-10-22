// The legacy `aside#sidebar` (PLAN §7.6): Explorer with All accounts and each account with its
// transaction count, the mini drop zone and a footer. Import history per account and the account
// context menu arrive with the import UX (Phase 9).

import { NavLink } from 'react-router';

import type { Account } from '../api/hooks/accounts';
import { fmtInt } from '../lib/format';
import { Icon } from '../ui/Icon';
import { openFilePicker } from './files';

type Props = { accounts: readonly Account[]; total: number; email: string };

const item = ({ isActive }: { isActive: boolean }) => (isActive ? 'fi on' : 'fi');

export function Sidebar({ accounts, total, email }: Props) {
  return (
    <aside className="sidebar" aria-label="Accounts">
      <nav className="sec" aria-label="Explorer">
        <div className="sec-h">Explorer</div>
        {accounts.length ? (
          <>
            <NavLink to="/t/all" className={item} title="Every account combined">
              <Icon name="layers" />
              <span className="fn">All accounts</span>
              <span className="fc">{fmtInt(total)}</span>
            </NavLink>
            <div className="fsep" />
            {accounts.map((a) => (
              <NavLink key={a.id} to={`/t/${a.id}`} className={item} title={accountTitle(a)}>
                <Icon name="folder" />
                <span className="fn">{a.name}</span>
                {a.isDemo && (
                  <span className="tag" title="Demo data — fictional transactions">
                    demo
                  </span>
                )}
                <span className="fc">{fmtInt(a.txCount)}</span>
              </NavLink>
            ))}
          </>
        ) : (
          <div className="fi-empty">No accounts yet</div>
        )}
        <button type="button" className="mini-drop" onClick={openFilePicker}>
          <Icon name="upload" />
          <span>
            Drop CSV files or <span className="accent-t">browse</span>
          </span>
        </button>
      </nav>
      <div className="sb-foot">Stored on your Finuance server · {email}</div>
    </aside>
  );
}

function accountTitle(a: Account): string {
  return [a.name, a.numberHint && `…${a.numberHint}`, a.currency].filter(Boolean).join(' · ');
}
