// Money helpers for charts. Amounts stay integer minor units; only ratios are floats.

import { fmtMoney, type Money } from '../../lib/format';

/** a / n rounded half away from zero (the API's averages round the same way). */
export function divRound(a: number, n: number): number {
  const q = Math.trunc(a / n);
  const r = a - q * n;
  if (2 * Math.abs(r) >= n) return a < 0 ? q - 1 : q + 1;
  return q;
}

/** Legacy money(): 2 decimals with the currency code. */
export const money = (amountMinor: number, currency: string) => fmtMoney({ amountMinor, currency }, { currency: true });
/** Legacy nf: 2 decimals, no currency (the summary tiles; the currency is in the label). */
export const plain = (amountMinor: number, currency: string) => fmtMoney({ amountMinor, currency });
/** Legacy nf0: whole units (bar and legend values). */
export const whole = (amountMinor: number, currency: string) => fmtMoney({ amountMinor, currency }, { whole: true });

export const m = (amountMinor: number, currency: string): Money => ({ amountMinor, currency });
