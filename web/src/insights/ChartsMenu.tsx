// The Charts menu (legacy openChartsMenu): show or hide each card and reorder them by dragging or
// with the arrows. The drag carries text/plain, so the file-drop overlay (which only reacts to
// Files) ignores it. Order and visibility are saved in the preferences.

import { useState, type DragEvent } from 'react';

import { CHARTS } from '../charts/registry';
import { cardOrder } from '../stores/prefs';
import { useUi } from '../stores/uiStore';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { Popover } from '../ui/Popover';

const titleOf = (k: string) => CHARTS.find((c) => c.key === k)?.title ?? k;

/** Moves key to index `to` of order (clamped). */
export function moved(order: readonly string[], key: string, to: number): string[] {
  const o = order.filter((k) => k !== key);
  o.splice(Math.max(0, Math.min(o.length, to)), 0, key);
  return o;
}

type Props = { anchor: HTMLElement; onClose: () => void };

export function ChartsMenu({ anchor, onClose }: Props) {
  const stored = useUi((s) => s.cardOrder);
  const cards = useUi((s) => s.cards);
  const order = cardOrder(stored);
  const [drag, setDrag] = useState<string | null>(null);
  const [drop, setDrop] = useState<{ key: string; after: boolean } | null>(null);
  const setOrder = (o: string[]) => {
    useUi.getState().setCardOrder(o);
  };
  const half = (e: DragEvent<HTMLElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    return e.clientY >= r.top + r.height / 2;
  };

  return (
    <Popover open anchor={anchor} onClose={onClose} label="Charts" width={300}>
      <div className="pop-h">
        <Icon name="chart" />
        <b>Charts</b>
        <span className="muted">drag or use arrows to reorder</span>
      </div>
      <div className="pop-s">
        {order.map((k, j) => (
          <div
            key={k}
            className={`co${drag === k ? ' dragging' : ''}${drop?.key === k ? (drop.after ? ' dropa' : ' dropb') : ''}`}
            draggable
            data-k={k}
            onDragStart={(e) => {
              setDrag(k);
              e.dataTransfer.effectAllowed = 'move';
              e.dataTransfer.setData('text/plain', k);
            }}
            onDragOver={(e) => {
              if (!drag) return;
              e.preventDefault();
              setDrop(k === drag ? null : { key: k, after: half(e) });
            }}
            onDrop={(e) => {
              e.preventDefault();
              if (drag && k !== drag) {
                const rest = order.filter((x) => x !== drag);
                setOrder(moved(order, drag, rest.indexOf(k) + (half(e) ? 1 : 0)));
              }
              setDrag(null);
              setDrop(null);
            }}
            onDragEnd={() => {
              setDrag(null);
              setDrop(null);
            }}
          >
            <span className="grip" title="Drag to reorder" aria-hidden="true">
              ⠿
            </span>
            <label className="vi">
              <input
                type="checkbox"
                checked={cards.includes(k)}
                onChange={(e) => {
                  const on = e.target.checked;
                  useUi.getState().setCards(on ? [...cards, k] : cards.filter((x) => x !== k));
                }}
              />
              <span className="vt">{titleOf(k)}</span>
            </label>
            <IconButton
              icon="up"
              size="sm"
              title={`Move ${titleOf(k)} up`}
              disabled={j === 0}
              onClick={() => {
                setOrder(moved(order, k, j - 1));
              }}
            />
            <IconButton
              icon="down"
              size="sm"
              title={`Move ${titleOf(k)} down`}
              disabled={j === order.length - 1}
              onClick={() => {
                setOrder(moved(order, k, j + 1));
              }}
            />
          </div>
        ))}
      </div>
    </Popover>
  );
}
