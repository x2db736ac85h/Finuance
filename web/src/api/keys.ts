// TanStack Query keys (PLAN §7.4). Mutations invalidate by these prefixes.
export const keys = {
  meta: ['meta'] as const,
  me: ['me'] as const,
  prefs: ['prefs'] as const,
  accounts: ['accounts'] as const,
  sessions: ['sessions'] as const,
  imports: (accountId?: string) => (accountId ? (['imports', accountId] as const) : (['imports'] as const)),
  categories: ['categories'] as const,
  tx: ['tx'] as const,
  insight: ['insight'] as const,
  overview: ['overview'] as const,
};
