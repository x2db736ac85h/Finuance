// execTool argument sets on the demo dataset (with groups assigned). Every filter, sort, group_by
// and both find_values fields are covered.
const S = 'search_transactions', G = 'aggregate_transactions', F = 'find_values';

export default [
  [S, {}], [S, {limit: 5}], [S, {limit: 0}], [S, {limit: 1000}], [S, {limit: -3}], [S, {limit: '7'}],
  [S, {sort: 'date_asc', limit: 5}], [S, {sort: 'date_desc', limit: 5}], [S, {sort: 'amount_desc', limit: 5}], [S, {sort: 'amount_asc', limit: 5}],
  [S, {sort: 'bogus', limit: 3}], [S, {query: 'kino'}], [S, {query: 'kino warszawa'}], [S, {query: 'PŁATNOŚĆ blik', limit: 3}],
  [S, {query: 'platnosc'}], [S, {payee: 'kawiarnia'}], [S, {payee: 'KAWIARNIA PODWÓRKO'}], [S, {category: 'rachunki'}],
  [S, {category: 'Zakupy przez internet', sort: 'amount_desc', limit: 3}], [S, {group: 'wakacje'}], [S, {group: 'Remont'}],
  [S, {date_from: '2025-03-01'}], [S, {date_to: '2024-01-15'}], [S, {date_from: '2024-06-01', date_to: '2024-06-30'}],
  [S, {date_from: '2024-06-30', date_to: '2024-06-01'}], [S, {date_from: 'garbage'}], [S, {date_from: '2025-03-10T15:00:00'}],
  [S, {type: 'expense', limit: 3}], [S, {type: 'income'}], [S, {type: 'any', limit: 2}], [S, {min_amount: 1000}], [S, {min_amount: -1000}],
  [S, {max_amount: 5}], [S, {min_amount: 100, max_amount: 200, type: 'expense', limit: 3}], [S, {min_amount: '50', limit: 2}],
  [S, {payee: 'nonexistent'}], [S, {query: 'kino', date_from: '2024-03-01', date_to: '2024-05-31', type: 'expense'}],
  [G, {group_by: 'none'}], [G, {}], [G, {group_by: 'category'}], [G, {group_by: 'payee'}], [G, {group_by: 'payee', top: 5}],
  [G, {group_by: 'month'}], [G, {group_by: 'month', top: 3}], [G, {group_by: 'week', date_from: '2025-02-01'}], [G, {group_by: 'day', date_from: '2025-03-01'}],
  [G, {group_by: 'weekday'}], [G, {group_by: 'type'}], [G, {group_by: 'group'}], [G, {group_by: 'category', type: 'expense', date_from: '2024-01-01', date_to: '2024-12-31'}],
  [G, {group_by: 'payee', query: 'warszawa', top: 200}], [G, {group_by: 'month', category: 'wynagrodzenie'}], [G, {group_by: 'bogus'}],
  [G, {group_by: 'category', top: 0}], [G, {group_by: 'day', top: 500}], [G, {group_by: 'weekday', payee: 'kino'}],
  [F, {field: 'payee'}], [F, {field: 'payee', query: 'kawiarnia'}], [F, {field: 'payee', query: 'ŁĄKA'}], [F, {field: 'category'}],
  [F, {field: 'category', query: 'zak'}], [F, {}], [F, {field: 'bogus', query: 'x'}], [F, {field: 'payee', query: 'zzz'}],
  ['unknown_tool', {}], [S, null], [G, 'not an object']
];
