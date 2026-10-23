// Budgets (legacy CHARTS.o_budget): each category's spending this month against its budget, with
// the pace marker (where spending would be today at an even pace) in a month in progress. A budget
// is edited in place (PUT /budgets/{categoryId}); emptying the field goes back to the default, the
// rounded average of up to three earlier months. Uncategorised spending has the default only.

import type { Schemas } from '../api/client';
import { useSetBudget } from '../api/hooks/overview';
import { money, whole } from '../charts/lib/money';
import { Empty, type ChartDef, type ChartParts } from '../charts/types';
import { currencyExponent, minorToDecimal, parseMoneyInput } from '../lib/format';
import { toast } from '../ui/toastStore';
import { colorsOf, useOverviewParts, type OverviewData } from './data';

type Row = Schemas['BudgetRow'];
type Status = Row['status'];

/** Status icon, label and colour (legacy STATUS). */
export const STATUS: Record<Status, [icon: string, label: string, color: string]> = {
  ok: ['✓', 'On track', 'var(--st-good)'],
  ahead: ['!', 'Ahead of pace', 'var(--st-warn)'],
  over: ['▲', 'Over budget', 'var(--st-crit)'],
  none: ['?', 'No budget', 'var(--st-warn)'],
};

const pct = (v: number) => `${Math.max(0, Math.min(100, v * 100)).toFixed(1)}%`;

/** A budget as the input shows it: "900", or "85.5" with a fraction (legacy: the number). */
export function inputText(minor: number, cur: string): string {
  const s = minorToDecimal(minor, currencyExponent(cur));
  return s.includes('.') ? s.replace(/\.?0+$/, '') : s;
}

function BudgetInput({ row, cur }: { row: Row; cur: string }) {
  const set = useSetBudget();
  const id = row.category?.id;
  const text = inputText(row.budgetMinor, cur);
  const commit = (el: HTMLInputElement) => {
    const value = el.value;
    if (!id || value === text) return;
    const v = parseMoneyInput(value, cur);
    // empty or unreadable: back to the default budget (legacy)
    const amountMinor = value.trim() === '' || v === null ? null : Math.max(0, v);
    if (amountMinor === null ? row.budgetIsDefault : amountMinor === row.budgetMinor && !row.budgetIsDefault) {
      el.value = text; // nothing changes
      return;
    }
    set.mutate(
      { categoryId: id, amountMinor },
      {
        onError: () => {
          toast('Could not save the budget — try again', { kind: 'error' });
        },
      },
    );
  };
  return (
    <input
      // a new value from the server replaces what was typed
      key={`${text}|${String(row.budgetIsDefault)}`}
      className="bud-in"
      defaultValue={text}
      inputMode="decimal"
      aria-label={`Budget for ${row.name}`}
      title="Click to change this budget"
      onBlur={(e) => {
        commit(e.currentTarget);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur();
      }}
    />
  );
}

function BudgetRow({
  row,
  total,
  status,
  color,
  pace,
  partial,
  cur,
}: {
  row: Row;
  total?: boolean;
  status: Status;
  color?: string;
  pace: number;
  partial: boolean;
  cur: string;
}) {
  const [icon, label, col] = STATUS[status];
  const left = row.budgetMinor - row.curMinor;
  const fill = row.budgetMinor ? row.curMinor / row.budgetMinor : row.curMinor ? 1 : 0;
  return (
    <div className={total ? 'br tot' : 'br'}>
      <span className="bn">
        {!total && <i className="sw" style={{ background: color }} />}
        <span>{row.name}</span>
      </span>
      <span className="bbar">
        <span className="bfill" style={{ width: pct(fill), background: col }} />
        {partial && <span className="bpace" style={{ left: pct(pace) }} />}
      </span>
      <span className="bv">
        {whole(row.curMinor, cur)} /{' '}
        {total || !row.category ? whole(row.budgetMinor, cur) : <BudgetInput row={row} cur={cur} />}
      </span>
      <span className="bst">
        <b style={{ color: col }}>{icon}</b>{' '}
        {status === 'over' ? `over ${whole(-left, cur)}` : status === 'none' ? label : `${whole(left, cur)} left`}
      </span>
    </div>
  );
}

export function budgetsParts(d: OverviewData, cur: string): ChartParts {
  const sub = d.avgMonths ? `default = ${d.avgMonths}-month average` : 'set your own limits';
  const legend = (
    <span>
      <i className="pace-k" /> expected by today
    </span>
  );
  const rows = d.categories;
  if (!rows.length)
    return { loading: false, error: false, sub, legend, body: <Empty>No spending yet</Empty>, table: null };
  const pace = d.elapsedDays / d.daysInMonth;
  const color = colorsOf(d);
  const tb = rows.reduce((a, r) => a + r.budgetMinor, 0);
  const ts = rows.reduce((a, r) => a + r.curMinor, 0);
  const totalRow: Row = {
    name: 'All categories',
    curMinor: ts,
    prevMinor: 0,
    avg3Minor: 0,
    budgetMinor: tb,
    budgetIsDefault: true,
    status: ts > tb ? 'over' : 'ok',
  };
  return {
    loading: false,
    error: false,
    sub,
    legend,
    table: {
      cols: ['Category', 'Spent', 'Budget', 'Left', 'Status'],
      num: [false, true, true, true, false],
      rows: rows.map((r) => [
        r.name,
        money(r.curMinor, cur),
        money(r.budgetMinor, cur),
        money(r.budgetMinor - r.curMinor, cur),
        STATUS[r.status][1],
      ]),
    },
    body: (
      <div className="bud">
        <BudgetRow row={totalRow} total status={totalRow.status} pace={pace} partial={d.partial} cur={cur} />
        {rows.map((r) => (
          <BudgetRow
            key={r.category?.id ?? r.name}
            row={r}
            status={r.status}
            color={color(r.name)}
            pace={pace}
            partial={d.partial}
            cur={cur}
          />
        ))}
      </div>
    ),
  };
}

export const budgetsChart: ChartDef = {
  key: 'o_budget',
  title: 'Budgets',
  use: (q, env) => useOverviewParts(q, env, budgetsParts),
};
