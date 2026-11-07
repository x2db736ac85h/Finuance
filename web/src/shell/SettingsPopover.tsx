// Quick settings, the title bar's ⚙ (the legacy `#setBtn` popover, slimmed down in Phase 20, 20.15):
// the colour theme (the current one, unfolding the others), System/Dark/Light, the interface size,
// then Keyboard shortcuts and All settings. Everything else lives on the settings pages; the account
// menu has Profile and Sign out.

import { useNavigate } from 'react-router';

import { AppearanceControls } from '../settings/Appearance';
import { useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { Popover } from '../ui/Popover';

type Props = { open: boolean; anchor: Element | null; onClose: () => void };

export function SettingsPopover({ open, anchor, onClose }: Props) {
  const navigate = useNavigate();
  return (
    <Popover open={open} anchor={anchor} onClose={onClose} label="Quick settings" width={320}>
      <div className="pop-h">
        <Icon name="cog" />
        <b>Quick settings</b>
      </div>
      <div className="pop-scroll">
        <div className="pop-s">
          <AppearanceControls compact />
        </div>
      </div>
      <div className="pop-f">
        <Button
          icon="type"
          onClick={() => {
            onClose();
            useUi.getState().setKeysOpen(true);
          }}
        >
          Keyboard shortcuts<kbd>?</kbd>
        </Button>
        <Button
          variant="primary"
          onClick={() => {
            onClose();
            void navigate('/settings/general');
          }}
        >
          All settings
        </Button>
      </div>
    </Popover>
  );
}
