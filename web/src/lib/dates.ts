// Date formatting (web/CLAUDE.md: only here and in format.ts). Instants (timestamps from the API)
// show in the browser's time zone; booking dates are date-only and never shift.

const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

/** An RFC 3339 instant as a local date and time, e.g. "21 Oct 2026, 14:05". */
export function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : dateTime.format(d);
}

/** A date-only value (YYYY-MM-DD) as dd.mm.yyyy, the fixed display form (Phase 7 decision). */
export function fmtDay(iso: string | undefined): string {
  if (!iso) return '';
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  return m ? `${m[3]}.${m[2]}.${m[1]}` : iso;
}

/** A YYYY-MM-DD date plus n days (calendar arithmetic in UTC, no time zone shifts). */
export function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number);
  const t = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1) + n * 86_400_000);
  return t.toISOString().slice(0, 10);
}

// Chart labels (legacy fmtMonth, fmtMon, fmtMonY, fmtDay, fmtD, fmtWd): browser locale, UTC.
const monthLong = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric', timeZone: 'UTC' });
const monthShort = new Intl.DateTimeFormat(undefined, { month: 'short', timeZone: 'UTC' });
const monthYear = new Intl.DateTimeFormat(undefined, { month: 'short', year: 'numeric', timeZone: 'UTC' });
const dayLong = new Intl.DateTimeFormat(undefined, {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
});
const dayMedium = new Intl.DateTimeFormat(undefined, {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
});
const weekday = new Intl.DateTimeFormat(undefined, { weekday: 'short', timeZone: 'UTC' });

/** A YYYY-MM or YYYY-MM-DD value as a UTC Date. */
export function utc(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number);
  return new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
}

/** "March 2025" */
export const fmtMonthLong = (month: string) => monthLong.format(utc(month));
/** "Mar" */
export const fmtMonthShort = (month: string) => monthShort.format(utc(month));
/** "Mar 2025" */
export const fmtMonthYear = (month: string) => monthYear.format(utc(month));
/** "Mon, 17 Mar 2025" */
export const fmtDayLong = (iso: string) => dayLong.format(utc(iso));
/** "17 Mar 2025" */
export const fmtDayMedium = (iso: string) => dayMedium.format(utc(iso));
/** "Mon" for a date's weekday. */
export const fmtWeekday = (iso: string) => weekday.format(utc(iso));

/** A YYYY-MM month plus n months. */
export function addMonths(month: string, n: number): string {
  const d = utc(month);
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + n, 1)).toISOString().slice(0, 7);
}

/** Days from a to b (YYYY-MM-DD), b exclusive. */
export function daysBetween(a: string, b: string): number {
  return Math.round((utc(b).getTime() - utc(a).getTime()) / 86_400_000);
}

/** Monday-first weekday index of a date: Mon 0 … Sun 6. */
export function weekdayMon(iso: string): number {
  return (utc(iso).getUTCDay() + 6) % 7;
}
