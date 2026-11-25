// The app frame (legacy `.app`): title bar / main (sidebar, content, inspector, later the assistant)
// / status bar, on a grid whose middle row is minmax(0, 1fr) so wide content never overflows the
// page. Routes render into the content area.

import { useQueryClient } from '@tanstack/react-query';
import { lazy, Suspense, useEffect, useRef } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router';

import { invalidateData, useAccounts } from '../api/hooks/accounts';
import { DuplicatesDialog } from '../duplicates/DuplicatesDialog';
import { configureImports } from '../import/queue';
import { escapeSelection, extendSelection, moveSelection } from '../table/controller';
import { Inspector } from '../table/Inspector';
import { TableStatus } from '../table/TableStatus';
import { useMeta, type User } from '../api/hooks/me';
import { sidebarShown, useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { TooltipLayer } from '../ui/Tooltip';
import { Toaster } from '../ui/Toast';
import { CashAccountDialog } from '../cash/CashAccountDialog';
import { useCashDialogs } from '../cash/store';
import { TransactionDialog } from '../cash/TransactionDialog';
import { DropOverlay } from './DropOverlay';
import { useAppearance, useSidebarAutoCollapse } from './effects';
import { FILE_ACCEPT, importFiles, openFilePicker, registerPicker } from './files';
import { useGlobalKeys } from './keys';
import { KeyboardShortcuts } from './KeyboardShortcuts';
import { loadOverview } from '../overview/Overview';
import { Sidebar } from './Sidebar';
import { StatusBar } from './StatusBar';
import { TitleBar } from './TitleBar';
import { inSharedPath, isViewPath, modeOf, sharedPath, txPath, viewIdOf, viewPath } from '../lib/paths';

export type ShellContext = { hasData: boolean; user: User };

// The assistant panel is its own chunk, loaded the first time it opens.
const ChatPanel = lazy(async () => ({ default: (await import('../assistant/ChatPanel')).ChatPanel }));

export function AppShell({ user }: { user: User }) {
  useAppearance();
  useSidebarAutoCollapse();
  const navigate = useNavigate();
  const location = useLocation();
  const viewId = viewIdOf(location.pathname);
  const sbShown = useUi(sidebarShown);
  // table or overview comes from the address alone, so the shell and the page change together (U2)
  const mode = modeOf(location.pathname);
  const vizOpen = useUi((s) => s.vizOpen);
  // an assistant an administrator switched off (Phase 19) never opens
  const assistantOn = useMeta().data?.features.assistant !== false;
  const aiOpen = useUi((s) => s.ai.open) && assistantOn;
  const aiMax = useUi((s) => s.ai.max);
  const searchRef = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();
  const viewAccount = useRef<string | undefined>(undefined);

  // The overview's chunk loads once the app is idle, so switching to it doesn't wait (PLAN §7.12).
  useEffect(() => {
    const t = setTimeout(() => void loadOverview(), 1500);
    return () => {
      clearTimeout(t);
    };
  }, []);

  // Every file entry point (drop, ⌘O, Browse, mini drop zone) feeds the upload queue.
  useEffect(() => {
    configureImports({
      navigate: (to) => void navigate(to),
      refresh: () => {
        invalidateData(qc);
      },
      autoCommit: () => useUi.getState().importFastPath,
      account: () => viewAccount.current,
    });
  }, [navigate, qc]);

  const accounts = useAccounts();
  const viewCash = accounts.data?.find((a) => a.id === viewId && a.kind === 'cash');
  useEffect(() => {
    // files dropped while looking at a cash account don't go into it (ADR 0029)
    viewAccount.current = viewId === 'all' || viewCash ? undefined : viewId;
  }, [viewId, viewCash]);
  const list = accounts.data ?? [];
  const total = list.reduce((n, a) => n + a.txCount, 0);
  // a cash account is data even before its first transaction: its table is where they are added
  const hasData = total > 0 || list.some((a) => a.kind === 'cash');
  const rows = viewId === 'all' ? total : (list.find((a) => a.id === viewId)?.txCount ?? 0);
  // settings and the admin panel share the settings layout
  const inSettings = location.pathname.startsWith('/settings') || location.pathname.startsWith('/admin');
  // the Shared tab (Phase 19): a page of its own, like the overview, without the table's parts
  const inShared = inSharedPath(location.pathname);
  // the Shared page holds shared groups and, since Phase 26, friends: it exists while either is on
  const features = useMeta().data?.features;
  const sharedOn = (features?.sharedGroups !== false || features.friends !== false) && !user.isDemo;
  const lastView = useRef(txPath());
  useEffect(() => {
    if (!inShared && !inSettings && isViewPath(location.pathname)) lastView.current = location.pathname;
  }, [inShared, inSettings, location.pathname]);
  const toggleShared = () => {
    void navigate(inShared ? lastView.current : sharedPath());
  };
  // The table's inspector and status bar, for a view that exists (Views redirects the others).
  const tableShown =
    mode === 'table' && hasData && !inSettings && !inShared && (viewId === 'all' || list.some((a) => a.id === viewId));
  const selected = useViews((s) => viewOf(s.views, viewId).selectedId);

  const setMode = (m: 'table' | 'ov') => {
    void navigate(viewPath(m, viewId));
  };

  useGlobalKeys({
    focusSearch: () => {
      const el = searchRef.current;
      if (!el || el.disabled) return false;
      el.focus();
      el.select();
      return true;
    },
    openFiles: openFilePicker,
    toggleAssistant: () => {
      if (!assistantOn) return;
      const s = useUi.getState();
      s.setAiOpen(!s.ai.open);
    },
    openShortcuts: () => {
      useUi.getState().setKeysOpen(true);
    },
    toggleShared: () => {
      if (!sharedOn) return false;
      toggleShared();
      return true;
    },
    addTransaction: () => {
      if (!viewCash || inSettings || inShared || mode !== 'table') return false;
      useCashDialogs.getState().openTransaction({ accountId: viewCash.id, currency: viewCash.currency });
      return true;
    },
    move: moveSelection,
    extend: extendSelection,
    escape: escapeSelection,
  });

  const cls = [
    'app',
    !sbShown && 'no-sb',
    !vizOpen && 'no-viz',
    mode === 'ov' && !inSettings && !inShared && 'mode-ov',
    inShared && 'mode-sh',
    accounts.isSuccess && !hasData && !inSettings && !inShared && 'is-empty',
    tableShown && selected && 'has-insp',
    aiOpen && 'has-chat',
    aiOpen && aiMax && 'chat-max',
    user.isDemo && 'is-demo',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div className={cls} id="app">
      {user.isDemo && (
        <div className="demo-banner" role="status">
          Demo — your data is deleted after 24 hours.
        </div>
      )}
      <TitleBar
        user={user}
        viewId={viewId}
        hasData={hasData}
        searchRef={searchRef}
        mode={mode}
        onToggleMode={setMode}
        inShared={inShared}
        onToggleShared={toggleShared}
      />
      <div className="main">
        <Sidebar accounts={list} total={total} email={user.email} />
        <section className="content">
          <Outlet context={{ hasData, user } satisfies ShellContext} />
        </section>
        {tableShown && <Inspector key={viewId} viewId={viewId} />}
        {aiOpen && (
          <Suspense fallback={null}>
            <ChatPanel viewId={viewId} />
          </Suspense>
        )}
      </div>
      <StatusBar hasData={hasData} rows={rows} accounts={list.length}>
        {tableShown ? <TableStatus key={viewId} viewId={viewId} all={rows} /> : null}
      </StatusBar>
      <DropOverlay onFiles={importFiles} />
      <KeyboardShortcuts />
      <DuplicatesDialog />
      <CashAccountDialog />
      <TransactionDialog />
      <Toaster />
      <TooltipLayer />
      <input
        ref={registerPicker}
        type="file"
        multiple
        accept={FILE_ACCEPT}
        hidden
        onChange={(e) => {
          if (e.target.files) importFiles(e.target.files);
          e.target.value = '';
        }}
      />
    </div>
  );
}
