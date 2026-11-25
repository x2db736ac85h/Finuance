// Content for /transactions/:viewId and /overview/:viewId: the empty state when there is no data, the table
// (Phase 10) or the overview (Phase 12).

import { Navigate, useOutletContext, useParams } from 'react-router';

import { useAccounts } from '../api/hooks/accounts';
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
        cash={account?.kind === 'cash' ? { accountId: account.id, currency: account.currency } : undefined}
      />
    );
  return <Overview key={viewId} viewId={viewId} />;
}
