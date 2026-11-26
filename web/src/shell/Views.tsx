// Content for /transactions/:viewId and /overview/:viewId: the empty state when there is no data, the table
// (Phase 10) or the overview (Phase 12).

import { Navigate, useOutletContext, useParams } from 'react-router';

import { useAccounts, type Account } from '../api/hooks/accounts';
import { useMeta } from '../api/hooks/me';
import { Overview } from '../overview/Overview';
import { Table } from '../table/Table';
import type { ShellContext } from './AppShell';
import { EmptyState } from './EmptyState';
import { txPath, viewPath } from '../lib/paths';

export function TableView() {
  return <View mode="table" />;
}

export function OverviewView() {
  return <View mode="ov" />;
}

function View({ mode }: { mode: 'table' | 'ov' }) {
  const { hasData } = useOutletContext<ShellContext>();
  const { viewId = 'all' } = useParams();
  const accounts = useAccounts();
  const overviewOn = useMeta().data?.features.overview ?? true;
  if (mode === 'ov' && !overviewOn) return <Navigate to={txPath(viewId)} replace />;
  if (!accounts.data) return null;
  if (!hasData) return <EmptyState />;
  const account = accounts.data.find((a) => a.id === viewId);
  if (viewId !== 'all' && !account) return <Navigate to={viewPath(mode)} replace />;
  const name = account?.name ?? 'All accounts';
  if (mode === 'table')
    return (
      <Table
        key={viewId}
        viewId={viewId}
        title={name}
        icon={account ? 'folder' : 'layers'}
        add={addTarget(accounts.data, account)}
      />
    );
  return <Overview key={viewId} viewId={viewId} />;
}

/**
 * Where *Add* puts a transaction typed by hand (Phase 27: every account, not cash accounts only):
 * the account in view, or with all accounts in view the first one that takes them, with the form
 * asking which. An archived account takes none.
 */
export function addTarget(
  accounts: readonly Account[],
  inView: Account | undefined,
): { accountId: string; currency: string; pick?: boolean } | undefined {
  const takes = (a: Account) => !a.archived;
  if (inView) return takes(inView) ? { accountId: inView.id, currency: inView.currency } : undefined;
  // cash first: that is where most typed expenses go
  const first = accounts.find((a) => takes(a) && a.kind === 'cash') ?? accounts.find(takes);
  return first ? { accountId: first.id, currency: first.currency, pick: true } : undefined;
}
