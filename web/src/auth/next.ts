/** Where to go after signing in: a same-origin path from ?next=, otherwise the app's start. */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return '/';
  if (next.startsWith('/login') || next.startsWith('/signup')) return '/';
  return next;
}
