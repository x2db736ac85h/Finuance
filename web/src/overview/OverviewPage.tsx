// The overview mode of a view (PLAN §7.9, legacy renderOverview): the month selector with ←/→, the
// notes (month in progress, filters from the list), the KPI row and the twelve cards on a 12-column
// container grid. Every card but the highlights opens in the chart window with its table and the
// range setting.

import { useState } from 'react';

import { useOverview } from '../api/hooks/overview';
import { Empty } from '../charts/types';
import { ChartModal, type ModalState } from '../insights/ChartModal';
import { addMonths, fmtDayMedium, fmtMonthLong } from '../lib/dates';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { cleared } from '../table/chips';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { hideTip } from '../ui/Tooltip';
import { useOverviewQuery, type OverviewData } from './data';
import { Kpis } from './Kpis';
import { OvCard } from './OvCard';
import { OV_CHARTS, ovChartOf } from './registry';

/** The cards the chart window steps through (legacy: all but the highlights). */
const EXPANDABLE = OV_CHARTS.filter((c) => c.expandable !== false).map((c) => c.key);

/** Every month from the last with data back to the first (the selector's options). */
export function monthsOf(d: Pick<OverviewData, 'firstMonth' | 'lastMonth'>): string[] {
  const out: string[] = [];
  for (let m = d.lastMonth; m >= d.firstMonth && out.length < 1200; m = addMonths(m, -1)) out.push(m);
  return out;
}

function MonthPicker({ d, onPick }: { d: OverviewData | undefined; onPick: (m: string) => void }) {
  const options = d ? monthsOf(d) : [];
  return (
    <div className="per">
      <IconButton
        icon="left"
        title="Previous month"
        disabled={!d || d.month <= d.firstMonth}
        onClick={() => {
          if (d) onPick(addMonths(d.month, -1));
        }}
      />
      <select
        className="sel"
        aria-label="Month"
        title="Month"
        value={d?.month ?? ''}
        disabled={!d}
        onChange={(e) => {
          onPick(e.target.value);
        }}
      >
        {options.map((m) => (
          <option key={m} value={m}>
            {fmtMonthLong(m)}
          </option>
        ))}
      </select>
      <IconButton
        icon="right"
        title="Next month"
        disabled={!d || d.month >= d.lastMonth}
        onClick={() => {
          if (d) onPick(addMonths(d.month, 1));
        }}
      />
    </div>
  );
}

export function OverviewPage({ viewId }: { viewId: string }) {
  const q = useOverviewQuery(viewId);
  const ovMonth = useViews((s) => viewOf(s.views, viewId).ovMonth);
  const search = useViews((s) => viewOf(s.views, viewId).search);
  const filters = useViews((s) => viewOf(s.views, viewId).filters);
  const months = useUi((s) => s.cfg.ovMonths);
  const patch = useViews((s) => s.patch);
  const ov = useOverview(q.context, q.ctxKey, ovMonth, months);
  const [modal, setModal] = useState<ModalState | null>(null);
  const d = ov.data?.data;
  const cur = ov.data?.currency ?? '';
  const nf =
    Object.values(filters).filter((f) => f && (f.cond.trim() || f.excluded.length)).length + (search.trim() ? 1 : 0);
  const clear = () => {
    patch(viewId, cleared(viewId));
  };

  return (
    <section className="overview" aria-label="Overview" aria-busy={ov.isFetching}>
      <div className="ov-h">
        <span className="ttl">
          <Icon name="dash" />
          <span>Overview</span>
        </span>
        <MonthPicker
          d={d}
          onPick={(m) => {
            hideTip();
            patch(viewId, { ovMonth: m });
          }}
        />
        <span className="ov-note">
          {d?.partial && (
            <span>
              <Icon name="cal" /> Month in progress — data until {fmtDayMedium(d.dataMax)}
            </span>
          )}
          {nf > 0 && (
            <span>
              <Icon name="filter" /> {nf} filter{nf > 1 ? 's' : ''} from the list applied{' '}
              <button type="button" className="link" onClick={clear}>
                Clear
              </button>
            </span>
          )}
        </span>
        <span className="spacer" />
      </div>
      <div className="ov-scroll">
        <div className="ov-grid" onPointerLeave={hideTip}>
          {d ? (
            <>
              <Kpis d={d} cur={cur} />
              {OV_CHARTS.map((def) => (
                <OvCard
                  key={def.key}
                  def={def}
                  viewId={viewId}
                  q={q}
                  onExpand={() => {
                    hideTip();
                    setModal({ key: def.key, list: EXPANDABLE, mode: 'chart' });
                  }}
                />
              ))}
            </>
          ) : ov.data ? (
            <div className="ov-empty">
              <Empty>
                {nf > 0 ? (
                  <span>
                    No transactions match the filters from the list.{' '}
                    <button type="button" className="link" onClick={clear}>
                      Clear filters
                    </button>
                  </span>
                ) : (
                  'The overview needs transactions with a date and an amount.'
                )}
              </Empty>
            </div>
          ) : ov.isError ? (
            <div className="ov-empty">
              <Empty>Could not load the overview — reload the page to try again.</Empty>
            </div>
          ) : null}
        </div>
      </div>
      {modal && <ChartModal viewId={viewId} q={q} state={modal} onChange={setModal} lookup={ovChartOf} />}
    </section>
  );
}
