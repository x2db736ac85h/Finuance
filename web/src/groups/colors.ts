// Group colours (legacy GCOLS): the seven categorical slots plus --g8, by the group's colour index.

import type { CSSProperties } from 'react';

export const GROUP_COLORS = [
  'var(--s1)',
  'var(--s2)',
  'var(--s3)',
  'var(--s4)',
  'var(--s5)',
  'var(--s6)',
  'var(--s7)',
  'var(--g8)',
] as const;

export const groupColor = (color: number): string => GROUP_COLORS[color % GROUP_COLORS.length] ?? 'var(--s1)';

/** `--c` for the legacy `.gdot` / `.gsw` styles. */
export const colorVar = (color: number) => ({ '--c': groupColor(color) }) as CSSProperties;

/** The first unused colour, or the number of groups modulo the palette (legacy nextColor). */
export function nextColor(used: readonly number[]): number {
  for (let k = 0; k < GROUP_COLORS.length; k++) if (!used.includes(k)) return k;
  return used.length % GROUP_COLORS.length;
}
