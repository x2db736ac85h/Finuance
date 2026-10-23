// Account balance (legacy CHARTS.o_balance): the opening balance plus every transaction of the
// view's accounts (list filters ignored), at the end of each day of the range. The opening balance
// is edited here in a single-account view (PATCH /accounts/{id}); "All accounts" shows their sum,
// read-only (PLAN §7.9).

import type { Schemas } from '../api/client';
import { useAccounts } from '../api/hooks/accounts';
import { useSetOpening } from '../api/hooks/overview';
import { PRIMARY } from '../charts/lib/colors';
import { money, whole } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import { Empty, type ChartDef, type ChartEnv, type ChartParts } from '../charts/types';
import { addMonths, fmtDayLong, fmtDayMedium, fmtMonthShort, utc } from '../lib/dates';
import { fmtCompact, parseMoneyInput } from '../lib/format';
import { toast } from '../ui/toastStore';
import { useTip } from '../ui/Tooltip';
import { inputText } from './Budgets';
import { monthsControl, useOverviewParts, type OverviewData } from './data';

type Point = Schemas['BalancePoint'];

/** The opening balance: an input for one account, the read-only sum for all of them. */
export function Opening({ viewId, minor, cur }: { viewId: string; minor: number; cur: string }) {
  const account = useAccounts().data?.find((a) => a.id === viewId);
  const set = useSetOpening();
  if (!account)
    return (
      <span className="ob" title="The accounts' opening balances added up; set them in each account's overview">
        Opening {whole(minor, cur)}
      </span>
    );
  const text = inputText(minor, cur);
  return (
    <label className="ob" title="Balance before the first imported operation">
      Opening{' '}
      <input
        key={text}
        className="bud-in"
        defaultValue={text}
        inputMode="decimal"
        aria-label="Opening balance"
        onBlur={(e) => {
          const el = e.currentTarget;
          if (el.value === text) return;
          const v = el.value.trim() === '' ? 0 : parseMoneyInput(el.value, cur);
          if (v === null || v === minor) {
            el.value = text;
            return;
          }
          set.mutate(
            { account, minor: v },
            {
              onError: () => {
                toast('Could not save the opening balance — try again', { kind: 'error' });
              },
            },
          );
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur();
        }}
      />
    </label>
  );
}

const time = (iso: string) => utc(iso).getTime();

function Hit({ p, x, y, w, h, cur }: { p: Point; x: number; y: number; w: number; h: number; cur: string }) {
  const tip = useTip(
    <TipContent title={fmtDayLong(p.date)} rows={[[PRIMARY, 'Balance (end of day)', money(p.balanceMinor, cur)]]} />,
  );
  return <rect x={x} y={y} width={Math.max(1, w)} height={h} fill="transparent" {...tip} />;
}

function Line({ pts, cur, env }: { pts: Point[]; cur: string; env: ChartEnv }) {
  const { w, h } = env;
  const padL = 46;
  const padR = 52;
  const padT = 10;
  const padB = 20;
  const pw = w - padL - padR;
  const ph = h - padT - padB;
  const vs = pts.map((p) => p.balanceMinor);
  const lo = Math.min(0, ...vs);
  const hi = Math.max(0, ...vs);
  const rg = hi - lo || 1;
  const t0 = time(pts[0]?.date ?? '');
  const t1 = time(pts[pts.length - 1]?.date ?? '');
  const X = (t: number) => padL + ((t - t0) / (t1 - t0 || 1)) * pw;
  const Y = (v: number) => padT + ((hi - v) / rg) * ph;
  const d = pts.map((p, j) => `${j ? 'L' : 'M'}${X(time(p.date)).toFixed(1)} ${Y(p.balanceMinor).toFixed(1)}`).join('');
  const last = pts[pts.length - 1];
  const months: string[] = [];
  for (let m = (pts[0]?.date ?? '').slice(0, 7); time(m) <= t1; m = addMonths(m, 1)) months.push(m);
  const every = Math.ceil(months.length / Math.max(2, pw / 46));
  const step = Math.max(1, Math.ceil(pts.length / Math.max(2, pw / 6)));
  const hits: { p: Point; x0: number; x1: number }[] = [];
  for (let j = 0; j < pts.length; j += step) {
    const p = pts[j];
    if (!p) continue;
    const prev = pts[j - step];
    const next = pts[j + step];
    const x = X(time(p.date));
    hits.push({
      p,
      x0: j && prev ? (X(time(prev.date)) + x) / 2 : padL,
      x1: next ? (x + X(time(next.date))) / 2 : padL + pw,
    });
  }
  const c = (v: number) => fmtCompact({ amountMinor: v, currency: cur });
  return (
    <Svg w={w} h={h} label="Account balance over time">
      {[hi, (hi + lo) / 2, lo].map((v, i) => (
        // the three grid lines are positional
        <g key={i}>
          <line x1={padL} x2={padL + pw} y1={Y(v)} y2={Y(v)} style={{ stroke: 'var(--gridl)' }} />
          <text className="ax" x={padL - 6} y={Y(v) + 3} textAnchor="end">
            {c(v)}
          </text>
        </g>
      ))}
      {lo < 0 && <line x1={padL} x2={padL + pw} y1={Y(0)} y2={Y(0)} style={{ stroke: 'var(--axis)' }} />}
      <path d={`${d}L${X(t1)} ${Y(0)}L${X(t0)} ${Y(0)}Z`} style={{ fill: PRIMARY, opacity: 0.1 }} />
      <path d={d} style={{ fill: 'none', stroke: PRIMARY, strokeWidth: 2, strokeLinejoin: 'round' }} />
      {last && (
        <>
          <circle
            cx={X(t1)}
            cy={Y(last.balanceMinor)}
            r={4}
            style={{ fill: PRIMARY, stroke: 'var(--panel)', strokeWidth: 2 }}
          />
          <text
            x={X(t1) + 8}
            y={Y(last.balanceMinor) + 4}
            style={{ font: '600 11px var(--sans)', fill: 'var(--text)' }}
          >
            {c(last.balanceMinor)}
          </text>
        </>
      )}
      {months.map((m, j) =>
        j % every === 0 && time(m) >= t0 ? (
          <text key={m} className="ax" x={X(time(m))} y={h - 5} textAnchor="middle">
            {fmtMonthShort(m)}
          </text>
        ) : null,
      )}
      {hits.map(({ p, x0, x1 }) => (
        <Hit key={p.date} p={p} x={x0} y={padT} w={x1 - x0} h={ph} cur={cur} />
      ))}
    </Svg>
  );
}

export function balanceParts(d: OverviewData, cur: string, env: ChartEnv): ChartParts {
  const legend = <Opening viewId={env.viewId} minor={d.balance.openingMinor} cur={cur} />;
  const pts = d.balance.points;
  if (pts.length < 2)
    return {
      loading: false,
      error: false,
      sub: '',
      legend,
      body: <Empty>Not enough data in this range</Empty>,
      table: null,
    };
  return {
    loading: false,
    error: false,
    sub: `now ${money(d.balance.nowMinor, cur)}`,
    legend,
    table: {
      cols: ['Day', 'Balance (end of day)'],
      num: [false, true],
      rows: [...pts].reverse().map((p) => [fmtDayMedium(p.date), money(p.balanceMinor, cur)]),
    },
    body: <Line pts={pts} cur={cur} env={env} />,
  };
}

export const balanceChart: ChartDef = {
  key: 'o_balance',
  title: 'Account balance',
  cfgControls: [monthsControl],
  use: (q, env) => useOverviewParts(q, env, balanceParts),
};
