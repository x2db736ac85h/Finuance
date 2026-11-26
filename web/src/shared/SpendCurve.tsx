// The Shared page's one chart (Phase 27): what all groups together cost over time, as a running
// total that steps up where each group starts. Deliberately small: the per-year table beside it
// holds the figures, so the curve needs no axis labels beyond its two ends.
import { Svg } from '../charts/lib/Svg';
import type { TimelineGroup } from '../api/hooks/shared';
import { money } from '../charts/lib/money';
import { fmtDay } from '../lib/dates';

export type CurvePoint = { on: string; totalMinor: number };

/**
 * The running total of what the groups spent, in the order they began. Groups in another currency
 * than `currency` and groups without expenses are left out (they can't be added up).
 */
export function curveOf(groups: readonly TimelineGroup[], currency: string): CurvePoint[] {
  const dated = groups
    .filter((g) => g.currency === currency && g.firstOn && g.spentMinor > 0)
    .map((g) => ({ on: g.firstOn ?? '', spent: g.spentMinor }))
    .sort((a, b) => a.on.localeCompare(b.on));
  let total = 0;
  const out: CurvePoint[] = [];
  for (const g of dated) {
    total += g.spent;
    // two groups that began on the same day are one step
    if (out.at(-1)?.on === g.on) out[out.length - 1] = { on: g.on, totalMinor: total };
    else out.push({ on: g.on, totalMinor: total });
  }
  return out;
}

const day = (iso: string) => Date.parse(`${iso}T00:00:00Z`) / 86_400_000;

export function SpendCurve({ groups, currency }: { groups: readonly TimelineGroup[]; currency: string }) {
  const pts = curveOf(groups, currency);
  const first = pts[0];
  const last = pts.at(-1);
  // one group is one number, which the table already shows
  if (!first || !last || pts.length < 2) return null;
  const w = 360;
  const h = 96;
  const pad = 6;
  const x0 = day(first.on);
  const span = Math.max(1, day(last.on) - x0);
  const x = (iso: string) => pad + ((day(iso) - x0) / span) * (w - 2 * pad);
  const y = (minor: number) => h - pad - (minor / last.totalMinor) * (h - 2 * pad);
  // a step line: flat until the next group begins, then up
  let d = `M${x(first.on).toFixed(1)} ${(h - pad).toFixed(1)}V${y(first.totalMinor).toFixed(1)}`;
  for (const p of pts.slice(1)) d += `H${x(p.on).toFixed(1)}V${y(p.totalMinor).toFixed(1)}`;
  const area = `${d}V${(h - pad).toFixed(1)}Z`;
  return (
    <figure className="sh-curve">
      <Svg
        w={w}
        h={h}
        label={`Spent in all groups over time: ${money(last.totalMinor, currency)} from ${fmtDay(first.on)} to ${fmtDay(last.on)}`}
      >
        <path className="sh-curve-area" d={area} />
        <path className="sh-curve-line" d={d} />
        {pts.map((p) => (
          <circle key={p.on} className="sh-curve-dot" cx={x(p.on)} cy={y(p.totalMinor)} r={2.2} />
        ))}
      </Svg>
      <figcaption>
        <span>{fmtDay(first.on)}</span>
        <b>{money(last.totalMinor, currency)} in all</b>
        <span>{fmtDay(last.on)}</span>
      </figcaption>
    </figure>
  );
}
