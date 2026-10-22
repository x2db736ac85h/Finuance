import { ApiError } from '../api/client';

/** A sentence for the user: the server's explanation, or what to do about a network failure. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    const field = e.problem.errors?.[0];
    if (field) return field.message;
    return e.problem.detail ?? e.problem.title;
  }
  return "Can't reach the Finuance server. Check your connection and try again.";
}
