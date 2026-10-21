// The API client (PLAN task 8.2): openapi-fetch typed from api/openapi.yaml. Components never call
// it directly; they use the hooks in ./hooks.

import createClient, { type Middleware } from 'openapi-fetch';

import type { components, paths } from './schema';

export type Problem = components['schemas']['Problem'];
export type Schemas = components['schemas'];

/** A failed request, carrying the server's RFC 9457 problem (or one made up for network errors). */
export class ApiError extends Error {
  readonly problem: Problem;
  constructor(problem: Problem) {
    super(problem.detail ?? problem.title);
    this.name = 'ApiError';
    this.problem = problem;
  }
  get status(): number {
    return this.problem.status;
  }
  get code(): Problem['code'] {
    return this.problem.code;
  }
}

/** Routes that answer 401 as part of their own flow (wrong credentials), not "signed out". */
const OWN_401 = ['/auth/login', '/auth/signup'];

let onUnauthenticated: (next: string) => void = (next) => {
  window.location.assign(`/login?next=${encodeURIComponent(next)}`);
};

/** The router replaces the default full-page redirect with a client-side navigation. */
export function setUnauthenticatedHandler(fn: (next: string) => void): void {
  onUnauthenticated = fn;
}

async function toProblem(res: Response): Promise<Problem> {
  try {
    const body = (await res.clone().json()) as Partial<Problem>;
    if (typeof body.code === 'string' && typeof body.title === 'string') {
      return { ...body, status: res.status } as Problem;
    }
  } catch {
    // not JSON: fall through
  }
  return {
    type: '/problems/internal',
    title: res.statusText || 'Request failed',
    status: res.status,
    code: 'internal',
  };
}

export const errorMiddleware: Middleware = {
  async onResponse({ response, schemaPath }) {
    if (response.ok) return response;
    const problem = await toProblem(response);
    if (response.status === 401 && !OWN_401.includes(schemaPath)) {
      const here = window.location.pathname + window.location.search;
      if (!here.startsWith('/login') && !here.startsWith('/signup')) onUnauthenticated(here);
    }
    throw new ApiError(problem);
  },
};

export const api = createClient<paths>({
  baseUrl: `${window.location.origin}/api/v1`,
  credentials: 'same-origin',
  // CSRF (ADR 0011): every unsafe request must carry this header.
  headers: { 'X-Finuance-Client': 'web' },
  // Looked up per call, so test interceptors (MSW) installed after import still apply.
  fetch: (request: Request) => globalThis.fetch(request),
});
api.use(errorMiddleware);

/** Unwraps an openapi-fetch result whose errors the middleware already turned into ApiError. */
export function data<T>(r: { data?: T; error?: unknown }): T {
  if (r.error !== undefined) throw r.error instanceof ApiError ? r.error : new Error('unexpected API error');
  return r.data as T;
}
