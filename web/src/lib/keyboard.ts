// Helpers for the global keyboard manager (PLAN §7.11).

/** Whether keys typed now go into a form control (plain-key shortcuts must not fire). */
export function isTyping(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  return tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA' || (el as HTMLElement).isContentEditable;
}

/** ⌘ on macOS, Ctrl elsewhere (both are accepted, as in the legacy app). */
export function isMod(e: KeyboardEvent): boolean {
  return e.metaKey || e.ctrlKey;
}
