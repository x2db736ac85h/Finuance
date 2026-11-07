// Settings → General (Phase 20, 20.15): settings that aren't about looks or the account. Today the
// import fast path, which used to sit in the settings popover.

import { useUi } from '../stores/uiStore';
import { Checkbox } from '../ui/inputs';

export function GeneralSettings() {
  const fastPath = useUi((s) => s.importFastPath);
  return (
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
  );
}
