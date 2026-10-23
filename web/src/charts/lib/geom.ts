// Chart geometry, ported from the legacy helpers (DESIGN §13.4): bars with a rounded data end,
// donut segments, sparklines and "nice" rounding. Everything is in logical units; <Svg> scales.

/**
 * A bar anchored to its baseline with a 3-unit rounded data end (legacy barPath). With top the bar
 * grows up from y + h; otherwise it hangs down from y. Bars under half a unit draw nothing.
 */
export function barPath(x: number, y: number, w: number, h: number, top: boolean): string {
  if (h < 0.5) return '';
  const r = Math.min(3, w / 2, h);
  return top
    ? `M${x} ${y + h}V${y + r}Q${x} ${y} ${x + r} ${y}H${x + w - r}Q${x + w} ${y} ${x + w} ${y + r}V${y + h}Z`
    : `M${x} ${y}V${y + h - r}Q${x} ${y + h} ${x + r} ${y + h}H${x + w - r}Q${x + w} ${y + h} ${x + w} ${y + h - r}V${y}Z`;
}

/**
 * A donut segment from angle a0 to a1 (radians, clockwise from 3 o'clock) between radii R and r
 * (legacy arc). A full turn is shortened by a hair so the arc stays drawable; single-category
 * donuts draw a ring instead.
 */
export function arc(cx: number, cy: number, R: number, r: number, a0: number, a1: number): string {
  if (a1 - a0 >= Math.PI * 2 - 1e-4) a1 = a0 + Math.PI * 2 - 1e-4;
  const large = a1 - a0 > Math.PI ? 1 : 0;
  const p = (rad: number, a: number) => `${(cx + rad * Math.cos(a)).toFixed(2)} ${(cy + rad * Math.sin(a)).toFixed(2)}`;
  return `M${p(R, a0)}A${R} ${R} 0 ${large} 1 ${p(R, a1)}L${p(r, a1)}A${r} ${r} 0 ${large} 0 ${p(r, a0)}Z`;
}

export type Spark = { d: string; last: [number, number] };

/**
 * A 110 × 30 sparkline through the finite values (legacy spark): null entries leave gaps in x.
 * Returns null with fewer than two points.
 */
export function spark(vals: readonly (number | null)[], w = 110, h = 30): Spark | null {
  const pts = vals.flatMap((v, i) => (v != null && Number.isFinite(v) ? [[i, v] as const] : []));
  if (pts.length < 2) return null;
  const ys = pts.map((p) => p[1]);
  const mn = Math.min(...ys);
  const mx = Math.max(...ys);
  const rg = mx - mn || 1;
  const X = (i: number) => 3 + (i / Math.max(1, vals.length - 1)) * (w - 6);
  const Y = (v: number) => 4 + (1 - (v - mn) / rg) * (h - 8);
  const d = pts.map(([i, v], j) => `${j ? 'L' : 'M'}${X(i).toFixed(1)} ${Y(v).toFixed(1)}`).join('');
  const [li, lv] = pts[pts.length - 1] ?? [0, 0];
  return { d, last: [X(li), Y(lv)] };
}

/** Budget rounding in major units (legacy roundNice): ≤ 0 → 0, under 100 up to 5s, else to 10s. */
export function roundNice(v: number): number {
  if (v <= 0) return 0;
  return v < 100 ? Math.ceil(v / 5) * 5 : Math.round(v / 10) * 10;
}

/** Quintile thresholds of positive values (legacy heat: the values at 20/40/60/80 %). */
export function quintiles(values: readonly number[]): number[] {
  const v = [...values].sort((a, b) => a - b);
  return [0.2, 0.4, 0.6, 0.8].map((p) => v[Math.floor(p * (v.length - 1))] ?? 0);
}

/** The heat bin of a value: 0 for nothing, 1–5 by the quintile thresholds. */
export function heatBin(v: number, q: readonly number[]): number {
  return v <= 0 ? 0 : 1 + q.filter((t) => v > t).length;
}
