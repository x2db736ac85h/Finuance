import { ApiError } from '../api/client';
import { fmtBytes, fmtInt } from './format';

/** What each limit allows, for the limit_reached message (Phase 19). */
const LIMIT_TEXT: Record<string, (v: number) => string> = {
  maxUploadBytes: (v) => `files up to ${fmtBytes(v)}`,
  importsPerDay: (v) => `${fmtInt(v)} imports a day`,
  maxAccounts: (v) => `${fmtInt(v)} account${v === 1 ? '' : 's'}`,
  maxGroups: (v) => `${fmtInt(v)} group${v === 1 ? '' : 's'}`,
  maxSharedGroups: (v) => `${fmtInt(v)} shared group${v === 1 ? '' : 's'}`,
  assistantPerHour: (v) => `${fmtInt(v)} assistant questions an hour`,
};

/** Limits that are not the account's own: the whole sentence. */
const LIMIT_SENTENCE: Record<string, (v: number) => string> = {
  // a connection's budget, across everyone (ADR 0045)
  connectionTokensPerDay: (v) =>
    `This connection's budget for today (${fmtInt(v)} tokens, for everyone) is used up. It starts again tomorrow (UTC); another connection may still answer.`,
};

/** A sentence for the user: the server's explanation, or what to do about a network failure. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    const limit = e.problem.limit;
    const sentence = limit && LIMIT_SENTENCE[limit.name];
    if (limit && sentence) return sentence(limit.value);
    const text = limit && LIMIT_TEXT[limit.name];
    if (limit && text) return `Your account allows ${text(limit.value)}. Ask an administrator if you need more.`;
    const field = e.problem.errors?.[0];
    if (field) return field.message;
    const message = e.problem.detail ?? e.problem.title;
    // A failure on the server's side: its request id is what an administrator can look up in the
    // logs and traces, so the person has something to quote (PLAN 24.8).
    if (e.status >= 500 && e.problem.requestId) return `${message} (Reference: ${e.problem.requestId})`;
    return message;
  }
  return "Can't reach the Finuance server. Check your connection and try again.";
}
