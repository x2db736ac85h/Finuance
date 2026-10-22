// Chart colours (legacy §19): categorical slots 1–7 follow the entity (the server's colorSlot),
// everything else is the grey "Other". Never generate extra hues.

export const slotColor = (slot: number | null | undefined): string =>
  slot && slot >= 1 && slot <= 7 ? `var(--s${slot})` : 'var(--c-other)';

export const INCOME = 'var(--inc)';
export const EXPENSE = 'var(--exp)';
export const PRIMARY = 'var(--s1)';
export const PREVIOUS = 'var(--c-prev)';
export const heatColor = (bin: number) => `var(--h${bin})`;
