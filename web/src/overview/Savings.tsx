// Savings rate (legacy CHARTS.o_savings): the share of income kept each month of the range, capped
// at ±100 %, with the average as a line. Clicking a month selects it.

import type { Schemas } from '../api/client';
import { EXPENSE, PRIMARY } from '../charts/lib/colors';
import { barPath } from '../charts/lib/geom';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import { Empty, type ChartDef, type ChartEnv, type ChartParts } from '../charts/types';
import { fmtMonthLong, fmtMonthShort } from '../lib/dates';
import { useTip } from '../ui/Tooltip';
import { monthsControl, rateOf, useOverviewParts, type OverviewData } from './data';

type Month = Schemas['MonthTotals'];

/** Each month's rate in percent, capped at ±100; null without income. */
export const savingsRates = (months: readonly Month[]) =>
  months.map((m) => {
    const r = rateOf(m);
    return r === null ? null : Math.max(-100, Math.min(100, r));
  });

function Hit({
  m,
  v,
  x,
  y,
  slot,
  h,
  env,
}: {
  m: Month;
  v: number | null;
  x: number;
  y: number;
  slot: number;
  h: number;
  env: ChartEnv;
}) {
  const tip = useTip(
    <TipContent
      title={fmtMonthLong(m.month)}
      rows={[['', 'Savings rate', v === null ? 'no income' : `${v.toFixed(0)}%`]]}
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

function Bars({ d, vals, avg, env }: { d: OverviewData; vals: (number | null)[]; avg: number; env: ChartEnv }) {
  const { w, h, big } = env;
  const ms = d.months;
  const ok = vals.filter((v): v is number => v !== null);
  const padL = 34;
  const padR = 6;
  const padT = 10;
  const padB = 20;
  const pw = w - padL - padR;
  const ph = h - padT - padB;
  const hi = Math.max(10, ...ok);
  const lo = Math.min(0, ...ok);
  const Y = (v: number) => padT + ((hi - v) / (hi - lo)) * ph;
  const y0 = Y(0);
  const slot = pw / ms.length;
  const bw = Math.max(3, Math.min(big ? 50 : 22, slot * 0.6));
  const every = Math.ceil(26 / slot);
  return (
    <Svg w={w} h={h} label="Savings rate by month">
      <text className="ax" x={padL - 5} y={padT + 4} textAnchor="end">
        {hi.toFixed(0)}%
      </text>
      {lo < 0 && (
        <text className="ax" x={padL - 5} y={padT + ph + 3} textAnchor="end">
          {lo.toFixed(0)}%
        </text>
      )}
      {ms.map((m, i) => {
        const v = vals[i] ?? null;
        const x = padL + i * slot + (slot - bw) / 2;
        const on = m.month === d.month;
        return (
          <g key={m.month}>
            {v !== null && (
              <path
                d={barPath(x, v >= 0 ? Y(v) : y0, bw, Math.abs(Y(v) - y0), v >= 0)}
                style={{ fill: v >= 0 ? PRIMARY : EXPENSE, opacity: on ? 1 : 0.45 }}
              />
            )}
            {(i % every === 0 || on) && (
              <text
                className="ax"
                x={padL + i * slot + slot / 2}
                y={h - 5}
                textAnchor="middle"
                style={on ? { fill: 'var(--text)', fontWeight: 600 } : undefined}
              >
                {fmtMonthShort(m.month).slice(0, big ? 3 : 1)}
              </text>
            )}
          </g>
        );
      })}
      <line x1={padL} x2={w - padR} y1={y0} y2={y0} style={{ stroke: 'var(--axis)' }} />
      <line
        x1={padL}
        x2={w - padR}
        y1={Y(avg)}
        y2={Y(avg)}
        style={{ stroke: 'var(--text-2)', strokeWidth: 1, opacity: 0.7 }}
      />
      {ms.map((m, i) => (
        <Hit key={m.month} m={m} v={vals[i] ?? null} x={padL + i * slot} y={padT} slot={slot} h={ph} env={env} />
      ))}
    </Svg>
  );
}

export function savingsParts(d: OverviewData, _cur: string, env: ChartEnv): ChartParts {
  const vals = savingsRates(d.months);
  const ok = vals.filter((v): v is number => v !== null);
  if (!ok.length)
    return { loading: false, error: false, sub: '', body: <Empty>No income recorded</Empty>, table: null };
  const avg = ok.reduce((a, b) => a + b, 0) / ok.length;
  return {
    loading: false,
    error: false,
    sub: `avg ${avg.toFixed(0)}%`,
    legend: (
      <span>
        <i className="sw ln" style={{ background: 'var(--text-2)' }} /> average
      </span>
    ),
    table: {
      cols: ['Month', 'Savings rate'],
      num: [false, true],
      rows: d.months
        .map((m, i) => [fmtMonthLong(m.month), vals[i] == null ? '–' : `${(vals[i] ?? 0).toFixed(1)}%`])
        .reverse(),
    },
    body: <Bars d={d} vals={vals} avg={avg} env={env} />,
  };
}

export const savingsChart: ChartDef = {
  key: 'o_savings',
  title: 'Savings rate',
  cfgControls: [monthsControl],
  use: (q, env) => useOverviewParts(q, env, savingsParts),
};
