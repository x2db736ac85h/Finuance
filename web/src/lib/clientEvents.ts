// The browser's errors, reported to the server (PLAN 24.8): an uncaught error, a promise nobody
// handled, a screen that failed to draw. What is sent is the kind, the error's class name and the
// screen's route pattern. Never a message, a stack or the address bar: those can quote what the
// person typed or looked at.

export type ClientEventKind = 'error' | 'unhandled_rejection' | 'render';

/** The first path segments the app defines; anything else in a URL is somebody's typing. */
const SCREENS = new Set(['login', 'signup', 'transactions', 'overview', 'imports', 'shared', 'settings', 'admin']);

/**
 * The screen's route pattern for a path: the app's own first segment, every later segment as
 * `:id`. `/imports/01a1…` becomes `/imports/:id`; an unknown first segment becomes `/*`.
 */
export function screenOf(pathname: string): string {
  const [first, ...rest] = pathname.split('/').filter(Boolean);
  if (first === undefined) return '/';
  if (!SCREENS.has(first)) return '/*';
  return `/${first}${rest.map(() => '/:id').join('')}`;
}

/** An error's class name when it looks like one (`TypeError`); nothing for anything else. */
export function errorName(e: unknown): string | undefined {
  const name = e instanceof Error ? e.name : undefined;
  return name !== undefined && /^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(name) ? name : undefined;
}

/** A page that fails in a loop reports its first few errors only. */
const MAX_PER_PAGE = 5;
let sent = 0;

/** For tests. */
export function resetClientEvents(): void {
  sent = 0;
}

/** Reports one event; never throws and never waits. */
export function reportClientEvent(kind: ClientEventKind, error: unknown): void {
  if (sent >= MAX_PER_PAGE) return;
  sent++;
  const name = errorName(error);
  const body = JSON.stringify({
    kind,
    route: screenOf(window.location.pathname),
    ...(name === undefined ? {} : { name }),
  });
  try {
    void globalThis
      .fetch(`${window.location.origin}/api/v1/client-events`, {
        method: 'POST',
        credentials: 'same-origin',
        keepalive: true, // still sent when the page is going away
        headers: { 'Content-Type': 'application/json', 'X-Finuance-Client': 'web' },
        body,
      })
      .catch(() => undefined); // reporting an error must not become another one
  } catch {
    // no fetch, or it refused the request: nothing to do
  }
}

/** Installs the two window handlers. Called once, before the app renders. */
export function installClientEventHandlers(): void {
  window.addEventListener('error', (e) => {
    // a failed <img> or <script> load also fires "error", without an error object: not ours
    if (e.error !== undefined && e.error !== null) reportClientEvent('error', e.error);
  });
  window.addEventListener('unhandledrejection', (e) => {
    reportClientEvent('unhandled_rejection', e.reason);
  });
}
