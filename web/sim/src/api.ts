// The control API's client (api/sim.openapi.yaml): openapi-fetch with the control token and the
// client header on every request. The token is kept for this tab only (sessionStorage) and never
// put in a URL the server sees: `make observe` hands it over in the address's #fragment.
import createClient, { type Middleware } from 'openapi-fetch';

import type { components, paths } from './api/schema';

export type Schemas = components['schemas'];
export type Problem = Schemas['Problem'];

const KEY = 'finuance-sim-token';

export function readToken(): string {
  try {
    const fromHash = new URLSearchParams(window.location.hash.slice(1)).get('token');
    if (fromHash) {
      window.sessionStorage.setItem(KEY, fromHash);
      window.history.replaceState(null, '', window.location.pathname);
      return fromHash;
    }
    return window.sessionStorage.getItem(KEY) ?? '';
  } catch {
    return '';
  }
}

export function saveToken(token: string): void {
  try {
    if (token) window.sessionStorage.setItem(KEY, token);
    else window.sessionStorage.removeItem(KEY);
  } catch {
    // private mode: the token lives as long as the page
  }
}

/** A refused request, with the problem the simulator answered. */
export class ControlError extends Error {
  readonly status: number;
  constructor(status: number, problem?: Problem) {
    super(problem?.title ?? `The simulator answered ${String(status)}.`);
    this.name = 'ControlError';
    this.status = status;
  }
}

export function headers(token: string): Record<string, string> {
  return { Authorization: `Bearer ${token}`, 'X-Finuance-Client': 'web' };
}

export function makeClient(token: string) {
  const client = createClient<paths>({ baseUrl: `${window.location.origin}/api` });
  const auth: Middleware = {
    onRequest({ request }) {
      for (const [k, v] of Object.entries(headers(token))) request.headers.set(k, v);
      return request;
    },
  };
  client.use(auth);
  return client;
}

export type Client = ReturnType<typeof makeClient>;

/** The data of an answer, or the problem as a ControlError. */
export function data<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.data === undefined) throw new ControlError(r.response.status, r.error as Problem | undefined);
  return r.data;
}
