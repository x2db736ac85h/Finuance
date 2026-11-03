// The app frame (legacy `.app`): title bar / main (sidebar, content, inspector, later the assistant)
// / status bar, on a grid whose middle row is minmax(0, 1fr) so wide content never overflows the
// page. Routes render into the content area.

import { useQueryClient } from '@tanstack/react-query';
import { lazy, Suspense, useEffect, useRef } from 'react';
import { Outlet, useLocation, useMatch, useNavigate } from 'react-router';

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
import { DropOverlay } from './DropOverlay';
import { useAppearance, useSidebarAutoCollapse } from './effects';
import { FILE_ACCEPT, importFiles, openFilePicker, registerPicker } from './files';
import { useGlobalKeys } from './keys';
import { KeyboardShortcuts } from './KeyboardShortcuts';
import { loadOverview } from '../overview/Overview';
import { Sidebar } from './Sidebar';
import { StatusBar } from './StatusBar';
import { TitleBar } from './TitleBar';

export type ShellContext = { hasData: boolean; user: User };

// The assistant panel is its own chunk, loaded the first time it opens.
const ChatPanel = lazy(async () => ({ default: (await import('../assistant/ChatPanel')).ChatPanel }));

export function AppShell({ user }: { user: User }) {
  useAppearance();
  useSidebarAutoCollapse();
  const navigate = useNavigate();
  const location = useLocation();
  const view = useMatch('/:mode/:viewId');
  const viewId = view && (view.params.mode === 't' || view.params.mode === 'o') ? (view.params.viewId ?? 'all') : 'all';
  const sbShown = useUi(sidebarShown);
  const mode = useUi((s) => s.mode);
  const vizOpen = useUi((s) => s.vizOpen);
  // an assistant an administrator switched off (Phase 19) never opens
  const assistantOn = useMeta().data?.features.assistant !== false;
  const aiOpen = useUi((s) => s.ai.open) && assistantOn;
  const aiMax = useUi((s) => s.ai.max);
  const searchRef = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();
  const viewAccount = useRef<string | undefined>(undefined);
  useEffect(() => {
    viewAccount.current = viewId === 'all' ? undefined : viewId;
  }, [viewId]);

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
  const list = accounts.data ?? [];
  const total = list.reduce((n, a) => n + a.txCount, 0);
  const hasData = total > 0;
  const rows = viewId === 'all' ? total : (list.find((a) => a.id === viewId)?.txCount ?? 0);
  // settings and the admin panel share the settings layout
  const inSettings = location.pathname.startsWith('/settings') || location.pathname.startsWith('/admin');
  // The table's inspector and status bar, for a view that exists (Views redirects the others).
  const tableShown =
    mode === 'table' && hasData && !inSettings && (viewId === 'all' || list.some((a) => a.id === viewId));
  const selected = useViews((s) => viewOf(s.views, viewId).selectedId);

  const setMode = (m: 'table' | 'ov') => {
    useUi.getState().setMode(m);
    void navigate(`/${m === 'ov' ? 'o' : 't'}/${viewId}`);
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
    move: moveSelection,
    extend: extendSelection,
    escape: escapeSelection,
  });

  const cls = [
    'app',
    !sbShown && 'no-sb',
    !vizOpen && 'no-viz',
    mode === 'ov' && !inSettings && 'mode-ov',
    accounts.isSuccess && !hasData && !inSettings && 'is-empty',
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
      <TitleBar user={user} viewId={viewId} hasData={hasData} searchRef={searchRef} onToggleMode={setMode} />
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
