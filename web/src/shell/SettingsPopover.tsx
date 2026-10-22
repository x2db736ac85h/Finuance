// The legacy settings popover (`#setBtn`, openSettings): scrollable, Appearance inline, Keyboard
// shortcuts, links to the settings pages, and Sign out. The legacy storage, passphrase and backup
// sections are retired: data lives on the server (Settings → Data).

import { useNavigate } from 'react-router';

import { useMe } from '../api/hooks/me';
import { AppearanceControls } from '../settings/Appearance';
import { useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { MenuItem } from '../ui/Menu';
import { Popover } from '../ui/Popover';
import { SECTIONS } from '../settings/sections';
import { useSignOut } from './useSignOut';

type Props = { open: boolean; anchor: Element | null; onClose: () => void };

export function SettingsPopover({ open, anchor, onClose }: Props) {
  const navigate = useNavigate();
  const signOut = useSignOut();
  const me = useMe();
  return (
    <Popover open={open} anchor={anchor} onClose={onClose} label="Settings" width={350}>
      <div className="pop-h">
        <Icon name="cog" />
        <b>Settings</b>
      </div>
      <div className="pop-scroll">
        <div className="pop-s">
          <div className="lbl">Appearance</div>
          <AppearanceControls />
        </div>
        <div className="pop-s">
          <div className="lbl">Account</div>
          {SECTIONS.filter((s) => s.id !== 'appearance').map((s) => (
            <MenuItem
              key={s.id}
              icon={s.icon}
              onSelect={() => {
                onClose();
                void navigate(`/settings/${s.id}`);
              }}
            >
              {s.label}
            </MenuItem>
          ))}
        </div>
        <div className="pop-s">
          <div className="lbl">Keyboard</div>
          <Button
            size="sm"
            icon="type"
            onClick={() => {
              onClose();
              useUi.getState().setKeysOpen(true);
            }}
          >
            Keyboard shortcuts<kbd>?</kbd>
          </Button>
        </div>
      </div>
      <div className="pop-f">
        <span className="hint">{me.data ? `Signed in as ${me.data.email}` : ''}</span>
        <Button
          size="sm"
          onClick={() => {
            onClose();
            void signOut();
          }}
        >
          Sign out
        </Button>
      </div>
    </Popover>
  );
}
