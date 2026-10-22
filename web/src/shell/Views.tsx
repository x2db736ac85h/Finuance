// Content for /t/:viewId and /o/:viewId: the empty state when there is no data, the table
// (Phase 10), and a placeholder for the overview until Phase 11.

import { useEffect } from 'react';
import { Navigate, useOutletContext, useParams } from 'react-router';

import { useAccounts } from '../api/hooks/accounts';
import { fmtInt } from '../lib/format';
import { useUi } from '../stores/uiStore';
import { Icon } from '../ui/Icon';
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
  if (!accounts.data) return null;
  if (!hasData) return <EmptyState />;
  const account = accounts.data.find((a) => a.id === viewId);
  if (viewId !== 'all' && !account) return <Navigate to={`/${mode === 'ov' ? 'o' : 't'}/all`} replace />;
  const name = account?.name ?? 'All accounts';
  if (mode === 'table') return <Table key={viewId} viewId={viewId} title={name} icon={account ? 'folder' : 'layers'} />;
  const rows = account ? account.txCount : accounts.data.reduce((n, a) => n + a.txCount, 0);
  return (
    <>
      <div className="toolbar">
        <div className="vt-title">
          <Icon name={account ? 'folder' : 'layers'} />
          <span>{name}</span>
        </div>
        <span className="vt-meta">{fmtInt(rows)} transactions</span>
      </div>
      <div className="placeholder">
        <p className="muted">The overview is not built yet.</p>
      </div>
    </>
  );
}
