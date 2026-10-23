// Spending by weekday (legacy CHARTS.o_week): the average spending per day of each weekday over the
// selected month and the two before it (up to the last day with data); the highest is emphasised.

import { PRIMARY } from '../charts/lib/colors';
import { barPath } from '../charts/lib/geom';
import { divRound, money } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import type { ChartDef, ChartEnv, ChartParts } from '../charts/types';
import { addDays, fmtWeekday } from '../lib/dates';
import { fmtCompact } from '../lib/format';
import { useTip } from '../ui/Tooltip';
import { useOverviewParts, type OverviewData } from './data';

/** Weekday names, Monday first (2026-01-05 is a Monday; legacy fmtWd). */
export const WEEKDAYS = Array.from({ length: 7 }, (_, j) => fmtWeekday(addDays('2026-01-05', j)));

/** Average per day of each weekday: exact minor-unit sums divided and rounded once (ALLOWED_DIFFS 13). */
export const weekdayAverages = (d: OverviewData) => d.weekday.map((x) => (x.days ? divRound(x.totalMinor, x.days) : 0));

function Hit({
  j,
  x,
  y,
  slot,
  h,
  avg,
  total,
  cur,
}: {
  j: number;
  x: number;
  y: number;
  slot: number;
  h: number;
  avg: number;
  total: number;
  cur: string;
}) {
  const tip = useTip(
    <TipContent
      title={WEEKDAYS[j] ?? ''}
      rows={[
        ['', 'Avg spend', money(avg, cur)],
        ['', 'Total (3 mo)', money(total, cur)],
      ]}
    />,
  );
  return <rect x={x} y={y} width={slot} height={h} fill="transparent" {...tip} />;
}

function Bars({ d, cur, env }: { d: OverviewData; cur: string; env: ChartEnv }) {
  const { w, h, big } = env;
  const avg = weekdayAverages(d);
  const mx = Math.max(...avg, 1);
  const mi = avg.indexOf(Math.max(...avg));
  const padL = 6;
  const padR = 6;
  const padT = 16;
  const padB = 20;
  const pw = w - padL - padR;
  const ph = h - padT - padB;
  const slot = pw / 7;
  const bw = Math.max(6, Math.min(big ? 70 : 26, slot * 0.6));
  return (
    <Svg w={w} h={h} label="Average spending by weekday">
      {avg.map((v, j) => {
        const x = padL + j * slot + (slot - bw) / 2;
        const bh = (v / mx) * ph;
        const name = WEEKDAYS[j] ?? '';
        return (
          // weekdays are positional (Monday first)
          <g key={j}>
            <path
              d={barPath(x, padT + ph - bh, bw, bh, true)}
              style={{ fill: j === mi ? PRIMARY : 'var(--c-other)' }}
            />
            <text className="ax" x={x + bw / 2} y={h - 5} textAnchor="middle">
              {name.slice(0, big ? 3 : 2)}
            </text>
            {j === mi && (
              <text
                x={x + bw / 2}
                y={padT + ph - bh - 4}
                textAnchor="middle"
                style={{ font: '600 10px var(--sans)', fill: 'var(--text)' }}
              >
                {fmtCompact({ amountMinor: v, currency: cur })}
              </text>
            )}
          </g>
        );
      })}
      <line x1={padL} x2={w - padR} y1={padT + ph} y2={padT + ph} style={{ stroke: 'var(--axis)' }} />
      {avg.map((v, j) => (
        <Hit
          key={j}
          j={j}
          x={padL + j * slot}
          y={padT}
          slot={slot}
          h={ph}
          avg={v}
          total={d.weekday[j]?.totalMinor ?? 0}
          cur={cur}
        />
      ))}
    </Svg>
  );
}

export function weekdayParts(d: OverviewData, cur: string, env: ChartEnv): ChartParts {
  const avg = weekdayAverages(d);
  return {
    loading: false,
    error: false,
    sub: 'avg / day · 3 months',
    table: {
      cols: ['Weekday', 'Avg per day', 'Total'],
      num: [false, true, true],
      rows: avg.map((v, j) => [WEEKDAYS[j] ?? '', money(v, cur), money(d.weekday[j]?.totalMinor ?? 0, cur)]),
    },
    body: <Bars d={d} cur={cur} env={env} />,
  };
}

export const weekdayChart: ChartDef = {
  key: 'o_week',
  title: 'Spending by weekday',
  use: (q, env) => useOverviewParts(q, env, weekdayParts),
};
