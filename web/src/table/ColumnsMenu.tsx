// The columns menu (legacy openColsMenu): show/hide each column, one toggle (Hide all when every
// column is visible, otherwise Show all) and Reset widths; plus whether the list shows transfers.

import { useViews, viewOf } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { Popover } from '../ui/Popover';
import type { ColumnDef } from './columns';

type Props = { viewId: string; columns: readonly ColumnDef[]; anchor: HTMLElement; onClose: () => void };

export function ColumnsMenu({ viewId, columns, anchor, onClose }: Props) {
  const hidden = useViews((s) => viewOf(s.views, viewId).hidden);
  const hideTransfers = useViews((s) => viewOf(s.views, viewId).hideTransfers);
  const patch = useViews((s) => s.patch);
  const visible = columns.filter((c) => !hidden.includes(c.id)).length;
  const allOn = visible === columns.length;
  const set = (id: string, on: boolean) => {
    patch(viewId, { hidden: on ? hidden.filter((h) => h !== id) : [...hidden, id] });
  };
  return (
    <Popover open anchor={anchor} onClose={onClose} label="Columns" width={280}>
      <div className="pop-h">
        <Icon name="columns" />
        <b>Columns</b>
        <span className="muted">{visible} visible</span>
      </div>
      <div className="pop-s grow">
        <div className="vlist cols-list" role="group" aria-label="Visible columns">
          {columns.map((c) => (
            <label key={c.id} className="vi">
              <input
                type="checkbox"
                checked={!hidden.includes(c.id)}
                onChange={(e) => {
                  set(c.id, e.target.checked);
                }}
              />
              <span className={`tt ${c.type}`}>
                {c.type === 'number' ? '#' : c.type === 'date' ? <Icon name="cal" /> : 'Aa'}
              </span>
              <span className="vt">{c.label}</span>
            </label>
          ))}
        </div>
        <label className="vi rows-opt">
          <input
            type="checkbox"
            checked={!hideTransfers}
            onChange={(e) => {
              patch(viewId, { hideTransfers: !e.target.checked });
            }}
          />
          <span className="vt">Show transfers between my accounts</span>
        </label>
      </div>
      <div className="pop-f">
        <Button
          variant="ghost"
          onClick={() => {
            patch(viewId, { widths: {} });
          }}
        >
          Reset widths
        </Button>
        <Button
          onClick={() => {
            const ids = columns.map((c) => c.id as string);
            patch(viewId, {
              hidden: allOn ? [...new Set([...hidden, ...ids])] : hidden.filter((h) => !ids.includes(h)),
            });
          }}
        >
          {allOn ? 'Hide all' : 'Show all'}
        </Button>
      </div>
    </Popover>
  );
}
