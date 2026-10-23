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

/** A money amount in minor units with its ISO 4217 code (the API's Money). */
export type Money = { amountMinor: number; currency: string };

const exponents = new Map<string, number>();

/** Minor-unit digits of a currency (2 for PLN, 0 for JPY), from Intl; 2 for unknown codes. */
export function currencyExponent(currency: string): number {
  let e = exponents.get(currency);
  if (e === undefined) {
    try {
      e = new Intl.NumberFormat('en', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2;
    } catch {
      e = 2;
    }
    exponents.set(currency, e);
  }
  return e;
}

/**
 * Minor units as an exact decimal string ("-1234.56"), built from the digits so no float rounding
 * is involved (CLAUDE.md: no float for money).
 */
export function minorToDecimal(minor: number, exponent: number): `${number}` {
  const neg = minor < 0;
  const digits = String(Math.abs(Math.trunc(minor))).padStart(exponent + 1, '0');
  const whole = digits.slice(0, digits.length - exponent);
  const frac = exponent ? `.${digits.slice(digits.length - exponent)}` : '';
  return `${neg ? '-' : ''}${whole}${frac}` as `${number}`;
}

const moneyFormats = new Map<number, Intl.NumberFormat>();

/**
 * "-1 234,56" in the browser locale; with currency: "-1 234,56 PLN" (legacy money()). `whole`
 * rounds to whole units ("-1 235", legacy nf0), from the exact decimal string.
 */
export function fmtMoney(m: Money, opts: { currency?: boolean; whole?: boolean } = {}): string {
  const exp = currencyExponent(m.currency);
  const digits = opts.whole ? 0 : exp;
  let nf = moneyFormats.get(digits);
  if (!nf) {
    nf = new Intl.NumberFormat(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits });
    moneyFormats.set(digits, nf);
  }
  const s = nf.format(minorToDecimal(m.amountMinor, exp));
  return opts.currency ? `${s} ${m.currency}` : s;
}

/** "1.2 MB" (legacy fmtSize). */
export function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${fmtInt(bytes)} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1048576).toFixed(1)} MB`;
}

const compact = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 });

/** Compact axis numbers without currency: "12.3K" (legacy cpt). */
export function fmtCompact(m: Money): string {
  return compact.format(minorToDecimal(m.amountMinor, currencyExponent(m.currency)));
}

/** A ratio as a percentage with fixed decimals, 0.4412 → "44.1%" (legacy toFixed, not localised). */
export function fmtShare(x: number, digits = 0): string {
  return `${(x * 100).toFixed(digits)}%`;
}
