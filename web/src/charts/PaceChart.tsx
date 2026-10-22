// Month pace (legacy CHARTS.pace): cumulative spending by day of month for the target month (the
// period's month, else the latest) against a comparison (previous month, same month last year, the
// average of the previous 3 or 6 months, or nothing). A crosshair follows the pointer.

import { useState, type PointerEvent } from 'react';

import { usePace, type InsightPace } from '../api/hooks/insights';
import { fmtMonthShort, fmtMonthYear } from '../lib/dates';
import { fmtCompact } from '../lib/format';
import { uiScale } from '../styles/scale';
import type { ViewQuery } from '../table/context';
import { hideTip, showTip } from '../ui/Tooltip';
import { PREVIOUS, PRIMARY } from './lib/colors';
import { money } from './lib/money';
import { Svg } from './lib/Svg';
import { TipContent, type TipRow } from './lib/Tip';
import { Empty, partsOf, type CfgControl, type ChartDef, type ChartEnv, type ChartParts } from './types';

const PAD = { l: 38, r: 48, t: 10, b: 20 };

type Series = { label: string; values: number[]; days: number };

/** The comparison series' label (legacy plabel). */
export function compareLabel(p: InsightPace): string {
  const c = p.compare;
  if (!c) return '';
  if (c.mode === 'prev' && c.month) return fmtMonthShort(c.month);
  if (c.mode === 'yoy' && c.month) return fmtMonthYear(c.month);
  return `${c.months ?? 0}-mo avg`;
}

function Lines({ p, env }: { p: InsightPace & { target: string }; env: ChartEnv }) {
  const { w, h } = env;
  const cur = p.current;
  const last = p.lastDay ?? 1;
  const nd = p.daysInMonth ?? 31;
  const pre: Series | null = p.compare
    ? { label: compareLabel(p), values: p.compare.values, days: p.compare.days }
    : null;
  const pw = w - PAD.l - PAD.r;
  const ph = h - PAD.t - PAD.b;
  const at = (a: readonly number[], d: number) => a[d - 1] ?? 0;
  const vmax = Math.max(at(cur, last), pre ? at(pre.values, pre.days) : 0, 1);
  const X = (d: number) => PAD.l + ((d - 1) / 30) * pw;
  const Y = (v: number) => PAD.t + ph - (v / vmax) * ph;
  const line = (a: readonly number[], n: number) => {
    let s = '';
    for (let d = 1; d <= n; d++) s += `${d === 1 ? 'M' : 'L'}${X(d).toFixed(1)} ${Y(at(a, d)).toFixed(1)}`;
    return s;
  };
  const c = p.currency;
  const ex = X(last);
  const ey = Y(at(cur, last));
  const [hover, setHover] = useState<number | null>(null);

  const onMove = (e: PointerEvent<SVGRectElement>) => {
    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const r = svg.getBoundingClientRect();
    const d = Math.max(1, Math.min(31, Math.round((((e.clientX - r.left) / uiScale() - PAD.l) / pw) * 30) + 1));
    setHover(d);
    const hasC = d <= last;
    const hasP = !!pre && d <= pre.days;
    const rows: TipRow[] = [];
    if (hasC) rows.push([PRIMARY, fmtMonthYear(p.target), money(-at(cur, d), c)]);
    if (hasP) rows.push([PREVIOUS, pre.label, money(-at(pre.values, d), c)]);
    const pv = pre ? at(pre.values, d) : 0;
    if (hasC && hasP && pv) rows.push(['', 'Difference', `${(((at(cur, d) - pv) / pv) * 100).toFixed(0)}%`]);
    showTip(<TipContent title={`Day ${d}`} rows={rows} />, e.clientX, e.clientY);
  };

  return (
    <Svg w={w} h={h} label="Cumulative spending this month vs comparison">
      {[0, 0.5, 1].map((f) => {
        const y = PAD.t + ph - f * ph;
        return (
          <g key={f}>
            <line x1={PAD.l} x2={PAD.l + pw} y1={y} y2={y} style={{ stroke: f ? 'var(--gridl)' : 'var(--axis)' }} />
            <text className="ax" x={PAD.l - 6} y={y + 3} textAnchor="end">
              {fmtCompact({ amountMinor: Math.round(vmax * f), currency: c })}
            </text>
          </g>
        );
      })}
      {[1, 8, 15, 22, 29].map((d) => (
        <text key={d} className="ax" x={X(d)} y={h - 5} textAnchor="middle">
          {d}
        </text>
      ))}
      {pre && (
        <path
          d={line(pre.values, pre.days)}
          style={{ fill: 'none', stroke: PREVIOUS, strokeWidth: 2, strokeLinejoin: 'round', strokeLinecap: 'round' }}
        />
      )}
      <path
        d={line(cur, last)}
        style={{ fill: 'none', stroke: PRIMARY, strokeWidth: 2, strokeLinejoin: 'round', strokeLinecap: 'round' }}
      />
      <circle cx={ex} cy={ey} r={4} style={{ fill: PRIMARY, stroke: 'var(--panel)', strokeWidth: 2 }} />
      <text x={ex + 8} y={ey + 4} style={{ font: '600 11px var(--sans)', fill: 'var(--text)' }}>
        {fmtCompact({ amountMinor: at(cur, last), currency: c })}
      </text>
      {hover !== null && (
        <g className="xh">
          <line x1={X(hover)} x2={X(hover)} y1={PAD.t} y2={PAD.t + ph} style={{ stroke: 'var(--axis)' }} />
          {hover <= last && (
            <circle
              cx={X(hover)}
              cy={Y(at(cur, Math.min(hover, nd)))}
              r={3.5}
              style={{ fill: PRIMARY, stroke: 'var(--panel)', strokeWidth: 2 }}
            />
          )}
          {pre && hover <= pre.days && (
            <circle
              cx={X(hover)}
              cy={Y(at(pre.values, hover))}
              r={3.5}
              style={{ fill: PREVIOUS, stroke: 'var(--panel)', strokeWidth: 2 }}
            />
          )}
        </g>
      )}
      <rect
        className="ov"
        x={PAD.l}
        y={PAD.t}
        width={pw}
        height={ph}
        fill="transparent"
        onPointerMove={onMove}
        onPointerLeave={() => {
          setHover(null);
          hideTip();
        }}
      />
    </Svg>
  );
}

export function paceParts(p: InsightPace, env: ChartEnv): ChartParts {
  const target = p.target;
  if (!target) return { loading: false, error: false, sub: '', body: <Empty>No dated amounts</Empty>, table: null };
  const label = compareLabel(p);
  const c = p.currency;
  const nd = p.daysInMonth ?? 31;
  const last = p.lastDay ?? nd;
  const pd = p.compare?.days ?? 0;
  const rows: string[][] = [];
  for (let d = 1; d <= Math.max(nd, pd); d++) {
    const row = [String(d), d <= last ? money(-(p.current[d - 1] ?? 0), c) : ''];
    if (p.compare) row.push(d <= pd ? money(-(p.compare.values[d - 1] ?? 0), c) : '');
    rows.push(row);
  }
  return {
    loading: false,
    error: false,
    sub: 'cumulative spend',
    legend: (
      <>
        <span>
          <i className="sw ln" style={{ background: PRIMARY }} /> {fmtMonthShort(target)}
        </span>
        {p.compare && (
          <span>
            <i className="sw ln" style={{ background: PREVIOUS }} /> {label}
          </span>
        )}
      </>
    ),
    table: {
      cols: ['Day', fmtMonthYear(target), ...(p.compare ? [label] : [])],
      num: [true, true, true],
      rows,
    },
    body: <Lines p={{ ...p, target }} env={env} />,
  };
}

export const paceControls: readonly CfgControl[] = [
  {
    key: 'paceCmp',
    label: 'Compare with',
    options: [
      ['prev', 'Previous month'],
      ['yoy', 'Same month last year'],
      ['avg3', 'Average of previous 3 months'],
      ['avg6', 'Average of previous 6 months'],
      ['none', 'Nothing'],
    ],
  },
];

export const paceChart: ChartDef = {
  key: 'pace',
  title: 'Month pace',
  cfgControls: paceControls,
  use: (q: ViewQuery, env: ChartEnv) =>
    partsOf(usePace(q.context, q.ctxKey, env.cfg.paceCmp, env.ready), (p) => paceParts(p, env)),
};
