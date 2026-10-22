// Date formatting (web/CLAUDE.md: only here and in format.ts). Instants (timestamps from the API)
// show in the browser's time zone; booking dates are date-only and never shift.

const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

/** An RFC 3339 instant as a local date and time, e.g. "21 Oct 2026, 14:05". */
export function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : dateTime.format(d);
}
