import { ApiError } from '../api/client';
import { fmtBytes, fmtInt } from './format';

/** What each limit allows, for the limit_reached message (Phase 19). */
const LIMIT_TEXT: Record<string, (v: number) => string> = {
  maxUploadBytes: (v) => `files up to ${fmtBytes(v)}`,
  importsPerDay: (v) => `${fmtInt(v)} imports a day`,
  maxAccounts: (v) => `${fmtInt(v)} account${v === 1 ? '' : 's'}`,
  maxGroups: (v) => `${fmtInt(v)} group${v === 1 ? '' : 's'}`,
  assistantPerHour: (v) => `${fmtInt(v)} assistant questions an hour`,
};

/** A sentence for the user: the server's explanation, or what to do about a network failure. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    const limit = e.problem.limit;
    const text = limit && LIMIT_TEXT[limit.name];
    if (limit && text) return `Your account allows ${text(limit.value)}. Ask an administrator if you need more.`;
    const field = e.problem.errors?.[0];
    if (field) return field.message;
    return e.problem.detail ?? e.problem.title;
  }
  return "Can't reach the Finuance server. Check your connection and try again.";
}
