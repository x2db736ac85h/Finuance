// The split editor's arithmetic (Phase 19, ADR 0028), the same rules as the server's
// internal/splits: an equal split gives the remainder cent by cent to the first members, shares
// split by weight with the largest remainders, exact amounts must add up. The server checks again;
// this is the preview.

export type SplitMode = 'equal' | 'shares' | 'exact';

export type SplitState = {
  mode: SplitMode;
  /** equal and shares: who takes part (member ids, in the group's order) */
  members: string[];
  /** shares: a weight per member id */
  weights: Record<string, number>;
  /** exact: an amount in minor units per member id */
  exact: Record<string, number>;
};

/** The parts of amount (minor units) per member id, or null when the split can't be made. */
export function previewShares(s: SplitState, amount: number, order: string[]): Record<string, number> | null {
  const sign = amount < 0 ? -1 : 1;
  const abs = Math.abs(amount);
  const members = order.filter((m) => s.members.includes(m));
  const out: Record<string, number> = {};
  if (s.mode === 'exact') {
    let sum = 0;
    for (const m of order) {
      const v = s.exact[m] ?? 0;
      if (v) out[m] = v;
      sum += v;
    }
    return sum === amount && Object.keys(out).length ? out : null;
  }
  if (!members.length) return null;
  if (s.mode === 'equal') {
    const base = Math.floor(abs / members.length);
    let rest = abs - base * members.length;
    for (const m of members) {
      out[m] = sign * (base + (rest > 0 ? 1 : 0));
      if (rest > 0) rest--;
    }
    return out;
  }
  const weights = members.map((m) => Math.max(0, Math.round(s.weights[m] ?? 1)));
  const total = weights.reduce((a, w) => a + w, 0);
  if (!total) return null;
  const parts = members.map((m, i) => {
    const exact = (abs * (weights[i] ?? 0)) / total;
    return { m, floor: Math.floor(exact), frac: exact - Math.floor(exact), i };
  });
  let rest = abs - parts.reduce((a, p) => a + p.floor, 0);
  for (const p of [...parts].sort((a, b) => b.frac - a.frac || a.i - b.i)) {
    if (rest <= 0) break;
    p.floor++;
    rest--;
  }
  for (const p of parts) out[p.m] = sign * p.floor;
  return out;
}

/** What is left to assign in an exact split. */
export function exactLeft(s: SplitState, amount: number): number {
  return amount - Object.values(s.exact).reduce((a, v) => a + v, 0);
}

/** The API's split document. */
export function toApiSplit(s: SplitState, order: string[]) {
  const members = order.filter((m) => s.members.includes(m));
  switch (s.mode) {
    case 'equal':
      return { mode: 'equal' as const, members };
    case 'shares':
      return {
        mode: 'shares' as const,
        members,
        weights: members.map((m) => Math.max(1, Math.round(s.weights[m] ?? 1))),
      };
    case 'exact':
      return {
        mode: 'exact' as const,
        exact: order.filter((m) => s.exact[m]).map((m) => ({ memberId: m, shareMinor: s.exact[m] ?? 0 })),
      };
  }
}
