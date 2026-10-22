// Cash flow by month (legacy CHARTS.flow): diverging bars, income up and expenses down, over the
// whole history of the filtered rows; months in the period are highlighted, the others dimmed.
// Clicking a month focuses the period on it (clicking it again shows all time). The range setting
// (fit to width, 6–36 months, all) cuts the list here; it never needs a new request.

import { useCashflow, type InsightCashflow } from '../api/hooks/insights';
import { fmtMonthLong, fmtMonthShort } from '../lib/dates';
import { fmtCompact } from '../lib/format';
import type { ChartCfg } from '../stores/prefs';
import type { ViewQuery } from '../table/context';
import { useTip } from '../ui/Tooltip';
import { EXPENSE, INCOME } from './lib/colors';
import { barPath } from './lib/geom';
import { money } from './lib/money';
import { Svg } from './lib/Svg';
import { TipContent } from './lib/Tip';
import { Empty, partsOf, type ChartDef, type ChartEnv, type ChartParts, type CfgControl } from './types';

type Month = InsightCashflow['months'][number];

const PAD = { l: 38, r: 8, t: 8, b: 20 };

/**
 * The months shown: the last N of the range setting, never more than one per 5 units of width
 * (legacy: fit = one per 16 units, 9 in the modal).
 */
export function visibleMonths(
  months: readonly Month[],
  flowMonths: ChartCfg['flowMonths'],
  w: number,
  big: boolean,
): Month[] {
  const pw = w - PAD.l - PAD.r;
  const fit = Math.max(1, Math.floor(pw / (big ? 9 : 16)));
  const cap = Math.max(1, Math.floor(pw / 5));
  const n = flowMonths === 'auto' ? fit : flowMonths === 'all' ? months.length : flowMonths;
  return months.slice(-Math.min(cap, n));
}

function MonthHit({
  m,
  x,
  slot,
  ph,
  cur,
  env,
}: {
  m: Month;
  x: number;
  slot: number;
  ph: number;
  cur: string;
  env: ChartEnv;
}) {
  const on = env.period === `m:${m.month}`;
  const tip = useTip(
    <TipContent
      title={fmtMonthLong(m.month)}
      rows={[
        [INCOME, 'In', money(m.incomeMinor, cur)],
        [EXPENSE, 'Out', money(-m.expenseMinor, cur)],
        ['', 'Net', money(m.incomeMinor - m.expenseMinor, cur)],
        ['', 'Operations', String(m.count)],
      ]}
      hint={on ? 'Click to show all time' : 'Click to focus this month'}
    />,
  );
  return (
    <rect
      x={x}
      y={PAD.t}
      width={slot}
      height={ph}
      fill="transparent"
      data-a=""
      data-month={m.month}
      {...tip}
      onClick={() => {
        env.actions.setPeriod(on ? 'all' : `m:${m.month}`);
      }}
    />
  );
}

function Bars({ months, cur, env }: { months: Month[]; cur: string; env: ChartEnv }) {
  const { w, h, big } = env;
  const pw = w - PAD.l - PAD.r;
  const ph = h - PAD.t - PAD.b;
  const mi = Math.max(0, ...months.map((m) => m.incomeMinor));
  const me = Math.max(0, ...months.map((m) => m.expenseMinor));
  const sc = ph / (mi + me || 1);
  const y0 = PAD.t + mi * sc;
  const slot = pw / months.length;
  const bw = Math.max(2, Math.min(big ? 48 : 26, slot * 0.6));
  const every = Math.ceil(34 / slot);
  return (
    <Svg w={w} h={h} label="Monthly income and expenses">
      <line x1={PAD.l} x2={w - PAD.r} y1={PAD.t} y2={PAD.t} style={{ stroke: 'var(--gridl)' }} />
      <line x1={PAD.l} x2={w - PAD.r} y1={PAD.t + ph} y2={PAD.t + ph} style={{ stroke: 'var(--gridl)' }} />
      <text className="ax" x={PAD.l - 6} y={PAD.t + 4} textAnchor="end">
        {mi ? `+${fmtCompact({ amountMinor: mi, currency: cur })}` : ''}
      </text>
      <text className="ax" x={PAD.l - 6} y={PAD.t + ph + 3} textAnchor="end">
        {me ? `−${fmtCompact({ amountMinor: me, currency: cur })}` : ''}
      </text>
      {months.map((m, i) => {
        const x = PAD.l + i * slot + (slot - bw) / 2;
        const op = m.inPeriod ? 1 : 0.3;
        const label = i % every === 0;
        const yr = m.month.endsWith('-01') || i === 0 ? ` '${m.month.slice(2, 4)}` : '';
        return (
          <g key={m.month}>
            <path
              d={barPath(x, y0 - m.incomeMinor * sc, bw, m.incomeMinor * sc, true)}
              style={{ fill: INCOME, opacity: op }}
            />
            <path
              d={barPath(x, y0 + 1, bw, Math.max(0, m.expenseMinor * sc - 1), false)}
              style={{ fill: EXPENSE, opacity: op }}
            />
            {label && (
              <text className="ax" x={PAD.l + i * slot + slot / 2} y={h - 5} textAnchor="middle">
                {fmtMonthShort(m.month)}
                {yr}
              </text>
            )}
          </g>
        );
      })}
      <line x1={PAD.l} x2={w - PAD.r} y1={y0} y2={y0} style={{ stroke: 'var(--axis)' }} />
      {months.map((m, i) => (
        <MonthHit key={m.month} m={m} x={PAD.l + i * slot} slot={slot} ph={ph} cur={cur} env={env} />
      ))}
    </Svg>
  );
}

const legend = (
  <>
    <span>
      <i className="sw" style={{ background: INCOME }} /> In
    </span>
    <span>
      <i className="sw" style={{ background: EXPENSE }} /> Out
    </span>
  </>
);

export function cashflowParts(d: InsightCashflow, env: ChartEnv): ChartParts {
  if (!d.months.length)
    return { loading: false, error: false, sub: '', legend, body: <Empty>No dated amounts</Empty>, table: null };
  const months = visibleMonths(d.months, env.cfg.flowMonths, env.w, env.big);
  const cur = d.currency;
  return {
    loading: false,
    error: false,
    sub: `${months.length} months`,
    legend,
    table: {
      cols: ['Month', 'In', 'Out', 'Net', 'Operations'],
      num: [false, true, true, true, true],
      rows: [...months]
        .reverse()
        .map((m) => [
          fmtMonthLong(m.month),
          money(m.incomeMinor, cur),
          money(-m.expenseMinor, cur),
          money(m.incomeMinor - m.expenseMinor, cur),
          String(m.count),
        ]),
    },
    body: <Bars months={months} cur={cur} env={env} />,
  };
}

export const flowControls: readonly CfgControl[] = [
  {
    key: 'flowMonths',
    label: 'Range',
    options: [
      ['auto', 'Fit to width'],
      [6, 'Last 6 months'],
      [12, 'Last 12 months'],
      [24, 'Last 24 months'],
      [36, 'Last 36 months'],
      ['all', 'All months'],
    ],
  },
];

export const cashflowChart: ChartDef = {
  key: 'flow',
  title: 'Cash flow by month',
  cfgControls: flowControls,
  use: (q: ViewQuery, env: ChartEnv) => partsOf(useCashflow(q.context, q.ctxKey), (d) => cashflowParts(d, env)),
};
