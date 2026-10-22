// Number formatting (web/CLAUDE.md: format money and dates only here and in dates.ts). Money
// formatting arrives with the grid; Phase 8 needs counts and percentages.

const intf = new Intl.NumberFormat();

/** A count with the browser locale's grouping (legacy intf). */
export function fmtInt(n: number): string {
  return intf.format(n);
}

/** "1 transaction" / "312 transactions". */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** A scale factor as a whole percentage: 1.12 → "112%". */
export function fmtPct(x: number): string {
  return `${Math.round(x * 100)}%`;
}
