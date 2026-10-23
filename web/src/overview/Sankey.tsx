// Money flow (legacy CHARTS.o_sankey): the month's income sources on the left, one cash column in
// the middle and the spending categories on the right, with what was saved (or taken from savings)
// balancing the sides. Clicking a category filters the list by it and opens the table.

import { money } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import { Empty, type ChartDef, type ChartEnv, type ChartParts } from '../charts/types';
import { fmtMonthLong } from '../lib/dates';
import { fmtCompact } from '../lib/format';
import { useTip } from '../ui/Tooltip';
import { categoryCond, colorsOf, negMoney, useOverviewParts, type OverviewData } from './data';

export type SNode = { k: string; v: number; col: string; other?: boolean; filter?: boolean };

/** The legacy top(): the n largest, then the rest as one entry when it is positive. */
export function topN(list: readonly { k: string; v: number }[], n: number, other: string) {
  const out: { k: string; v: number; other?: boolean }[] = list.slice(0, n).map((x) => ({ ...x }));
  const rest = list.slice(n).reduce((s, x) => s + x.v, 0);
  if (rest > 0) out.push({ k: other, v: rest, other: true });
  return out;
}

/** The two sides of the flow (legacy o_sankey): top income sources and categories, plus the balance. */
export function sides(d: OverviewData, big: boolean): { L: SNode[]; R: SNode[]; T: number } {
  const inc = d.current.incomeMinor;
  const exp = d.current.expenseMinor;
  const color = colorsOf(d);
  const src = d.incomeSources.map((s) => ({ k: s.payee, v: s.spentMinor }));
  // categories by this month's spending, largest first (a stable sort keeps the budget order on ties)
  const cats = d.categories
    .filter((c) => c.curMinor > 0)
    .map((c) => ({ k: c.name, v: c.curMinor }))
    .sort((a, b) => b.v - a.v);
  const L: SNode[] = topN(src, big ? 6 : 4, 'Other income').map((n) => ({ ...n, col: 'var(--inc)' }));
  const R: SNode[] = topN(cats, big ? 10 : 7, 'Other spending').map((n) => ({
    ...n,
    col: n.other ? 'var(--c-other)' : color(n.k),
    filter: !n.other,
  }));
  const net = inc - exp;
  if (net < 0) L.push({ k: 'From savings', v: -net, col: 'var(--c-other)' });
  if (net > 0) R.push({ k: 'Saved', v: net, col: 'var(--st-good)' });
  return { L, R, T: Math.max(inc, exp) };
}

const pctOf = (v: number, T: number) => `${((v / T) * 100).toFixed(1)}%`;

type Laid = SNode & { y: number; h: number };

/** A band (link) or a node of a side; categories filter the list on click. */
function Item({
  n,
  shape,
  cur,
  T,
  env,
}: {
  n: Laid;
  shape: { d: string } | { x: number };
  cur: string;
  T: number;
  env: ChartEnv;
}) {
  const tip = useTip(
    <TipContent
      title={n.k}
      rows={[
        [n.col, 'Amount', money(n.v, cur)],
        ['', 'Share', pctOf(n.v, T)],
      ]}
      hint={n.filter ? 'Click to filter the list' : undefined}
    />,
  );
  const click = n.filter
    ? {
        'data-a': '',
        'data-cat': n.k,
        onClick: () => {
          env.actions.toggleFilter('category', categoryCond(n.k));
          env.actions.showTable();
        },
      }
    : {};
  return 'd' in shape ? (
    <path className="lk" d={shape.d} style={{ fill: n.col }} {...tip} {...click} />
  ) : (
    <rect x={shape.x} y={n.y} width={10} height={n.h} rx={2} style={{ fill: n.col }} {...tip} {...click} />
  );
}

function CashNode({ x, y, h, d, cur }: { x: number; y: number; h: number; d: OverviewData; cur: string }) {
  const c = d.current;
  const tip = useTip(
    <TipContent
      title={fmtMonthLong(d.month)}
      rows={[
        ['var(--inc)', 'Income', money(c.incomeMinor, cur)],
        ['var(--exp)', 'Spending', negMoney(c.expenseMinor, cur)],
        ['', 'Net', money(c.incomeMinor - c.expenseMinor, cur)],
      ]}
    />,
  );
  return <rect x={x} y={y} width={10} height={h} rx={2} style={{ fill: 'var(--text-2)' }} {...tip} />;
}

function Flow({ d, cur, env }: { d: OverviewData; cur: string; env: ChartEnv }) {
  const { w, h, big } = env;
  const { L, R, T } = sides(d, big);
  const nw = 10;
  const gap = big ? 10 : 6;
  const padT = 18;
  const padB = 6;
  const H = h - padT - padB;
  const labW = Math.min(big ? 240 : 170, w * 0.27);
  const xL = labW;
  const xR = w - labW - nw;
  const xM = (xL + xR) / 2 - nw / 2;
  const k = Math.min((H - gap * (L.length - 1)) / T, (H - gap * (R.length - 1)) / T);
  const lay = (arr: SNode[]): Laid[] => {
    const colH = arr.reduce((s, n) => s + n.v * k, 0) + gap * (arr.length - 1);
    let y = padT + (H - colH) / 2;
    return arr.map((n) => {
      const o = { ...n, y, h: Math.max(1, n.v * k) };
      y += n.v * k + gap;
      return o;
    });
  };
  const l = lay(L);
  const r = lay(R);
  const mH = T * k;
  const mY = padT + (H - mH) / 2;
  const band = (x0: number, a0: number, a1: number, x1: number, b0: number, b1: number) => {
    const xm = (x0 + x1) / 2;
    return `M${x0} ${a0}C${xm} ${a0} ${xm} ${b0} ${x1} ${b0}L${x1} ${b1}C${xm} ${b1} ${xm} ${a1} ${x0} ${a1}Z`;
  };
  const maxC = Math.max(6, Math.floor((labW - 50) / 7.6));
  const label = (n: Laid, x: number, anchor: 'start' | 'end') =>
    n.h >= 7 || big ? (
      <text
        key={`t${n.k}`}
        x={x}
        y={n.y + n.h / 2 + 4}
        textAnchor={anchor}
        style={{ font: '500 11px var(--sans)', fill: 'var(--text)' }}
      >
        {n.k.length > maxC ? `${n.k.slice(0, maxC - 1)}…` : n.k}{' '}
        <tspan style={{ fill: 'var(--muted)', fontWeight: 400 }}>
          {fmtCompact({ amountMinor: n.v, currency: cur })}
        </tspan>
      </text>
    ) : null;
  // each band starts where the previous one ended on the cash column
  const bands = (arr: Laid[], link: (n: Laid, off: number) => string) => {
    let off = 0;
    return arr.map((n) => {
      const p = link(n, off);
      off += n.v * k;
      return p;
    });
  };
  const lb = bands(l, (n, off) => band(xL + nw, n.y, n.y + n.v * k, xM, mY + off, mY + off + n.v * k));
  const rb = bands(r, (n, off) => band(xM + nw, mY + off, mY + off + n.v * k, xR, n.y, n.y + n.v * k));
  return (
    <Svg w={w} h={h} label="Money flow from income to spending categories">
      {l.map((n, i) => (
        <Item key={`l${n.k}`} n={n} shape={{ d: lb[i] ?? '' }} cur={cur} T={T} env={env} />
      ))}
      {r.map((n, i) => (
        <Item key={`r${n.k}`} n={n} shape={{ d: rb[i] ?? '' }} cur={cur} T={T} env={env} />
      ))}
      {l.map((n) => (
        <Item key={`ln${n.k}`} n={n} shape={{ x: xL }} cur={cur} T={T} env={env} />
      ))}
      {r.map((n) => (
        <Item key={`rn${n.k}`} n={n} shape={{ x: xR }} cur={cur} T={T} env={env} />
      ))}
      <CashNode x={xM} y={mY} h={mH} d={d} cur={cur} />
      {l.map((n) => label(n, xL - 8, 'end'))}
      {r.map((n) => label(n, xR + nw + 8, 'start'))}
      <text x={xL + nw / 2} y={11} textAnchor="middle" className="ax">
        INCOME
      </text>
      <text x={xM + nw / 2} y={11} textAnchor="middle" className="ax">
        CASH
      </text>
      <text x={xR + nw / 2} y={11} textAnchor="middle" className="ax">
        WHERE IT WENT
      </text>
    </Svg>
  );
}

export function sankeyParts(d: OverviewData, cur: string, env: ChartEnv): ChartParts {
  const c = d.current;
  if (!c.incomeMinor && !c.expenseMinor)
    return { loading: false, error: false, sub: '', body: <Empty>No money movement this month</Empty>, table: null };
  const { L, R, T } = sides(d, env.big);
  return {
    loading: false,
    error: false,
    sub: fmtMonthLong(d.month),
    table: {
      cols: ['Side', 'Item', 'Amount', 'Share'],
      num: [false, false, true, true],
      rows: [
        ...L.map((n) => ['In', n.k, money(n.v, cur), pctOf(n.v, T)]),
        ...R.map((n) => ['Out', n.k, money(n.v, cur), pctOf(n.v, T)]),
      ],
    },
    body: <Flow d={d} cur={cur} env={env} />,
  };
}

export const sankeyChart: ChartDef = {
  key: 'o_sankey',
  title: 'Money flow',
  use: (q, env) => useOverviewParts(q, env, sankeyParts),
};
