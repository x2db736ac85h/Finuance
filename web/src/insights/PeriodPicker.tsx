// The period picker of the insights pane (legacy renderVizHead): all time, presets, every month
// and year of the view's data, and a custom range; months, years and custom ranges step with the
// arrows. The period applies to the charts and the list alike.

import { useAccounts } from '../api/hooks/accounts';
import { addDays, addMonths, daysBetween, fmtMonthLong } from '../lib/dates';
import type { CustomRange, PeriodPref } from '../stores/prefs';
import { useViews, viewOf } from '../stores/viewStore';
import { IconButton } from '../ui/IconButton';

type Props = {
  viewId: string;
  /** The resolved period of the current query (label for periods outside the data, custom default). */
  resolved: { from: string; to: string; label: string } | undefined;
  dataRange: { min: string; max: string } | undefined;
};

/** The data's first and last booking dates in the view's accounts (unfiltered, like the legacy dataset). */
function useSpan(viewId: string): { first: string; last: string } | null {
  const accounts = useAccounts().data ?? [];
  const list = viewId === 'all' ? accounts : accounts.filter((a) => a.id === viewId);
  let first = '';
  let last = '';
  for (const a of list) {
    if (a.firstOn && (!first || a.firstOn < first)) first = a.firstOn;
    if (a.lastOn && (!last || a.lastOn > last)) last = a.lastOn;
  }
  return first && last ? { first, last } : null;
}

/** Months (newest first) and years of a span. */
export function periodOptions(span: { first: string; last: string } | null): { months: string[]; years: number[] } {
  if (!span) return { months: [], years: [] };
  const months: string[] = [];
  for (let m = span.last.slice(0, 7); m >= span.first.slice(0, 7); m = addMonths(m, -1)) months.push(m);
  const years: number[] = [];
  for (let y = Number(span.last.slice(0, 4)); y >= Number(span.first.slice(0, 4)); y--) years.push(y);
  return { months, years };
}

/** The period one step before or after (legacy stepPeriod); null when it can't step. */
export function stepped(
  period: PeriodPref,
  custom: CustomRange | null,
  d: number,
): { period: PeriodPref; custom?: CustomRange } | null {
  if (period.startsWith('m:')) return { period: `m:${addMonths(period.slice(2), d)}` };
  if (period.startsWith('y:')) return { period: `y:${Number(period.slice(2)) + d}` };
  if (period === 'custom' && custom) {
    const len = daysBetween(custom.from, custom.to) + 1;
    return { period, custom: { from: addDays(custom.from, d * len), to: addDays(custom.to, d * len) } };
  }
  return null;
}

const PRESETS: readonly [PeriodPref, string][] = [
  ['l30', 'Last 30 days'],
  ['l3m', 'Last 3 months'],
  ['l12m', 'Last 12 months'],
  ['ytd', 'Year to date'],
];

export function PeriodPicker({ viewId, resolved, dataRange }: Props) {
  const period = useViews((s) => viewOf(s.views, viewId).period);
  const custom = useViews((s) => viewOf(s.views, viewId).custom);
  const patch = useViews((s) => s.patch);
  const span = useSpan(viewId);
  const { months, years } = periodOptions(span);
  const known =
    period === 'all' ||
    period === 'custom' ||
    PRESETS.some(([p]) => p === period) ||
    months.some((m) => `m:${m}` === period) ||
    years.some((y) => `y:${y}` === period);
  const step = (d: number) => {
    const n = stepped(period, custom, d);
    if (n) patch(viewId, n);
  };
  const canStep = stepped(period, custom, 1) !== null;
  const setCustom = (from: string, to: string) => {
    if (!from || !to) return;
    patch(viewId, { custom: from <= to ? { from, to } : { from: to, to: from } });
  };

  return (
    <div className="per">
      <IconButton
        icon="left"
        title="Previous period"
        disabled={!canStep}
        onClick={() => {
          step(-1);
        }}
      />
      <select
        className="sel"
        aria-label="Period"
        title="Period — applies to charts and the operations list"
        value={period}
        disabled={!span}
        onChange={(e) => {
          const p = e.target.value;
          if (p === 'custom' && !custom) {
            // start from the period shown, else the whole data (legacy)
            const r = resolved
              ? { from: resolved.from, to: addDays(resolved.to, -1) }
              : span
                ? { from: span.first, to: span.last }
                : dataRange
                  ? { from: dataRange.min, to: dataRange.max }
                  : null;
            patch(viewId, r ? { period: p, custom: r } : { period: p });
            return;
          }
          patch(viewId, { period: p });
        }}
      >
        <option value="all">All time</option>
        {span && (
          <>
            {PRESETS.map(([p, label]) => (
              <option key={p} value={p}>
                {label}
              </option>
            ))}
            <optgroup label="Month">
              {months.map((m) => (
                <option key={m} value={`m:${m}`}>
                  {fmtMonthLong(m)}
                </option>
              ))}
            </optgroup>
            <optgroup label="Year">
              {years.map((y) => (
                <option key={y} value={`y:${y}`}>
                  {y}
                </option>
              ))}
            </optgroup>
            <option value="custom">Custom range…</option>
          </>
        )}
        {!known && <option value={period}>{resolved?.label ?? period}</option>}
      </select>
      <IconButton
        icon="right"
        title="Next period"
        disabled={!canStep}
        onClick={() => {
          step(1);
        }}
      />
      {period === 'custom' && (
        <span className="per-c">
          <input
            type="date"
            className="inp"
            aria-label="From"
            value={custom?.from ?? ''}
            onChange={(e) => {
              setCustom(e.target.value, custom?.to ?? e.target.value);
            }}
          />
          –
          <input
            type="date"
            className="inp"
            aria-label="To"
            value={custom?.to ?? ''}
            onChange={(e) => {
              setCustom(custom?.from ?? e.target.value, e.target.value);
            }}
          />
        </span>
      )}
    </div>
  );
}
