// The app's addresses, built in one place (Phase 26, U1). The table is /transactions/<view>, the
// overview /overview/<view> and the Shared page /shared; a view is an account's id or "all".

export type ViewMode = 'table' | 'ov';

export const SHARED = '/shared';

export const txPath = (viewId = 'all'): string => `/transactions/${viewId}`;
export const overviewPath = (viewId = 'all'): string => `/overview/${viewId}`;
export const viewPath = (mode: ViewMode, viewId = 'all'): string =>
  mode === 'ov' ? overviewPath(viewId) : txPath(viewId);
export const sharedPath = (groupId?: string): string => (groupId ? `${SHARED}/${groupId}` : SHARED);

/** True on the Shared page and on one of its groups. */
export const inSharedPath = (pathname: string): boolean => pathname === SHARED || pathname.startsWith(`${SHARED}/`);

/** True on the table or the overview of a view. */
export const isViewPath = (pathname: string): boolean => /^\/(transactions|overview)\/[^/]+$/.test(pathname);

/**
 * Table or overview, read from the address. The address is the only place that says which: a
 * second copy elsewhere changes a render earlier or later than the page does (U2).
 */
export const modeOf = (pathname: string): ViewMode => (pathname.startsWith('/overview/') ? 'ov' : 'table');

/** The view an address shows: the account's id, or "all" anywhere else. */
export function viewIdOf(pathname: string): string {
  const m = /^\/(?:transactions|overview)\/([^/]+)$/.exec(pathname);
  return m?.[1] ?? 'all';
}
