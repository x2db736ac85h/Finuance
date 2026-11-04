// Content for /t/:viewId and /o/:viewId: the empty state when there is no data, the table
// (Phase 10) or the overview (Phase 12).

import { useEffect } from 'react';
import { Navigate, useOutletContext, useParams } from 'react-router';

import { useAccounts } from '../api/hooks/accounts';
import { useMeta } from '../api/hooks/me';
import { Overview } from '../overview/Overview';
import { useUi } from '../stores/uiStore';
import { Table } from '../table/Table';
import type { ShellContext } from './AppShell';
import { EmptyState } from './EmptyState';

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
  // The route decides the mode; the store remembers it for the next visit to /.
  useEffect(() => {
    if (useUi.getState().mode !== mode) useUi.getState().setMode(mode);
  }, [mode]);
  const overviewOn = useMeta().data?.features.overview ?? true;
  if (mode === 'ov' && !overviewOn) return <Navigate to={`/t/${viewId}`} replace />;
  if (!accounts.data) return null;
  if (!hasData) return <EmptyState />;
  const account = accounts.data.find((a) => a.id === viewId);
  if (viewId !== 'all' && !account) return <Navigate to={`/${mode === 'ov' ? 'o' : 't'}/all`} replace />;
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
