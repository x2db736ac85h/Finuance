// The legacy `header.titlebar` (PLAN §7.6): sidebar, charts-pane and overview toggles, the brand,
// search (⌘F, /), the assistant button (⌘J), settings and the user menu. No import, theme or size
// buttons: files open with ⌘O, the drop zones or drag & drop; appearance lives in settings.

import { useEffect, useRef, useState, type RefObject } from 'react';
import { useNavigate } from 'react-router';

import { useMeta, type User } from '../api/hooks/me';
import { sidebarShown, useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { modKey } from '../ui/Kbd';
import { Logo } from '../ui/Logo';
import { Menu, MenuItem } from '../ui/Menu';
import { usePopover } from '../ui/Popover';
import { SettingsPopover } from './SettingsPopover';
import { useSignOut } from './useSignOut';

/** Search debounce (legacy). */
const SEARCH_MS = 120;

type Props = {
  user: User;
  viewId: string;
  hasData: boolean;
  searchRef: RefObject<HTMLInputElement | null>;
  onToggleMode: (mode: 'table' | 'ov') => void;
};

export function TitleBar({ user, viewId, hasData, searchRef, onToggleMode }: Props) {
  const mode = useUi((s) => s.mode);
  const vizOpen = useUi((s) => s.vizOpen);
  const sbShown = useUi(sidebarShown);
  const aiOpen = useUi((s) => s.ai.open);
  const { toggleSidebar, toggleViz, setAiOpen } = useUi.getState();
  const settings = usePopover();
  const account = usePopover();
  const signOut = useSignOut();
  const navigate = useNavigate();
  // parts of the app an administrator switched off (Phase 19) have no button
  const features = useMeta().data?.features;

  return (
    <header className="titlebar">
      <IconButton
        icon="sidebar"
        title="Toggle sidebar"
        aria-pressed={sbShown}
        onClick={() => {
          toggleSidebar();
        }}
      />
      <IconButton
        icon="chart"
        title="Charts pane (list view)"
        on={mode !== 'ov' && vizOpen}
        aria-pressed={mode !== 'ov' && vizOpen}
        onClick={() => {
          if (mode === 'ov') onToggleMode('table');
          toggleViz();
        }}
      />
      {features?.overview !== false && (
        <IconButton
          icon="dash"
          title="Personal finance overview"
          on={mode === 'ov'}
          aria-pressed={mode === 'ov'}
          onClick={() => {
            onToggleMode(mode === 'ov' ? 'table' : 'ov');
          }}
        />
      )}
      <div className="brand">
        <Logo />
        <span className="name">Finuance</span>
      </div>
      <SearchBox key={viewId} viewId={viewId} disabled={!hasData} inputRef={searchRef} />
      {features?.assistant !== false && (
        <button
          type="button"
          className={aiOpen ? 'btn sm aibtn on' : 'btn sm aibtn'}
          title={`AI assistant (${modKey('J')})`}
          aria-pressed={aiOpen}
          onClick={() => {
            setAiOpen(!aiOpen);
          }}
        >
          <Icon name="ai" />
          <span className="bl">Assistant</span>
        </button>
      )}
      <IconButton icon="cog" title="Settings" aria-expanded={settings.open} onClick={settings.toggle} />
      <button
        type="button"
        className={account.open ? 'avatar on' : 'avatar'}
        title={user.displayName || user.email}
        aria-label="Account menu"
        aria-expanded={account.open}
        onClick={account.toggle}
      >
        {initial(user)}
      </button>
      <SettingsPopover open={settings.open} anchor={settings.anchor} onClose={settings.close} />
      <Menu open={account.open} anchor={account.anchor} onClose={account.close} label="Account" width={240}>
        <div className="who">
          <b>{user.displayName || user.email}</b>
          {user.displayName && <span className="hint">{user.email}</span>}
        </div>
        <MenuItem
          icon="pen"
          onSelect={() => {
            account.close();
            void navigate('/settings/profile');
          }}
        >
          Profile
        </MenuItem>
        <MenuItem
          icon="lock"
          onSelect={() => {
            account.close();
            void navigate('/settings/sessions');
          }}
        >
          Sessions
        </MenuItem>
        {user.isAdmin && (
          <MenuItem
            icon="cog"
            onSelect={() => {
              account.close();
              void navigate('/admin/users');
            }}
          >
            Administration
          </MenuItem>
        )}
        <MenuItem
          icon="x"
          onSelect={() => {
            account.close();
            void signOut();
          }}
        >
          Sign out
        </MenuItem>
      </Menu>
    </header>
  );
}

function initial(u: User): string {
  return (u.displayName || u.email).trim().charAt(0).toUpperCase() || '?';
}

/**
 * Search box: writes the view's search (session-only) after a short pause, and follows changes made
 * elsewhere (Clear all, the search chip). `sent` is the last value this box wrote, so its own
 * debounced writes don't reset what the user typed since.
 */
function SearchBox({
  viewId,
  disabled,
  inputRef,
}: {
  viewId: string;
  disabled: boolean;
  inputRef: RefObject<HTMLInputElement | null>;
}) {
  const stored = useViews((s) => viewOf(s.views, viewId).search);
  const [value, setValue] = useState(stored);
  const [seen, setSeen] = useState(stored);
  const [sent, setSent] = useState(stored);
  if (stored !== seen) {
    setSeen(stored);
    if (stored !== sent) {
      setSent(stored);
      setValue(stored);
    }
  }
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(
    () => () => {
      clearTimeout(timer.current);
    },
    [],
  );
  const commit = (v: string, delay: number) => {
    setValue(v);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      setSent(v);
      useViews.getState().patch(viewId, { search: v });
    }, delay);
  };
  return (
    <label className="searchbox">
      <Icon name="search" />
      <input
        ref={inputRef}
        id="search"
        type="text"
        placeholder="Search all columns…"
        aria-label="Search all columns"
        autoComplete="off"
        spellCheck={false}
        disabled={disabled}
        value={value}
        onChange={(e) => {
          commit(e.target.value, SEARCH_MS);
        }}
        onKeyDown={(e) => {
          // legacy Esc order: clear the search, then leave it
          if (e.key !== 'Escape') return;
          e.stopPropagation();
          if (value) commit('', 0);
          else e.currentTarget.blur();
        }}
      />
      {value && (
        <IconButton
          icon="x"
          size="sm"
          title="Clear search"
          onClick={() => {
            commit('', 0);
            inputRef.current?.focus();
          }}
        />
      )}
      <kbd>{modKey('F')}</kbd>
    </label>
  );
}
