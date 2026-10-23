// Overview cases on the legacy demo dataset (PLAN Phase 12): each case sets the list's filters and
// search, the overview month, the cfg.ovMonths range, budgets (by category name, major units) and
// the opening balance, then records the overview context, every card's table twin and subtitle at
// both sizes, and the highlight sentences. The demo data ends on 17.03.2025 (March is partial).
const C = 'Kategoria', P = 'Nadawca / Odbiorca', A = 'Kwota operacji';

export default [
  {name: 'latest-partial', month: '2025-03', ovMonths: 12},
  {name: 'full-month', month: '2025-02', ovMonths: 12, budgets: {'Transport': 500, 'Artykuły spożywcze': 900, 'Zdrowie i uroda': 0}},
  {name: 'first-month', month: '2024-01', ovMonths: 12},
  {name: 'second-month', month: '2024-02', ovMonths: 6},
  {name: 'mid-year-6', month: '2024-06', ovMonths: 6, opening: 12500.5},
  {name: 'year-end-24', month: '2024-12', ovMonths: 24},
  {name: 'default-month', month: null, ovMonths: 36},
  {name: 'filtered', month: '2025-02', ovMonths: 12, filters: {[C]: '!Mieszkanie'}},
  {name: 'search', month: '2024-12', ovMonths: 12, search: 'warszawa'},
  {name: 'expenses-only', month: '2024-11', ovMonths: 12, filters: {[A]: '<0'}},
  {name: 'payee-filter', month: '2024-09', ovMonths: 12, filters: {[P]: 'market'}}
];
