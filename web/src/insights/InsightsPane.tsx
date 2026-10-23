// The insights pane (PLAN task 11.5, legacy #viz): the period picker, the Charts menu and a
// horizontal row of chart cards above the list. The height is resizable by dragging the bottom
// edge (170 px … 75 % of the window; double-click resets) and saved; the pane collapses with the
// chevron (or the title-bar toggle).

import { useRef, useState, type CSSProperties, type PointerEvent } from 'react';

import { chartOf } from '../charts/registry';
import { Empty } from '../charts/types';
import { cardOrder } from '../stores/prefs';
import { useUi } from '../stores/uiStore';
import type { ViewQuery } from '../table/context';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { hideTip } from '../ui/Tooltip';
import { ChartCard } from './ChartCard';
import { ChartModal, type ModalState } from './ChartModal';
import { ChartsMenu } from './ChartsMenu';
import { PeriodPicker } from './PeriodPicker';

type Props = {
  viewId: string;
  q: ViewQuery;
  resolved: { from: string; to: string; label: string } | undefined;
  dataRange: { min: string; max: string } | undefined;
  /** The list's first window has arrived; the charts load after it so the table paints first. */
  ready: boolean;
};

/** The pane height for a drag (legacy: 170 px … 75 % of the window height). */
export function clampHeight(px: number, windowHeight: number): number {
  return Math.max(170, Math.min(windowHeight * 0.75, px));
}

export function InsightsPane({ viewId, q, resolved, dataRange, ready }: Props) {
  const height = useUi((s) => s.paneHeight);
  const cards = useUi((s) => s.cards);
  const stored = useUi((s) => s.cardOrder);
  const [menu, setMenu] = useState<HTMLElement | null>(null);
  const [modal, setModal] = useState<ModalState | null>(null);
  const [resizing, setResizing] = useState(false);
  const pane = useRef<HTMLDivElement>(null);
  const list = cardOrder(stored).filter((k) => cards.includes(k));

  // legacy: mousedown on the edge, then move/up on the document
  const startResize = (e: PointerEvent<HTMLDivElement>) => {
    const el = pane.current;
    if (!el || e.button !== 0) return;
    e.preventDefault();
    const y0 = e.clientY;
    const h0 = el.offsetHeight;
    setResizing(true);
    const move = (ev: globalThis.PointerEvent) => {
      el.style.setProperty('--viz-h', `${clampHeight(h0 + ev.clientY - y0, window.innerHeight)}px`);
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', up);
      setResizing(false);
      useUi.getState().setPaneHeight(el.style.getPropertyValue('--viz-h'));
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', up);
  };

  return (
    <div
      className="viz"
      ref={pane}
      style={height ? ({ '--viz-h': height } as CSSProperties) : undefined}
      onScroll={hideTip}
    >
      <div className="viz-h">
        <span className="ttl">
          <Icon name="chart" />
          <span>Insights</span>
        </span>
        <PeriodPicker viewId={viewId} resolved={resolved} dataRange={dataRange} />
        <span className="spacer" />
        <Button
          size="sm"
          icon="chart"
          title="Show / hide charts"
          aria-expanded={!!menu}
          onClick={(e) => {
            setMenu(menu ? null : e.currentTarget);
          }}
        >
          <span className="bl">Charts</span>
        </Button>
        <IconButton
          icon="chevup"
          title="Hide insights"
          onClick={() => {
            useUi.getState().toggleViz();
          }}
        />
      </div>
      <div className="cards" onPointerLeave={hideTip}>
        {list.length ? (
          list.map((k) => {
            const def = chartOf(k);
            return (
              def && (
                <ChartCard
                  key={k}
                  def={def}
                  viewId={viewId}
                  q={q}
                  ready={ready}
                  onExpand={() => {
                    hideTip();
                    setModal({ key: k, list, mode: 'chart' });
                  }}
                />
              )
            );
          })
        ) : (
          <Empty>
            <span>
              All charts are hidden — turn some on in <b>Charts</b>.
            </span>
          </Empty>
        )}
      </div>
      <div
        className={`viz-rz${resizing ? ' on' : ''}`}
        title="Drag to resize · double-click to reset"
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize insights"
        onPointerDown={startResize}
        onDoubleClick={() => {
          pane.current?.style.removeProperty('--viz-h');
          useUi.getState().setPaneHeight('');
        }}
      />
      {menu && (
        <ChartsMenu
          anchor={menu}
          onClose={() => {
            setMenu(null);
          }}
        />
      )}
      {modal && <ChartModal viewId={viewId} q={q} state={modal} onChange={setModal} />}
    </div>
  );
}
