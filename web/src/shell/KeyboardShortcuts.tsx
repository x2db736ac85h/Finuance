// The keyboard shortcuts window (legacy #keysDlg + KEYS): opened from settings or with `?`.

import { useUi } from '../stores/uiStore';
import { Kbd, MOD } from '../ui/Kbd';
import { Modal } from '../ui/Modal';

type Row = [keys: string[], what: string];

export const KEYS: [section: string, rows: Row[]][] = [
  [
    'General',
    [
      [[MOD, 'F'], 'Search all columns'],
      [['/'], 'Search (when not typing)'],
      [[MOD, 'O'], 'Open CSV files'],
      [[MOD, 'J'], 'Show / hide the assistant'],
      [['?'], 'This list of shortcuts'],
      [['Esc'], 'Close a window or menu · clear the search · clear the selection'],
    ],
  ],
  [
    'Table',
    [
      [['↑', '↓'], 'Previous / next row'],
      [['K', 'J'], 'Previous / next row'],
      [['PgUp', 'PgDn'], 'Jump one screen of rows'],
      [['Click header'], 'Sort: ascending → descending → off'],
      [['Right-click header'], 'Column menu — filter, sort, hide'],
      [['Double-click cell'], 'Copy the value'],
      [['Drag across rows'], 'Select several rows'],
      [['Shift', 'Click'], 'Select a range of rows'],
      [[MOD, 'Click'], 'Add / remove a row from the selection'],
    ],
  ],
  [
    'Charts & assistant',
    [
      [['←', '→'], 'Previous / next chart in the large view'],
      [['Enter'], 'Send a message to the assistant'],
      [['Shift', 'Enter'], 'New line in a message'],
    ],
  ],
];

export function KeyboardShortcuts() {
  const open = useUi((s) => s.keysOpen);
  return (
    <Modal
      open={open}
      onClose={() => {
        useUi.getState().setKeysOpen(false);
      }}
      title="Keyboard shortcuts"
      size="sm"
    >
      <div className="keys-b">
        {KEYS.map(([section, rows]) => (
          <div className="ks" key={section}>
            <div className="lbl">{section}</div>
            {rows.map(([keys, what]) => (
              <div className="kr" key={what + keys.join()}>
                <span className="kk">
                  <Kbd keys={keys} />
                </span>
                <span>{what}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </Modal>
  );
}
