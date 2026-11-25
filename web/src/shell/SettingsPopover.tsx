// Quick settings, the title bar's ⚙ (the legacy `#setBtn` popover; slimmed down in Phase 20, laid out
// again in Phase 26, U4): "All settings" in the header, the colour theme with the dark-mode switch
// on one line, the interface size as a plain slider, then Keyboard shortcuts. Everything else lives
// on the settings pages; the account menu has Profile and Sign out.

import { useNavigate } from 'react-router';

import { QuickAppearance } from '../settings/Appearance';
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
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            onClose();
            void navigate('/settings/general');
          }}
        >
          All settings
        </Button>
      </div>
      <div className="pop-scroll">
        <div className="pop-s qs">
          <QuickAppearance />
        </div>
      </div>
      <div className="pop-f">
        <Button
          variant="ghost"
          icon="cmd"
          className="qs-keys"
          onClick={() => {
            onClose();
            useUi.getState().setKeysOpen(true);
          }}
        >
          Keyboard shortcuts<kbd>?</kbd>
        </Button>
      </div>
    </Popover>
  );
}
