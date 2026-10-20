// Manual investment portfolios (legacy S.inv) and the dataset each one is rendered with.
export default [
  ['all-zero', 'demo', {ppk: {employee: 0, employer: 0, state: 0, value: 0}, positions: [], swr: 4, ret: 5}],
  ['typical', 'demo', {ppk: {employee: 12000, employer: 9000, state: 690, value: 25400}, positions: [
    {id: 'p1', name: 'ETF World', type: 'ETF', invested: 40000, value: 46500, income: 0},
    {id: 'p2', name: 'Obligacje EDO', type: 'Bonds', invested: 20000, value: 21300, income: 1360},
    {id: 'p3', name: 'Akcje dywidendowe', type: 'Stocks', invested: 8000, value: 9100, income: 640}], swr: 4, ret: 5}],
  ['no-income-fallback', 'demo-dividends', {ppk: {employee: 3000, employer: 2250, state: 250, value: 6100}, positions: [
    {id: 'p1', name: 'Akcje dywidendowe', type: 'Stocks', invested: 15000, value: 16200, income: 0},
    {id: 'p2', name: 'Lokata', type: 'Deposit', invested: 10000, value: 10000, income: ''}], swr: 3.5, ret: 4}],
  ['short-history', 'demo-short', {ppk: {employee: 1000, employer: 750, state: 250, value: 2100}, positions: [
    {id: 'p1', name: 'ETF', type: 'ETF', invested: 5000, value: 5200, income: 50}], swr: 4, ret: 5}],
  ['fi-unreachable', 'demo-expenses-only', {ppk: {employee: 0, employer: 0, state: 0, value: 0}, positions: [
    {id: 'p1', name: 'Konto oszczędnościowe', type: 'Cash', invested: 1000, value: 1000, income: 10}], swr: 4, ret: 0}]
];
