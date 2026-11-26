// Settings → General (Phase 20, 20.15; Phase 26, U3): how the app looks, and the import fast path,
// which used to sit in the settings popover; and the assistant's memory (Phase 27).

import { useUi } from '../stores/uiStore';
import { Checkbox } from '../ui/inputs';
import { useMeta } from '../api/hooks/me';
import { AppearanceControls } from './Appearance';
import { MemoryCard } from './Memory';

export function GeneralSettings() {
  const fastPath = useUi((s) => s.importFastPath);
  const assistantOn = useMeta().data?.features.assistant !== false;
  return (
    <>
      <div className="set-card" id="appearance">
        <div className="lbl">Appearance</div>
        <AppearanceControls />
      </div>
      <div className="set-card">
        <div className="lbl">Import</div>
        <Checkbox
          checked={fastPath}
          onChange={(on) => {
            useUi.getState().setImportFastPath(on);
          }}
          label="Import known layouts without review"
        />
        <p className="hint">
          Files laid out like one you imported before go straight in; the toast offers Review and Undo.
        </p>
      </div>
      {assistantOn && <MemoryCard />}
    </>
  );
}
