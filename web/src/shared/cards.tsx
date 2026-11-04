// The shared group page's cards (Phase 19): summary, categories, spending per day, top payees,
// the largest expenses and paid vs share. Every card has a table twin (the same numbers as a
// table, for screen readers and for reading exact values), switched with the card's Table button.

import { useState, type ReactNode } from 'react';

import type { SharedGroupDetail, SharedGroupReport } from '../api/hooks/shared';
import { money, whole } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import { fmtDay, fmtDayMedium } from '../lib/dates';
import { fmtInt, fmtShare, plural } from '../lib/format';
import { IconButton } from '../ui/IconButton';
import { useTip } from '../ui/Tooltip';

type Row = (string | number)[];

/** A card with its table twin. */
export function Card({
  title,
  head,
  rows,
  className,
  children,
}: {
  title: string;
  head: string[];
  rows: Row[];
  className?: string;
  children: ReactNode;
}) {
  const [table, setTable] = useState(false);
  return (
    <section className={className ? `sh-card ${className}` : 'sh-card'} aria-label={title}>
      <header>
        <h3>{title}</h3>
        <IconButton
          icon="table"
          size="sm"
          title={table ? `Show ${title} as a chart` : `Show ${title} as a table`}
          aria-pressed={table}
          on={table}
          onClick={() => {
            setTable((t) => !t);
          }}
        />
      </header>
      {table ? (
        <div className="sh-twin" tabIndex={0} role="region" aria-label={`${title}, table`}>
          <table className="adm-table">
            <thead>
              <tr>
                {head.map((h, i) => (
                  <th key={h} className={i > 0 ? 'num' : undefined}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>
                  {r.map((c, j) => (
                    <td key={j} className={j > 0 ? 'num' : undefined}>
                      {c}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        children
      )}
    </section>
  );
}

function Bars({ items, cur, label }: { items: { name: string; v: number; n?: number }[]; cur: string; label: string }) {
  if (!items.length) return <p className="hint">Nothing yet.</p>;
  const max = Math.max(1, ...items.map((i) => Math.abs(i.v)));
  return (
    <div className="gpayees" role="list" aria-label={label}>
      {items.map((p) => (
        <div key={p.name} className="hb" role="listitem">
          <span className="hb-l">
            <span>{p.name}</span>
          </span>
          <span className="hb-t">
            <span
              className="hb-f"
              style={{ width: `${((Math.abs(p.v) / max) * 100).toFixed(2)}%`, background: 'var(--exp)' }}
            />
          </span>
          <span className="hb-n">{p.n !== undefined ? `${fmtInt(p.n)}×` : ''}</span>
          <span className="hb-v">{whole(p.v, cur)}</span>
        </div>
      ))}
    </div>
  );
}

const uncategorised = (n: string) => n || '(uncategorised)';

export function SummaryCard({ r, d }: { r: SharedGroupReport; d: SharedGroupDetail }) {
  const cur = r.currency;
  const me = d.members.find((m) => m.isMe);
  const mine = r.members.find((m) => m.memberId === me?.id);
  const span = r.firstOn && r.lastOn ? `${fmtDay(r.firstOn)} – ${fmtDay(r.lastOn)}` : 'no expenses yet';
  const rows: Row[] = [
    ['Spent', money(r.totalMinor, cur)],
    ['Expenses', fmtInt(r.count)],
    ['Days', fmtInt(r.days)],
    ['Per day', money(r.avgPerDayMinor, cur)],
    ...(mine ? [['Your share', money(mine.shareMinor, cur)] as Row] : []),
  ];
  return (
    <Card title="Summary" head={['', 'Value']} rows={rows} className="sh-sum">
      <div className="kpis">
        <div className="kpi">
          <span className="l">Spent</span>
          <span className="n">{money(r.totalMinor, cur)}</span>
          <span className="d">{plural(r.count, 'expense')}</span>
        </div>
        <div className="kpi">
          <span className="l">Per day</span>
          <span className="n">{money(r.avgPerDayMinor, cur)}</span>
          <span className="d">{plural(r.days, 'day')}</span>
        </div>
        <div className="kpi">
          <span className="l">Your share</span>
          <span className="n">{money(mine?.shareMinor ?? 0, cur)}</span>
          <span className="d">you paid {money(mine?.paidMinor ?? 0, cur)}</span>
        </div>
        <div className="kpi">
          <span className="l">Dates</span>
          <span className="n sh-span">{span}</span>
        </div>
      </div>
    </Card>
  );
}

export function CategoriesCard({ r }: { r: SharedGroupReport }) {
  const total = r.categories.reduce((a, c) => a + c.spentMinor, 0);
  return (
    <Card
      title="Categories"
      head={['Category', 'Spent', 'Share', 'Expenses']}
      rows={r.categories.map((c) => [
        uncategorised(c.name),
        money(c.spentMinor, r.currency),
        total ? fmtShare(c.spentMinor / total, 1) : '',
        fmtInt(c.count),
      ])}
    >
      <Bars
        label="Spending per category"
        cur={r.currency}
        items={r.categories.map((c) => ({ name: uncategorised(c.name), v: c.spentMinor, n: c.count }))}
      />
    </Card>
  );
}

export function PayeesCard({ r }: { r: SharedGroupReport }) {
  return (
    <Card
      title="Top payees"
      head={['Payee', 'Spent', 'Expenses']}
      rows={r.payees.map((p) => [p.name || '(no payee)', money(p.spentMinor, r.currency), fmtInt(p.count)])}
    >
      <Bars
        label="Top payees"
        cur={r.currency}
        items={r.payees.map((p) => ({ name: p.name || '(no payee)', v: p.spentMinor, n: p.count }))}
      />
    </Card>
  );
}

const DAY_W = 640;
const DAY_H = 120;

function DayBar({
  x,
  y,
  w,
  h,
  date,
  v,
  cur,
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  date: string;
  v: number;
  cur: string;
}) {
  const tip = useTip(<TipContent title={fmtDayMedium(date)} rows={[['var(--exp)', 'Spent', money(v, cur)]]} />);
  return <rect x={x} y={y} width={w} height={h} rx={1.5} style={{ fill: 'var(--exp)' }} {...tip} />;
}

export function DailyCard({ r }: { r: SharedGroupReport }) {
  const first = r.daily[0]?.date;
  const last = r.daily[r.daily.length - 1]?.date;
  let chart: ReactNode = <p className="hint">No expenses yet.</p>;
  if (first && last) {
    const start = Date.parse(`${first}T00:00:00Z`);
    const days = Math.round((Date.parse(`${last}T00:00:00Z`) - start) / 86_400_000) + 1;
    const max = Math.max(1, ...r.daily.map((d) => d.spentMinor));
    const step = DAY_W / days;
    const w = Math.max(1, Math.min(18, step - (step > 4 ? 2 : 0)));
    const chartH = DAY_H - 16;
    chart = (
      <Svg w={DAY_W} h={DAY_H} label="Spending per day">
        {r.daily.map((d) => {
          const i = Math.round((Date.parse(`${d.date}T00:00:00Z`) - start) / 86_400_000);
          const h = Math.max(1, (Math.max(0, d.spentMinor) / max) * (chartH - 4));
          return (
            <DayBar
              key={d.date}
              x={i * step + (step - w) / 2}
              y={chartH - h}
              w={w}
              h={h}
              date={d.date}
              v={d.spentMinor}
              cur={r.currency}
            />
          );
        })}
        <text x={0} y={DAY_H - 2} className="ax">
          {fmtDay(first)}
        </text>
        <text x={DAY_W} y={DAY_H - 2} textAnchor="end" className="ax">
          {fmtDay(last)}
        </text>
      </Svg>
    );
  }
  return (
    <Card
      title="Spending per day"
      head={['Day', 'Spent', 'Expenses']}
      rows={r.daily.map((d) => [fmtDay(d.date), money(d.spentMinor, r.currency), fmtInt(d.count)])}
      className="sh-wide"
    >
      {chart}
    </Card>
  );
}

export function LargestCard({ r, d }: { r: SharedGroupReport; d: SharedGroupDetail }) {
  const name = (id: string) => d.members.find((m) => m.id === id)?.displayName ?? 'Former member';
  return (
    <Card
      title="Largest expenses"
      head={['Expense', 'Amount', 'Paid by', 'Day']}
      rows={r.largest.map((e) => [e.payee, money(e.amountMinor, r.currency), name(e.paidBy), fmtDay(e.bookedOn)])}
    >
      {r.largest.length ? (
        <ol className="sh-largest">
          {r.largest.map((e) => (
            <li key={e.id}>
              <span className="nm">{e.payee}</span>
              <span className="muted">
                {name(e.paidBy)} · {fmtDay(e.bookedOn)}
              </span>
              <span className="vl">{money(e.amountMinor, r.currency)}</span>
            </li>
          ))}
        </ol>
      ) : (
        <p className="hint">No expenses yet.</p>
      )}
    </Card>
  );
}

export function PaidShareCard({ r, d }: { r: SharedGroupReport; d: SharedGroupDetail }) {
  const name = (id: string) => d.members.find((m) => m.id === id)?.displayName ?? 'Former member';
  const max = Math.max(1, ...r.members.flatMap((m) => [m.paidMinor, m.shareMinor]));
  return (
    <Card
      title="Paid vs share"
      head={['Member', 'Paid', 'Share', 'Difference']}
      rows={r.members.map((m) => [
        name(m.memberId),
        money(m.paidMinor, r.currency),
        money(m.shareMinor, r.currency),
        money(m.paidMinor - m.shareMinor, r.currency),
      ])}
    >
      <div className="sh-ps" role="list" aria-label="Paid vs share">
        {r.members.map((m) => (
          <div key={m.memberId} className="sh-ps-r" role="listitem">
            <span className="nm">{name(m.memberId)}</span>
            <span className="sh-ps-b">
              <span className="paid" style={{ width: `${((m.paidMinor / max) * 100).toFixed(1)}%` }} title="Paid" />
              <span className="share" style={{ width: `${((m.shareMinor / max) * 100).toFixed(1)}%` }} title="Share" />
            </span>
            <span className="vl">
              {whole(m.paidMinor, r.currency)} / {whole(m.shareMinor, r.currency)}
            </span>
          </div>
        ))}
        <p className="hint">
          <i className="sw paid" /> paid · <i className="sw share" /> share
        </p>
      </div>
    </Card>
  );
}
