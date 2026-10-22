// Spending by category (legacy CHARTS.cat): a donut of the categories that own a colour (the 7
// largest all-time spenders) plus "Other", or bars of every category. Clicking a category filters
// the list to it (=Name); clicking it again clears the filter.

import { useCategorySpend, type InsightCategories } from '../api/hooks/insights';
import { fmtCompact, fmtShare } from '../lib/format';
import type { ViewQuery } from '../table/context';
import { slotColor } from './lib/colors';
import { arc } from './lib/geom';
import { money, whole } from './lib/money';
import { Svg } from './lib/Svg';
import { TipContent } from './lib/Tip';
import { useTip } from '../ui/Tooltip';
import { Empty, partsOf, type ChartDef, type ChartEnv, type ChartParts } from './types';

/** The legacy label of uncategorised spending. */
export const UNCATEGORIZED = '(uncategorized)';

type Entry = { name: string; v: number; share: number; color: string; cond: string | null; other?: boolean };

const HINT = 'Click to filter the list';

/** The category filter condition shown as a highlighted entry (legacy: an "=Name" condition). */
export function activeCategory(env: Pick<ChartEnv, 'filters'>): string | null {
  const c = env.filters['category']?.cond;
  if (!c?.startsWith('=')) return null;
  return c === '=' ? UNCATEGORIZED : c.slice(1);
}

function entriesOf(d: InsightCategories): Entry[] {
  return d.categories.map((c) => ({
    name: c.category?.name ?? UNCATEGORIZED,
    v: c.spentMinor,
    share: c.share,
    color: slotColor(c.colorSlot),
    // "=" alone matches an empty category (the filter language), so the uncategorised entry filters too
    cond: c.category ? `=${c.category.name}` : '=',
  }));
}

function Bar({ e, max, cur, on, env }: { e: Entry; max: number; cur: string; on: boolean; env: ChartEnv }) {
  const tip = useTip(
    <TipContent
      title={e.name}
      rows={[
        [e.color, 'Spent', money(e.v, cur)],
        ['', 'Share', fmtShare(e.share, 1)],
      ]}
      hint={HINT}
    />,
  );
  return (
    <div
      className={`hb${on ? ' on' : ''}`}
      data-a=""
      {...tip}
      onClick={() => {
        if (e.cond) env.actions.toggleFilter('category', e.cond);
      }}
    >
      <span className="hb-l">
        <i className="sw" style={{ background: e.color }} />
        <span>{e.name}</span>
      </span>
      <span className="hb-t">
        <span className="hb-f" style={{ width: `${((e.v / max) * 100).toFixed(2)}%`, background: e.color }} />
      </span>
      <span className="hb-v">{whole(e.v, cur)}</span>
    </div>
  );
}

function Segment({
  s,
  d,
  active,
  cur,
  env,
}: {
  s: Entry;
  d: string | null;
  active: string | null;
  cur: string;
  env: ChartEnv;
}) {
  const tip = useTip(
    <TipContent
      title={s.name}
      rows={[
        [s.color, 'Spent', money(s.v, cur)],
        ['', 'Share', fmtShare(s.share, 1)],
      ]}
      hint={s.other ? undefined : HINT}
    />,
  );
  const click = s.cond
    ? () => {
        if (s.cond) env.actions.toggleFilter('category', s.cond);
      }
    : undefined;
  return d === null ? null : (
    <path
      d={d}
      style={{ fill: s.color, stroke: 'var(--panel)', strokeWidth: 2 }}
      className={active && active !== s.name ? 'dim' : undefined}
      data-a={click ? '' : undefined}
      {...tip}
      onClick={click}
    />
  );
}

function Ring({
  s,
  cx,
  cy,
  R,
  r,
  cur,
  env,
}: {
  s: Entry;
  cx: number;
  cy: number;
  R: number;
  r: number;
  cur: string;
  env: ChartEnv;
}) {
  const tip = useTip(
    <TipContent
      title={s.name}
      rows={[
        [s.color, 'Spent', money(s.v, cur)],
        ['', 'Share', fmtShare(s.share, 1)],
      ]}
      hint={s.other ? undefined : HINT}
    />,
  );
  return (
    <circle
      cx={cx}
      cy={cy}
      r={(R + r) / 2}
      style={{ fill: 'none', stroke: s.color, strokeWidth: R - r }}
      data-a={s.cond ? '' : undefined}
      {...tip}
      onClick={() => {
        if (s.cond) env.actions.toggleFilter('category', s.cond);
      }}
    />
  );
}

function LegendRow({ s, on, cur, env }: { s: Entry; on: boolean; cur: string; env: ChartEnv }) {
  const tip = useTip(
    <TipContent
      title={s.name}
      rows={[
        [s.color, 'Spent', money(s.v, cur)],
        ['', 'Share', fmtShare(s.share, 1)],
      ]}
      hint={s.other ? undefined : HINT}
    />,
  );
  return (
    <div
      className={`lg${on ? ' on' : ''}`}
      data-a={s.cond ? '' : undefined}
      {...tip}
      onClick={() => {
        if (s.cond) env.actions.toggleFilter('category', s.cond);
      }}
    >
      <i className="sw" style={{ background: s.color }} />
      <span className="nm">{s.name}</span>
      <span className="vl">{whole(s.v, cur)}</span>
      <span className="pc">{fmtShare(s.share)}</span>
    </div>
  );
}

function Donut({ d, env }: { d: InsightCategories; env: ChartEnv }) {
  const { w, h, big } = env;
  const cur = d.currency;
  const all = d.totalMinor;
  const active = activeCategory(env);
  const segs: Entry[] = [];
  let other = 0;
  let otherN = 0;
  for (const e of entriesOf(d)) {
    if (e.color !== slotColor(null)) segs.push(e);
    else {
      other += e.v;
      otherN++;
    }
  }
  if (other)
    segs.push({
      name: `Other (${otherN})`,
      v: other,
      share: other / all,
      color: slotColor(null),
      cond: null,
      other: true,
    });
  const size = Math.max(60, Math.min(h - 14, w * 0.36, big ? 520 : 170));
  const R = size / 2;
  const r = R * 0.64;
  const cx = R + 1;
  const cy = h / 2;
  let a0 = -Math.PI / 2;
  const paths = segs.map((s) => {
    const a1 = a0 + (s.v / all) * Math.PI * 2;
    const p = arc(cx, cy, R, r, a0, a1);
    a0 = a1;
    return p;
  });
  const first = segs[0];
  return (
    <div className="donut">
      <Svg w={size + 2} h={h} label="Spending by category">
        {segs.length === 1 && first ? (
          <Ring s={first} cx={cx} cy={cy} R={R} r={r} cur={cur} env={env} />
        ) : (
          segs.map((s, i) => <Segment key={s.name} s={s} d={paths[i] ?? null} active={active} cur={cur} env={env} />)
        )}
        <text
          x={cx}
          y={cy - 1}
          textAnchor="middle"
          style={{ font: `600 ${Math.max(11, Math.min(big ? 34 : 16, R / 3.6))}px var(--sans)`, fill: 'var(--text)' }}
        >
          {fmtCompact({ amountMinor: all, currency: cur })}
        </text>
        <text x={cx} y={cy + 13} textAnchor="middle" className="ax">
          spent
        </text>
      </Svg>
      <div className="leg">
        {segs.map((s) => (
          <LegendRow key={s.name} s={s} on={active === s.name} cur={cur} env={env} />
        ))}
      </div>
    </div>
  );
}

function CatMode({ env }: { env: ChartEnv }) {
  return (
    <span className="seg">
      {(['donut', 'bars'] as const).map((m) => (
        <button
          key={m}
          type="button"
          className={env.catMode === m ? 'on' : ''}
          aria-pressed={env.catMode === m}
          onClick={() => {
            env.actions.setCatMode(m);
          }}
        >
          {m === 'donut' ? 'Donut' : 'Bars'}
        </button>
      ))}
    </span>
  );
}

export function categoriesParts(d: InsightCategories, env: ChartEnv): ChartParts {
  const legend = <CatMode env={env} />;
  if (!d.totalMinor)
    return {
      loading: false,
      error: false,
      sub: '',
      legend,
      body: <Empty>No spending in this selection</Empty>,
      table: null,
    };
  const ents = entriesOf(d);
  const n = ents.length;
  const active = activeCategory(env);
  const max = ents[0]?.v ?? 1;
  return {
    loading: false,
    error: false,
    sub: `${n} categor${n === 1 ? 'y' : 'ies'}`,
    legend,
    table: {
      cols: ['Category', 'Spent', 'Share'],
      num: [false, true, true],
      rows: ents.map((e) => [e.name, money(e.v, d.currency), fmtShare(e.share, 1)]),
    },
    body:
      env.catMode === 'bars' ? (
        <div className="hbars">
          {ents.map((e) => (
            <Bar key={e.name} e={e} max={max} cur={d.currency} on={active === e.name} env={env} />
          ))}
        </div>
      ) : (
        <Donut d={d} env={env} />
      ),
  };
}

export const categoriesChart: ChartDef = {
  key: 'cat',
  title: 'Spending by category',
  use: (q: ViewQuery, env: ChartEnv) => partsOf(useCategorySpend(q.context, q.ctxKey), (d) => categoriesParts(d, env)),
};
