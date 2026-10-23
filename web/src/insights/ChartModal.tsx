// The expanded chart window (legacy renderModal, §15): the chart at a large size or its table twin,
// the range settings (saved, and applied to the small card too), ←/→ through the cards that were
// visible, Esc or a backdrop click to close. It redraws when its size changes.

import { useEffect, useEffectEvent } from 'react';

import { chartOf } from '../charts/registry';
import type { CfgControl, ChartDef, Twin } from '../charts/types';
import { Empty } from '../charts/types';
import { useUi } from '../stores/uiStore';
import type { ViewQuery } from '../table/context';
import { IconButton } from '../ui/IconButton';
import { useLayers } from '../ui/layers';
import { Modal } from '../ui/Modal';
import { hideTip } from '../ui/Tooltip';
import { useChartEnv } from './useChartEnv';
import { drawable, useSize } from './useSize';

export type ModalState = { key: string; list: string[]; mode: 'chart' | 'table' };

type Props = {
  viewId: string;
  q: ViewQuery;
  state: ModalState;
  onChange: (s: ModalState | null) => void;
  /** Where the window finds its charts: the pane's by default, the overview passes its own. */
  lookup?: (key: string) => ChartDef | undefined;
};

/** The table twin (legacy tableHtml). */
export function TwinTable({ t }: { t: Twin }) {
  const cls = (j: number) => (t.num[j] ? 'r' : undefined);
  return (
    <div className="mt-wrap">
      <table className="mt">
        <thead>
          <tr>
            {t.cols.map((c, j) => (
              <th key={c} className={cls(j)} scope="col">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {t.rows.map((row, i) => (
            // rows are positional (the twin of a drawing), so the index is their identity
            <tr key={i}>
              {row.map((v, j) => (
                <td key={j} className={cls(j)}>
                  {v}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** A range setting's select (legacy cfgCtl): numbers stay numbers in the saved settings. */
function CfgSelect({ c }: { c: CfgControl }) {
  const value = useUi((s) => s.cfg[c.key]);
  return (
    <label className="cfg">
      {c.label}
      <select
        className="sel"
        value={String(value)}
        onChange={(e) => {
          const v = e.target.value;
          useUi.getState().setCfg({ [c.key]: /^-?\d+$/.test(v) ? Number(v) : v });
        }}
      >
        {c.options.map(([v, label]) => (
          <option key={String(v)} value={String(v)}>
            {label}
          </option>
        ))}
      </select>
    </label>
  );
}

function useModalChart(def: ChartDef, viewId: string, q: ViewQuery, mode: ModalState['mode']) {
  const [box, size] = useSize();
  const env = useChartEnv(viewId, size.w, size.h, true);
  const parts = def.use(q, env);
  return {
    sub: parts.sub,
    legend: parts.legend,
    body: (
      <div className="modal-b">
        <div className="modal-chart" ref={box}>
          {mode === 'table' ? (
            parts.table ? (
              <TwinTable t={parts.table} />
            ) : (
              <Empty>No table view for this card</Empty>
            )
          ) : (
            drawable(size) && parts.body
          )}
        </div>
      </div>
    ),
  };
}

export function ChartModal({ viewId, q, state, onChange, lookup = chartOf }: Props) {
  const def = lookup(state.key);
  const step = (d: number) => {
    const n = state.list.length;
    if (n < 2) return;
    const i = state.list.indexOf(state.key);
    hideTip();
    onChange({ ...state, key: state.list[(i + d + n) % n] ?? state.key });
  };
  const arrows = useEffectEvent((e: KeyboardEvent) => {
    if (useLayers.getState().pop > 0 || e.target instanceof HTMLSelectElement || e.target instanceof HTMLInputElement)
      return;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      step(e.key === 'ArrowLeft' ? -1 : 1);
    }
  });
  useEffect(() => {
    const l = (e: KeyboardEvent) => {
      arrows(e);
    };
    document.addEventListener('keydown', l);
    return () => {
      document.removeEventListener('keydown', l);
    };
  }, []);
  if (!def) return null;
  return <Window key={def.key} def={def} viewId={viewId} q={q} state={state} onChange={onChange} step={step} />;
}

function Window({ def, viewId, q, state, onChange, step }: Props & { def: ChartDef; step: (d: number) => void }) {
  const { sub, legend, body } = useModalChart(def, viewId, q, state.mode);
  const close = () => {
    hideTip();
    onChange(null);
  };
  return (
    <Modal
      open
      onClose={close}
      title={def.title}
      sub={sub}
      actions={
        <>
          <span className="ch-x">{legend}</span>
          <span className="m-cfg">
            {def.cfgControls?.map((c) => (
              <CfgSelect key={c.key} c={c} />
            ))}
          </span>
          <span className="seg" role="group" aria-label="View">
            {(['chart', 'table'] as const).map((m) => (
              <button
                key={m}
                type="button"
                className={state.mode === m ? 'on' : ''}
                aria-pressed={state.mode === m}
                onClick={() => {
                  onChange({ ...state, mode: m });
                }}
              >
                {m === 'chart' ? 'Chart' : 'Table'}
              </button>
            ))}
          </span>
          <IconButton
            icon="left"
            title="Previous chart (←)"
            disabled={state.list.length < 2}
            onClick={() => {
              step(-1);
            }}
          />
          <IconButton
            icon="right"
            title="Next chart (→)"
            disabled={state.list.length < 2}
            onClick={() => {
              step(1);
            }}
          />
        </>
      }
    >
      {body}
    </Modal>
  );
}
