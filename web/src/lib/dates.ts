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
