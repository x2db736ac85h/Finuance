// The KPI row (legacy renderOverview's kpi()): income, spending, net saved with the savings rate,
// and the average per day, or the projected spending in a month in progress. Each has the change
// against the previous month (coloured by whether up is good) and a sparkline over the range.

import type { ReactNode } from 'react';

import { EXPENSE, INCOME, PREVIOUS, PRIMARY } from '../charts/lib/colors';
import { spark } from '../charts/lib/geom';
import { divRound, plain, whole } from '../charts/lib/money';
import { fmtMonthShort } from '../lib/dates';
import { uiScale } from '../styles/scale';
import { rateOf, type OverviewData } from './data';

/** The change line (legacy deltaHtml): no data, "same as" under half a percent, else ▲/▼ n%. */
export function Delta({
  cur,
  prev,
  goodWhenUp,
  label,
}: {
  cur: number;
  prev: number;
  goodWhenUp: boolean;
  label: string;
}) {
  if (!prev) return <span className="muted">no data for {label}</span>;
  const d = ((cur - prev) / Math.abs(prev)) * 100;
  if (Math.abs(d) < 0.5) return <>same as {label}</>;
  const good = d > 0 === goodWhenUp;
  return (
    <>
      <span className={good ? 'gd' : 'bd'}>
        {d > 0 ? '▲' : '▼'} {Math.abs(d).toFixed(0)}%
      </span>{' '}
      vs {label}
    </>
  );
}

function Spark({ vals, color }: { vals: number[]; color: string }) {
  const s = spark(vals);
  if (!s) return null;
  return (
    // logical units scaled to the interface size, as every chart (legacy svgTag)
    <svg className="spark" width={110 * uiScale()} height={30 * uiScale()} viewBox="0 0 110 30" aria-hidden="true">
      <path
        d={s.d}
        style={{ fill: 'none', stroke: color, strokeWidth: 1.6, strokeLinejoin: 'round', strokeLinecap: 'round' }}
      />
      <circle cx={s.last[0]} cy={s.last[1]} r={2.6} style={{ fill: color, stroke: 'var(--panel)', strokeWidth: 1.5 }} />
    </svg>
  );
}

function Kpi({ label, n, d, sp, neg }: { label: string; n: string; d: ReactNode; sp: ReactNode; neg?: boolean }) {
  return (
    <div className="ovk" role="group" aria-label={label}>
      <div className="ovk-t">
        <div className="l">{label}</div>
        <div className={neg ? 'n neg' : 'n'}>{n}</div>
        <div className="d">{d}</div>
      </div>
      {sp}
    </div>
  );
}

export function Kpis({ d, cur }: { d: OverviewData; cur: string }) {
  const c = d.current;
  const p = d.previous;
  const mon = fmtMonthShort(d.prevMonth);
  const cmp = d.partial ? d.prevToDateMinor : p.expenseMinor;
  const cl = cur ? ` · ${cur}` : '';
  const r = rateOf(c);
  const net = c.incomeMinor - c.expenseMinor;
  const s = (f: (m: OverviewData['months'][number]) => number) => d.months.map(f);
  return (
    <>
      <Kpi
        label={`Income${cl}`}
        n={plain(c.incomeMinor, cur)}
        d={<Delta cur={c.incomeMinor} prev={p.incomeMinor} goodWhenUp label={mon} />}
        sp={<Spark vals={s((m) => m.incomeMinor)} color={INCOME} />}
      />
      <Kpi
        label={`Spending${cl}`}
        n={plain(c.expenseMinor, cur)}
        d={<Delta cur={c.expenseMinor} prev={cmp} goodWhenUp={false} label={mon + (d.partial ? ' (same days)' : '')} />}
        sp={<Spark vals={s((m) => m.expenseMinor)} color={EXPENSE} />}
      />
      <Kpi
        label={`Net saved${cl}`}
        n={(net > 0 ? '+' : '') + plain(net, cur)}
        neg={net < 0}
        d={
          r === null ? (
            'no income this month'
          ) : (
            <>
              savings rate <b>{r.toFixed(0)}%</b>
            </>
          )
        }
        sp={<Spark vals={s((m) => m.incomeMinor - m.expenseMinor)} color={PRIMARY} />}
      />
      {d.partial && d.projectedMinor !== undefined ? (
        <Kpi
          label={`Projected spend${cl}`}
          n={plain(d.projectedMinor, cur)}
          d={`${d.elapsedDays} of ${d.daysInMonth} days · avg ${whole(divRound(c.expenseMinor, d.elapsedDays), cur)}/day`}
          sp={<Spark vals={s((m) => m.expenseMinor)} color={PREVIOUS} />}
        />
      ) : (
        <Kpi
          label={`Avg / day${cl}`}
          n={plain(divRound(c.expenseMinor, d.daysInMonth), cur)}
          d={`over ${d.daysInMonth} days`}
          sp={<Spark vals={s((m) => m.expenseMinor)} color={PREVIOUS} />}
        />
      )}
    </>
  );
}
