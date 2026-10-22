// Daily spending (legacy CHARTS.heat): a GitHub-style calendar with Monday-first weeks and a
// sequential ramp binned by quintiles. Cells fit the height and the width; when the range is too
// wide the most recent weeks show. Clicking a day shows that day in the list.

import { useDaily, type InsightDaily } from '../api/hooks/insights';
import { addDays, daysBetween, fmtDayLong, fmtMonthShort, fmtWeekday, weekdayMon } from '../lib/dates';
import type { ViewQuery } from '../table/context';
import { useTip } from '../ui/Tooltip';
import { heatColor } from './lib/colors';
import { heatBin, quintiles } from './lib/geom';
import { money } from './lib/money';
import { Svg } from './lib/Svg';
import { TipContent } from './lib/Tip';
import { Empty, partsOf, type CfgControl, type ChartDef, type ChartEnv, type ChartParts } from './types';

const PAD = { l: 26, t: 14, b: 6 };

export type HeatLayout = { start: string; weeks: number; cs: number; st: number; oy: number };

/**
 * The calendar's geometry (legacy heat): weeks start on the Monday on or before from; the cell size
 * fits 7 rows in the height and every week in the width (7–16 units, 34 in the modal); weeks that
 * still don't fit are dropped from the start. The grid is centred vertically.
 */
export function heatLayout(from: string, to: string, w: number, h: number, big: boolean): HeatLayout {
  let start = addDays(from, -weekdayMon(from));
  let weeks = Math.ceil(daysBetween(start, to) / 7);
  const cs = Math.max(
    7,
    Math.min(big ? 34 : 16, Math.floor((h - PAD.t - PAD.b) / 7) - 2, Math.floor((w - PAD.l - 10) / weeks) - 2),
  );
  const st = cs + 2;
  const maxW = Math.max(1, Math.floor((w - PAD.l - 10) / st));
  if (weeks > maxW) {
    start = addDays(start, (weeks - maxW) * 7);
    weeks = maxW;
  }
  return { start, weeks, cs, st, oy: Math.max(0, (h - PAD.t - PAD.b - 7 * st) / 2) };
}

function Cell(props: {
  d: string;
  x: number;
  y: number;
  cs: number;
  v: number;
  n: number;
  bin: number;
  cur: string;
  env: ChartEnv;
}) {
  const { d, x, y, cs, v, n, bin, cur, env } = props;
  const tip = useTip(
    <TipContent
      title={fmtDayLong(d)}
      rows={[
        ['', 'Spent', money(v, cur)],
        ['', 'Operations', String(n)],
      ]}
      hint="Click to show this day"
    />,
  );
  return (
    <rect
      x={x}
      y={y}
      width={cs}
      height={cs}
      rx={2}
      style={{ fill: heatColor(bin) }}
      data-a=""
      data-day={d}
      {...tip}
      onClick={() => {
        env.actions.setPeriod('custom', { from: d, to: d });
      }}
    />
  );
}

function Calendar({ d, from, to, env }: { d: InsightDaily; from: string; to: string; env: ChartEnv }) {
  const { w, h, big } = env;
  const { start, weeks, cs, st, oy } = heatLayout(from, to, w, h, big);
  const daily = new Map(d.days.map((x) => [x.date, x]));
  const q = quintiles(d.days.map((x) => x.spentMinor));
  const labels = [];
  const cells = [];
  let lastM = '';
  for (let k = 0; k < weeks; k++) {
    const ws = addDays(start, k * 7);
    const shown = ws < from ? from : ws;
    const mo = shown.slice(0, 7);
    if (mo !== lastM) {
      if (k < weeks - 1 || lastM === '')
        labels.push(
          <text key={`m${k}`} className="ax" x={PAD.l + k * st} y={oy + PAD.t - 4}>
            {fmtMonthShort(mo)}
          </text>,
        );
      lastM = mo;
    }
    for (let r = 0; r < 7; r++) {
      const day = addDays(ws, r);
      if (day < from || day >= to) continue;
      const o = daily.get(day);
      const v = o?.spentMinor ?? 0;
      cells.push(
        <Cell
          key={day}
          d={day}
          x={PAD.l + k * st}
          y={oy + PAD.t + r * st}
          cs={cs}
          v={v}
          n={o?.count ?? 0}
          bin={heatBin(v, q)}
          cur={d.currency}
          env={env}
        />,
      );
    }
  }
  return (
    <Svg w={w} h={h} label="Daily spending calendar">
      {[0, 2, 4].map((r) => (
        <text key={r} className="ax" x={PAD.l - 6} y={oy + PAD.t + r * st + cs - 1} textAnchor="end">
          {fmtWeekday(addDays('2026-01-05', r))}
        </text>
      ))}
      {labels}
      {cells}
    </Svg>
  );
}

const legend = (
  <>
    <span style={{ fontSize: '.65rem', color: 'var(--muted)' }}>Less</span>
    {[0, 1, 2, 3, 4, 5].map((k) => (
      <i key={k} className="sw" style={{ background: heatColor(k) }} />
    ))}
    <span style={{ fontSize: '.65rem', color: 'var(--muted)' }}>More</span>
  </>
);

export function dailyParts(d: InsightDaily, env: ChartEnv): ChartParts {
  const { from, to } = d;
  if (!from || !to)
    return { loading: false, error: false, sub: '', legend, body: <Empty>No dated operations</Empty>, table: null };
  const { weeks } = heatLayout(from, to, env.w, env.h, env.big);
  const hr = env.cfg.heatRange;
  return {
    loading: false,
    error: false,
    sub: `${weeks} weeks${hr === 'period' ? '' : ` · ${hr === 'all' ? 'all data' : `last ${hr} mo`}`}`,
    legend,
    table: {
      cols: ['Day', 'Spent', 'Operations'],
      num: [false, true, true],
      rows: [...d.days].reverse().map((x) => [fmtDayLong(x.date), money(x.spentMinor, d.currency), String(x.count)]),
    },
    body: <Calendar d={d} from={from} to={to} env={env} />,
  };
}

export const heatControls: readonly CfgControl[] = [
  {
    key: 'heatRange',
    label: 'Range',
    options: [
      ['period', 'Follow selected period'],
      [3, 'Last 3 months'],
      [6, 'Last 6 months'],
      [12, 'Last 12 months'],
      ['all', 'All data'],
    ],
  },
];

export const dailyChart: ChartDef = {
  key: 'heat',
  title: 'Daily spending',
  cfgControls: heatControls,
  use: (q: ViewQuery, env: ChartEnv) =>
    partsOf(useDaily(q.context, q.ctxKey, env.cfg.heatRange), (d) => dailyParts(d, env)),
};
