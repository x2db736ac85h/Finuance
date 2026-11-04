// The legacy `aside#sidebar` (PLAN §7.6): Explorer with All accounts and each account with its
// transaction count and, folded out, its import history (task 9.5); the mini drop zone and a footer.

import { useState } from 'react';
import { NavLink, useMatch } from 'react-router';

import type { Account } from '../api/hooks/accounts';
import { useImports, type ImportSummary } from '../api/hooks/imports';
import { fmtInt } from '../lib/format';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { useCashDialogs } from '../cash/store';
import { openFilePicker } from './files';
import { ImportHistory } from './ImportHistory';

type Props = { accounts: readonly Account[]; total: number; email: string };

const item = ({ isActive }: { isActive: boolean }) => (isActive ? 'fi on' : 'fi');

export function Sidebar({ accounts, total, email }: Props) {
  const imports = useImports();
  const byAccount = new Map<string, ImportSummary[]>();
  const pending: ImportSummary[] = [];
  for (const i of imports.data ?? []) {
    if (!i.accountId) pending.push(i);
    else byAccount.set(i.accountId, [...(byAccount.get(i.accountId) ?? []), i]);
  }
  return (
    <aside className="sidebar" aria-label="Accounts">
      <nav className="sec" aria-label="Explorer">
        <div className="sec-h">Explorer</div>
        {accounts.length || pending.length ? (
          <>
            <NavLink to="/t/all" className={item} title="Every account combined">
              <Icon name="layers" />
              <span className="fn">All accounts</span>
              <span className="fc">{fmtInt(total)}</span>
            </NavLink>
            <div className="fsep" />
            {accounts.map((a) => (
              <AccountItem key={a.id} account={a} imports={byAccount.get(a.id) ?? []} />
            ))}
            {pending.length > 0 && (
              <>
                <div className="fsep" />
                <div className="fi-sub">Not imported yet</div>
                <ImportHistory imports={pending} />
              </>
            )}
          </>
        ) : (
          <div className="fi-empty">No accounts yet</div>
        )}
        <button
          type="button"
          className="sb-add"
          onClick={() => {
            useCashDialogs.getState().openNewAccount();
          }}
          title="A wallet for cash: you add its expenses by hand"
        >
          <Icon name="plus" />
          <span>New cash account</span>
        </button>
        <button type="button" className="mini-drop" onClick={openFilePicker}>
          <Icon name="upload" />
          <span>
            Drop CSV files or <span className="accent-t">browse</span>
          </span>
        </button>
      </nav>
      <div className="sb-foot" title={`Signed in as ${email}`}>
        Stored on your Finuance server.
      </div>
    </aside>
  );
}

function accountTitle(a: Account): string {
  return [a.name, a.numberHint && `…${a.numberHint}`, a.currency].filter(Boolean).join(' · ');
}

function AccountItem({ account: a, imports }: { account: Account; imports: ImportSummary[] }) {
  const active = useMatch(`/t/${a.id}`) !== null;
  const [open, setOpen] = useState(false);
  return (
    <>
      <div className={active ? 'fi acc on' : 'fi acc'}>
        <IconButton
          icon={open ? 'chev' : 'right'}
          size="sm"
          className="fold"
          title={open ? 'Hide imports' : 'Show imports'}
          aria-expanded={open}
          onClick={() => {
            setOpen(!open);
          }}
        />
        <NavLink to={`/t/${a.id}`} className="fi-link" title={accountTitle(a)}>
          <Icon name="folder" />
          <span className="fn">{a.name}</span>
          {a.kind === 'cash' && (
            <span className="tag" title="A cash account: its transactions are added by hand">
              cash
            </span>
          )}
          {a.isDemo && (
            <span className="tag" title="Demo data — fictional transactions">
              demo
            </span>
          )}
          <span className="fc">{fmtInt(a.txCount)}</span>
        </NavLink>
      </div>
      {open && <ImportHistory imports={imports} />}
    </>
  );
}
