// Summary (legacy CHARTS.sum): spent, income, net with the savings rate, the average per day and
// the largest expense, with the spending change against the previous period. Numbers show without
// currency so they fit narrow tiles; the currency is in the labels.

import { useSummary, type InsightSummary } from '../api/hooks/insights';
import { fmtDay } from '../lib/dates';
import { fmtShare } from '../lib/format';
import type { ViewQuery } from '../table/context';
import { money, plain } from './lib/money';
import { partsOf, type ChartDef, type ChartEnv, type ChartParts } from './types';

/** The delta caption (legacy: same as previous below half a percent). */
export function deltaText(s: InsightSummary): { text: string; dir?: 'up' | 'dn' } {
  if (!s.period) return { text: 'all time' };
  const d = s.spentDelta;
  if (d === undefined) return { text: 'no spending in previous period' };
  if (Math.abs(d) < 0.005) return { text: 'same as previous' };
  return { text: `${d > 0 ? '▲' : '▼'} ${Math.abs(d * 100).toFixed(0)}%`, dir: d > 0 ? 'up' : 'dn' };
}

export function summaryParts(s: InsightSummary, env: Pick<ChartEnv, 'actions'>): ChartParts {
  const cur = s.currency;
  const cl = cur ? ` · ${cur}` : '';
  const delta = deltaText(s);
  const big = s.largestExpense;
  const net = s.netMinor;
  const days = `over ${s.days} day${s.days > 1 ? 's' : ''}`;
  return {
    loading: false,
    error: false,
    sub: s.period ? s.period.label : 'All time',
    table: {
      cols: ['Metric', 'Value'],
      num: [false, true],
      rows: [
        ['Spent', money(s.spentMinor, cur)],
        ['Income', money(s.incomeMinor, cur)],
        ['Net', money(net, cur)],
        ['Avg / day', money(s.avgPerDayMinor, cur)],
        ['Days', String(s.days)],
        ['Operations', String(s.count)],
        ['Largest expense', big ? money(big.amountMinor, cur) : '–'],
      ],
    },
    body: (
      <div className="kpis">
        <div className="kpi">
          <span className="l">Spent{cl}</span>
          <span className="n" title={money(s.spentMinor, cur)}>
            {plain(s.spentMinor, cur)}
          </span>
          <span className="d">
            {delta.dir ? (
              <>
                <span className={delta.dir}>{delta.text}</span> vs previous
              </>
            ) : (
              delta.text
            )}
          </span>
        </div>
        <div className="kpi">
          <span className="l">Income{cl}</span>
          <span className="n" title={money(s.incomeMinor, cur)}>
            {plain(s.incomeMinor, cur)}
          </span>
          <span className="d">{s.count} operations</span>
        </div>
        <div className="kpi">
          <span className="l">Net{cl}</span>
          <span className={`n${net < 0 ? ' neg' : ''}`}>
            {net > 0 ? '+' : ''}
            {plain(net, cur)}
          </span>
          <span className="d">
            {s.savingsRate !== undefined ? `saved ${fmtShare(s.savingsRate)} of income` : 'no income'}
          </span>
        </div>
        <div className="kpi">
          <span className="l">Avg / day{cl}</span>
          <span className="n">{plain(s.avgPerDayMinor, cur)}</span>
          <span className="d">{days}</span>
        </div>
        <div className="kpi" style={{ gridColumn: 'span 2' }}>
          <span className="l">Largest expense{cl}</span>
          {big ? (
            <button
              type="button"
              className="n kpi-link"
              title="Show this transaction in the list"
              onClick={() => {
                env.actions.locate(big.id);
              }}
            >
              {plain(big.amountMinor, cur)}
            </button>
          ) : (
            <span className="n">–</span>
          )}
          <span className="d">{big ? `${big.payee} · ${fmtDay(big.bookedOn)}` : ''}</span>
        </div>
      </div>
    ),
  };
}

export const summaryChart: ChartDef = {
  key: 'sum',
  title: 'Summary',
  use: (q: ViewQuery, env: ChartEnv) => partsOf(useSummary(q.context, q.ctxKey), (s) => summaryParts(s, env)),
};
