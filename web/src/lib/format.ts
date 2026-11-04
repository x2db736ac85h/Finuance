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

/**
 * A typed amount as minor units (legacy parseNum for inputs): spaces and a currency code or sign
 * are ignored, and "1 234,56", "1,234.56", "1234.5" and "-80" all read; extra decimals round half
 * away from zero. null for anything else (also empty text). Exact: the digits are moved, never
 * multiplied as floats.
 */
export function parseMoneyInput(text: string, currency: string): number | null {
  let s = text
    .trim()
    .replace(/[\s\u00a0\u202f']/g, '')
    .replace(/^(PLN|EUR|USD|GBP|CHF|zł|\$|€|£)|(PLN|EUR|USD|GBP|CHF|zł|\$|€|£)$/i, '');
  if (!/^[-+]?[\d.,]+$/.test(s) || !/\d/.test(s)) return null;
  const lc = s.lastIndexOf(',');
  const ld = s.lastIndexOf('.');
  if (lc > -1 && ld > -1) s = lc > ld ? s.replace(/\./g, '').replace(',', '.') : s.replace(/,/g, '');
  else if (lc > -1) s = s.split(',').length > 2 ? s.replace(/,/g, '') : s.replace(',', '.');
  else if (ld > -1 && s.split('.').length > 2) s = s.replace(/\./g, '');
  const m = /^([-+]?)(\d*)(?:\.(\d*))?$/.exec(s);
  if (!m) return null;
  const exp = currencyExponent(currency);
  const frac = m[3] ?? '';
  const digits = (m[2] ?? '') + frac.slice(0, exp).padEnd(exp, '0');
  if (digits.replace(/^0+/, '').length > 15) return null;
  let v = Number(digits || '0');
  if ((frac[exp] ?? '0') >= '5') v += 1;
  return m[1] === '-' && v !== 0 ? -v : v;
}

/** Minor units as text to edit, without grouping: 2450 → "24,50" (parseMoneyInput reads it back). */
export function moneyInputText(minor: number, currency: string): string {
  const exp = currencyExponent(currency);
  const digits = String(Math.abs(minor)).padStart(exp + 1, '0');
  const text = exp ? `${digits.slice(0, -exp)},${digits.slice(-exp)}` : digits;
  return minor < 0 ? `-${text}` : text;
}

const oneDecimal = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });

/** A size in binary units: 2048 → "2 KiB", 20971520 → "20 MiB". */
export function fmtBytes(n: number): string {
  const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? fmtInt(v) : oneDecimal.format(v)} ${units[i] ?? ''}`;
}
