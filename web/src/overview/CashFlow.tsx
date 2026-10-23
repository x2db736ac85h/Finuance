// Cash flow (legacy CHARTS.o_flow): money in above and out below the axis for the months setting's
// range ending at the selected month, with the net as a dot. The selected month is drawn solid;
// clicking another month selects it.

import type { Schemas } from '../api/client';
import { EXPENSE, INCOME } from '../charts/lib/colors';
import { barPath } from '../charts/lib/geom';
import { money } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import type { ChartDef, ChartEnv, ChartParts } from '../charts/types';
import { fmtMonthLong, fmtMonthShort } from '../lib/dates';
import { fmtCompact } from '../lib/format';
import { useTip } from '../ui/Tooltip';
import { monthsControl, negMoney, rateOf, useOverviewParts, type OverviewData } from './data';

type Month = Schemas['MonthTotals'];

const rateText = (m: Month) => {
  const r = rateOf(m);
  return r === null ? '–' : `${r.toFixed(0)}%`;
};

function MonthHit({
  m,
  x,
  y,
  slot,
  h,
  cur,
  env,
}: {
  m: Month;
  x: number;
  y: number;
  slot: number;
  h: number;
  cur: string;
  env: ChartEnv;
}) {
  const net = m.incomeMinor - m.expenseMinor;
  const tip = useTip(
    <TipContent
      title={fmtMonthLong(m.month)}
      rows={[
        [INCOME, 'In', money(m.incomeMinor, cur)],
        [EXPENSE, 'Out', negMoney(m.expenseMinor, cur)],
        ['', 'Net', money(net, cur)],
        ['', 'Savings rate', rateText(m)],
      ]}
      hint="Click to open this month"
    />,
  );
  return (
    <rect
      x={x}
      y={y}
      width={slot}
      height={h}
      fill="transparent"
      data-a=""
      data-month={m.month}
      {...tip}
      onClick={() => {
        env.actions.setOvMonth(m.month);
      }}
    />
  );
}

function Bars({ d, cur, env }: { d: OverviewData; cur: string; env: ChartEnv }) {
  const { w, h, big } = env;
  const ms = d.months;
  const padL = 40;
  const padR = 8;
  const padT = 10;
  const padB = 22;
  const pw = w - padL - padR;
  const ph = h - padT - padB;
  const mi = Math.max(0, ...ms.map((m) => m.incomeMinor));
  const me = Math.max(0, ...ms.map((m) => m.expenseMinor));
  const sc = ph / (mi + me || 1);
  const y0 = padT + mi * sc;
  const slot = pw / ms.length;
  const bw = Math.max(4, Math.min(big ? 56 : 30, slot * 0.55));
  const every = Math.ceil(30 / slot);
  return (
    <Svg w={w} h={h} label="Income, expenses and net by month">
      <line x1={padL} x2={w - padR} y1={padT} y2={padT} style={{ stroke: 'var(--gridl)' }} />
      <line x1={padL} x2={w - padR} y1={padT + ph} y2={padT + ph} style={{ stroke: 'var(--gridl)' }} />
      <text className="ax" x={padL - 6} y={padT + 4} textAnchor="end">
        {mi ? `+${fmtCompact({ amountMinor: mi, currency: cur })}` : ''}
      </text>
      <text className="ax" x={padL - 6} y={padT + ph + 3} textAnchor="end">
        {me ? `−${fmtCompact({ amountMinor: me, currency: cur })}` : ''}
      </text>
      {ms.map((m, i) => {
        const x = padL + i * slot + (slot - bw) / 2;
        const on = m.month === d.month;
        const op = on ? 1 : 0.42;
        const yr = m.month.endsWith('-01') ? ` '${m.month.slice(2, 4)}` : '';
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
            {(i % every === 0 || on) && (
              <text
                className="ax"
                x={padL + i * slot + slot / 2}
                y={h - 6}
                textAnchor="middle"
                style={on ? { fill: 'var(--text)', fontWeight: 600 } : undefined}
              >
                {fmtMonthShort(m.month)}
                {yr}
              </text>
            )}
          </g>
        );
      })}
      {ms.map((m, i) =>
        m.count ? (
          <circle
            key={m.month}
            cx={padL + i * slot + slot / 2}
            cy={y0 - (m.incomeMinor - m.expenseMinor) * sc}
            r={3.5}
            style={{
              fill: 'var(--text)',
              stroke: 'var(--panel)',
              strokeWidth: 2,
              opacity: m.month === d.month ? 1 : 0.7,
            }}
          />
        ) : null,
      )}
      <line x1={padL} x2={w - padR} y1={y0} y2={y0} style={{ stroke: 'var(--axis)' }} />
      {ms.map((m, i) => (
        <MonthHit key={m.month} m={m} x={padL + i * slot} y={padT} slot={slot} h={ph} cur={cur} env={env} />
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
    <span>
      <i className="sw" style={{ background: 'var(--text)', borderRadius: '50%' }} /> Net
    </span>
  </>
);

export function cashFlowParts(d: OverviewData, cur: string, env: ChartEnv): ChartParts {
  return {
    loading: false,
    error: false,
    sub: `last ${d.months.length} months`,
    legend,
    table: {
      cols: ['Month', 'In', 'Out', 'Net', 'Savings rate'],
      num: [false, true, true, true, true],
      rows: [...d.months]
        .reverse()
        .map((m) => [
          fmtMonthLong(m.month),
          money(m.incomeMinor, cur),
          negMoney(m.expenseMinor, cur),
          money(m.incomeMinor - m.expenseMinor, cur),
          rateText(m),
        ]),
    },
    body: <Bars d={d} cur={cur} env={env} />,
  };
}

export const cashFlowChart: ChartDef = {
  key: 'o_flow',
  title: 'Cash flow',
  cfgControls: [monthsControl],
  use: (q, env) => useOverviewParts(q, env, cashFlowParts),
};
