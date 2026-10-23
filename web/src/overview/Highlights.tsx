// Highlights (legacy CHARTS.o_high): the month's facts as short sentences. The server picks the
// facts and their numbers (insights/highlights.go); the wording is here, with the amounts in bold.

import type { ReactNode } from 'react';

import type { Schemas } from '../api/client';
import { money } from '../charts/lib/money';
import type { ChartDef } from '../charts/types';
import { fmtMonthLong, fmtMonthShort } from '../lib/dates';
import { useOverviewParts } from './data';

type Highlight = Schemas['Highlight'];

const ICON: Record<Highlight['tone'], string> = { up: '▲', down: '▼', info: '•', good: '✓', warn: '!' };

/** A highlight as the legacy sentence, with its numbers in bold. */
export function sentence(h: Highlight, cur: string): ReactNode[] {
  const m = (x: number) => money(x, cur);
  const b = (s: string | number) => <b>{s}</b>;
  const parts = ((): ReactNode[] => {
    switch (h.kind) {
      case 'projection':
        return ['Projected month-end spending ', b(m(h.amountMinor)), ` — ${h.days} of ${h.of} days in.`];
      case 'vsPrev':
        return [
          `Spending is ${h.tone === 'up' ? 'up' : 'down'} `,
          b(`${h.pct.toFixed(0)}%`),
          ` vs ${h.month ? fmtMonthShort(h.month) : ''}${h.sameDays ? ' at the same point' : ''} (${m(h.amountMinor)} vs ${m(h.otherMinor)}).`,
        ];
      case 'vsAverage':
        return [`${h.tone === 'up' ? 'Above' : 'Below'} your ${h.months}-month average by `, b(m(h.amountMinor)), '.'];
      case 'topCategory':
        return [b(h.category), ` is the biggest category — ${h.pct.toFixed(0)}% of spending.`];
      case 'categoryUp':
        return [b(h.category), ' is ', b(m(h.amountMinor)), ' above its usual level.'];
      case 'categoryDown':
        return [b(h.category), ' is ', b(m(h.amountMinor)), ' below its usual level.'];
      case 'savings':
        return [
          'You kept ',
          b(`${h.pct.toFixed(0)}%`),
          ` of income (${h.months}-month average ${h.avgPct.toFixed(0)}%).`,
        ];
      case 'overspend':
        return ['You spent ', b(m(h.amountMinor)), ' more than you earned.'];
      case 'noIncome':
        return ['No income recorded this month.'];
      case 'noSpendDays':
        return [b(h.days), ` no-spend day${h.days === 1 ? '' : 's'} so far this month.`];
      case 'recurring':
        return [b(h.count), ' recurring payments ≈ ', b(m(h.amountMinor)), `/month (${m(h.otherMinor)}/year).`];
      case 'largest':
        return ['Largest expense: ', b(m(h.amountMinor)), ` at ${h.payee}.`];
    }
  })();
  // the parts are positional, so the index is their identity
  return parts.map((p, i) => <span key={i}>{p}</span>);
}

export const highlightsChart: ChartDef = {
  key: 'o_high',
  title: 'Highlights',
  expandable: false,
  use: (q, env) =>
    useOverviewParts(q, env, (d, cur) => ({
      loading: false,
      error: false,
      sub: fmtMonthLong(d.month),
      table: null,
      body: (
        <ul className="hls" tabIndex={0} aria-label="Highlights, list">
          {d.highlights.map((h, i) => (
            // the list is positional (the server's order), so the index is its identity
            <li key={i} className={`hl ${h.tone}`}>
              <span className="hl-i" aria-hidden="true">
                {ICON[h.tone]}
              </span>
              <span className="hl-t">{sentence(h, cur)}</span>
            </li>
          ))}
        </ul>
      ),
    })),
};
