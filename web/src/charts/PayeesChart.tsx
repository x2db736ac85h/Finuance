// Top payees (legacy CHARTS.payee): horizontal bars in one colour, over the rows shown or all time.
// Clicking a payee filters the list to it (a contains condition); clicking it again clears it.

import { payeeTop, usePayees, type InsightPayees } from '../api/hooks/insights';
import type { ViewQuery } from '../table/context';
import { useTip } from '../ui/Tooltip';
import { PRIMARY } from './lib/colors';
import { divRound, money, whole } from './lib/money';
import { TipContent } from './lib/Tip';
import { Empty, partsOf, type CfgControl, type ChartDef, type ChartEnv, type ChartParts } from './types';

type Payee = InsightPayees['payees'][number];

function Bar({ p, max, cur, on, env }: { p: Payee; max: number; cur: string; on: boolean; env: ChartEnv }) {
  const tip = useTip(
    <TipContent
      title={p.payee}
      rows={[
        [PRIMARY, 'Spent', money(p.spentMinor, cur)],
        ['', 'Operations', String(p.count)],
        ['', 'Average', money(divRound(p.spentMinor, p.count), cur)],
      ]}
      hint="Click to filter the list"
    />,
  );
  return (
    <div
      className={`hb${on ? ' on' : ''}`}
      data-a=""
      {...tip}
      onClick={() => {
        env.actions.toggleFilter('payee', p.payee);
      }}
    >
      <span className="hb-l">
        <span>{p.payee}</span>
      </span>
      <span className="hb-t">
        <span className="hb-f" style={{ width: `${((p.spentMinor / max) * 100).toFixed(2)}%`, background: PRIMARY }} />
      </span>
      <span className="hb-v">{whole(p.spentMinor, cur)}</span>
    </div>
  );
}

export function payeesParts(d: InsightPayees, env: ChartEnv): ChartParts {
  const list = d.payees;
  const first = list[0];
  if (!first)
    return { loading: false, error: false, sub: '', body: <Empty>No spending in this selection</Empty>, table: null };
  const cur = d.currency;
  const active = env.filters['payee']?.cond ?? null;
  return {
    loading: false,
    error: false,
    sub: `top ${list.length} of ${d.total}${env.cfg.payeeScope === 'all' ? ' · all time' : ''}`,
    table: {
      cols: ['Payee', 'Spent', 'Operations', 'Average'],
      num: [false, true, true, true],
      rows: list.map((p) => [
        p.payee,
        money(p.spentMinor, cur),
        String(p.count),
        money(divRound(p.spentMinor, p.count), cur),
      ]),
    },
    body: (
      <div className="hbars" tabIndex={0} role="region" aria-label="Top payees, list">
        {list.map((p) => (
          <Bar key={p.payee} p={p} max={first.spentMinor} cur={cur} on={active === p.payee} env={env} />
        ))}
      </div>
    ),
  };
}

export const payeeControls: readonly CfgControl[] = [
  {
    key: 'payeeScope',
    label: 'Data',
    options: [
      ['list', 'Selected period'],
      ['all', 'All time'],
    ],
  },
  {
    key: 'payeeTop',
    label: 'Show',
    options: [
      [10, 'Top 10'],
      [25, 'Top 25'],
      [50, 'Top 50'],
      [100, 'Top 100'],
      [0, 'All payees'],
    ],
  },
];

export const payeesChart: ChartDef = {
  key: 'payee',
  title: 'Top payees',
  cfgControls: payeeControls,
  use: (q: ViewQuery, env: ChartEnv) =>
    partsOf(usePayees(q.context, q.ctxKey, payeeTop(env.cfg.payeeTop), env.cfg.payeeScope, env.ready), (d) =>
      payeesParts(d, env),
    ),
};
