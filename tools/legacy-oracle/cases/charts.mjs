// Insight chart cases on the legacy demo dataset (PLAN Phase 11): each case sets the table view
// (period, column filters, search) and the chart range settings (S.cfg), then records the table twin
// and subtitle of every pane chart. Filters use the demo CSV's headers (column-map.json).
const C = 'Kategoria', P = 'Nadawca / Odbiorca', A = 'Kwota operacji';

export default [
  {name: 'all-time', period: 'all', cfg: {}},
  {name: 'month-latest', period: 'm:2025-03', cfg: {paceCmp: 'prev', heatRange: 'period'}},
  {name: 'month-yoy', period: 'm:2025-02', cfg: {paceCmp: 'yoy', heatRange: 3, payeeTop: 10}},
  {name: 'month-avg3', period: 'm:2024-06', cfg: {paceCmp: 'avg3', heatRange: 12, payeeTop: 10, payeeScope: 'all'}},
  {name: 'first-month', period: 'm:2024-01', cfg: {paceCmp: 'prev', heatRange: 6}},
  {name: 'l30-avg6', period: 'l30', cfg: {paceCmp: 'avg6', heatRange: 'all', payeeTop: 0}},
  {name: 'l3m-search', period: 'l3m', search: 'warszawa', cfg: {paceCmp: 'avg3', payeeTop: 50}},
  {name: 'l12m-none', period: 'l12m', cfg: {paceCmp: 'none', payeeTop: 100}},
  {name: 'ytd', period: 'ytd', cfg: {paceCmp: 'yoy'}},
  {name: 'year', period: 'y:2024', cfg: {paceCmp: 'avg6', payeeScope: 'all', payeeTop: 0}},
  {name: 'custom-range', period: 'custom', custom: ['2024-07-01T00:00:00Z', '2024-07-15T00:00:00Z'], cfg: {paceCmp: 'prev'}},
  {name: 'one-day', period: 'custom', custom: ['2025-03-10T00:00:00Z', '2025-03-11T00:00:00Z'], cfg: {heatRange: 'period'}},
  {name: 'before-the-data', period: 'm:2023-06', cfg: {paceCmp: 'prev', heatRange: 'period'}},
  {name: 'category-filter', period: 'l12m', filters: {[C]: '!Rachunki'}, cfg: {paceCmp: 'avg3', heatRange: 6}},
  {name: 'payee-and-amount', period: 'all', filters: {[P]: 'warszawa', [A]: '<-50'}, cfg: {paceCmp: 'yoy', payeeTop: 25, heatRange: 12}},
  {name: 'income-only', period: 'y:2024', filters: {[A]: '>0'}, cfg: {}}
];
