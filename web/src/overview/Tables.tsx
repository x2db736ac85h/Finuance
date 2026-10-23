// The overview's table cards: Recurring payments (legacy CHARTS.o_recur, detected by the server's
// findRecurring port) and Largest expenses (legacy CHARTS.o_top; a click shows the row in the
// table, legacy gotoRow).

import type { Schemas } from '../api/client';
import { money, plain, whole } from '../charts/lib/money';
import { TipContent } from '../charts/lib/Tip';
import { Empty, type ChartDef, type ChartEnv, type ChartParts } from '../charts/types';
import { fmtDay, fmtDayMedium, fmtMonthLong, fmtMonthShort } from '../lib/dates';
import { useTip } from '../ui/Tooltip';
import { colorsOf, negMoney, useOverviewParts, type OverviewData } from './data';

type Recurring = Schemas['RecurringPayment'];

const dayOf = (iso: string) => Number(iso.slice(8, 10));

function RecurringRow({ r, cur, color }: { r: Recurring; cur: string; color: (n: string) => string }) {
  const tip = useTip(
    <TipContent
      title={r.payee}
      rows={[
        ['', 'Typical amount', negMoney(r.medianMinor, cur)],
        ['', 'Payments seen', String(r.count)],
        ['', 'Last charged', fmtDayMedium(r.last)],
        ['', 'Next expected', r.active ? fmtDayMedium(r.next) : 'not seen recently'],
      ]}
    />,
  );
  return (
    <tr className={r.active ? undefined : 'off'} {...tip}>
      <td>{r.payee}</td>
      <td>
        {r.category && (
          <>
            <i className="sw" style={{ background: color(r.category) }} /> {r.category}
          </>
        )}
      </td>
      <td className="r">{plain(r.medianMinor, cur)}</td>
      <td>{r.active ? `${fmtMonthShort(r.next)} ${dayOf(r.next)}` : <span className="muted">stopped?</span>}</td>
      <td className="r">{whole(r.yearlyMinor, cur)}</td>
    </tr>
  );
}

export function recurringParts(d: OverviewData, cur: string): ChartParts {
  const rec = d.recurring;
  if (!rec.length)
    return {
      loading: false,
      error: false,
      sub: '',
      body: <Empty>No monthly repeating payments detected yet (needs 3+ months of data).</Empty>,
      table: null,
    };
  const active = rec.filter((r) => r.active);
  const perMonth = active.reduce((a, r) => a + r.medianMinor, 0);
  const color = colorsOf(d);
  return {
    loading: false,
    error: false,
    sub: `${active.length} active · ≈ ${money(perMonth, cur)}/mo`,
    table: {
      cols: ['Payee', 'Category', 'Typical', 'Last', 'Next expected', 'Per year', 'Status'],
      num: [false, false, true, false, false, true, false],
      rows: rec.map((r) => [
        r.payee,
        r.category ?? '',
        negMoney(r.medianMinor, cur),
        fmtDayMedium(r.last),
        r.active ? fmtDayMedium(r.next) : '–',
        negMoney(r.yearlyMinor, cur),
        r.active ? 'active' : 'stopped?',
      ]),
    },
    body: (
      <div className="oscroll" tabIndex={0} role="region" aria-label="Recurring payments, list">
        <table className="otab">
          <colgroup>
            <col style={{ width: '34%' }} />
            <col style={{ width: '22%' }} />
            <col style={{ width: '16%' }} />
            <col style={{ width: '13%' }} />
            <col style={{ width: '15%' }} />
          </colgroup>
          <thead>
            <tr>
              <th>Payee</th>
              <th>Category</th>
              <th className="r">Typical</th>
              <th>Next</th>
              <th className="r">Per year</th>
            </tr>
          </thead>
          <tbody>
            {rec.map((r) => (
              <RecurringRow key={r.payee} r={r} cur={cur} color={color} />
            ))}
          </tbody>
        </table>
      </div>
    ),
  };
}

export const recurringChart: ChartDef = {
  key: 'o_recur',
  title: 'Recurring payments',
  use: (q, env) => useOverviewParts(q, env, recurringParts),
};

export function largestParts(d: OverviewData, cur: string, env: ChartEnv): ChartParts {
  const list = d.largest.slice(0, env.big ? 40 : 12);
  if (!list.length)
    return { loading: false, error: false, sub: '', body: <Empty>No expenses this month</Empty>, table: null };
  const color = colorsOf(d);
  return {
    loading: false,
    error: false,
    sub: fmtMonthLong(d.month),
    table: {
      cols: ['Date', 'Payee', 'Category', 'Amount'],
      num: [false, false, false, true],
      rows: list.map((x) => [fmtDay(x.bookedOn), x.payee, x.category?.name ?? '', money(x.amountMinor, cur)]),
    },
    body: (
      <div className="oscroll" tabIndex={0} role="region" aria-label="Largest expenses, list">
        <table className="otab">
          <colgroup>
            <col style={{ width: '17%' }} />
            <col style={{ width: '40%' }} />
            <col style={{ width: '23%' }} />
            <col style={{ width: '20%' }} />
          </colgroup>
          <thead>
            <tr>
              <th>Date</th>
              <th>Payee</th>
              <th>Category</th>
              <th className="r">Amount</th>
            </tr>
          </thead>
          <tbody>
            {list.map((x) => (
              <tr
                key={x.id}
                data-a=""
                title="Show in the list"
                onClick={() => {
                  env.actions.locate(x.id);
                  env.actions.showTable();
                }}
              >
                <td className="mono">{fmtDay(x.bookedOn)}</td>
                <td>{x.payeeGroup}</td>
                <td>
                  {x.category && (
                    <>
                      <i className="sw" style={{ background: color(x.category.name) }} /> {x.category.name}
                    </>
                  )}
                </td>
                <td className="r neg">{plain(x.amountMinor, cur)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    ),
  };
}

export const largestChart: ChartDef = {
  key: 'o_top',
  title: 'Largest expenses',
  use: (q, env) => useOverviewParts(q, env, largestParts),
};
