import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type {
  InsightCashflow,
  InsightCategories,
  InsightDaily,
  InsightPace,
  InsightPayees,
  InsightSummary,
} from '../api/hooks/insights';
import { fmtDayLong, fmtMonthLong, fmtMonthShort, fmtMonthYear } from '../lib/dates';
import { fmtMoney } from '../lib/format';
import { DEFAULT_CFG } from '../stores/prefs';
import { cashflowParts, visibleMonths } from './CashflowChart';
import { activeCategory, categoriesParts, UNCATEGORIZED } from './CategoriesChart';
import { dailyParts, heatLayout } from './DailyHeatmap';
import { compareLabel, paceParts } from './PaceChart';
import { payeesParts } from './PayeesChart';
import { deltaText, summaryParts } from './SummaryChart';
import type { ChartEnv, ChartParts } from './types';

const money = (v: number) => fmtMoney({ amountMinor: v, currency: 'PLN' }, { currency: true });
const whole = (v: number) => fmtMoney({ amountMinor: v, currency: 'PLN' }, { whole: true });

function env(over: Partial<ChartEnv> = {}): ChartEnv {
  return {
    w: 400,
    h: 200,
    big: false,
    cfg: DEFAULT_CFG,
    catMode: 'donut',
    period: 'all',
    filters: {},
    actions: { toggleFilter: vi.fn(), setPeriod: vi.fn(), locate: vi.fn(), setCatMode: vi.fn() },
    ...over,
  };
}

/** A copy of o without key k (optional API fields are absent, never undefined). */
function omit<T extends object, K extends keyof T>(o: T, k: K): Omit<T, K> {
  return Object.fromEntries(Object.entries(o).filter(([key]) => key !== k)) as Omit<T, K>;
}

const draw = (p: ChartParts) => render(<div>{p.body}</div>).container;

describe('summary', () => {
  const s: InsightSummary = {
    period: { kind: 'month', from: '2025-02-01', to: '2025-03-01', label: 'Feb 2025' },
    currency: 'PLN',
    count: 50,
    spentMinor: 556962,
    incomeMinor: 958000,
    netMinor: 401038,
    savingsRate: 0.4186,
    days: 28,
    avgPerDayMinor: 19892,
    largestExpense: { id: 'tx-1', bookedOn: '2025-02-02', amountMinor: -285000, payee: 'WSPÓLNOTA MIESZKANIOWA' },
    prevSpentMinor: 667515,
    spentDelta: -0.1656,
  };

  it('shows the tiles, captions and the table twin', () => {
    const e = env();
    const p = summaryParts(s, e);
    const el = draw(p);
    expect(p.sub).toBe('Feb 2025');
    expect([...el.querySelectorAll('.kpi .l')].map((x) => x.textContent)).toEqual([
      'Spent · PLN',
      'Income · PLN',
      'Net · PLN',
      'Avg / day · PLN',
      'Largest expense · PLN',
    ]);
    expect([...el.querySelectorAll('.kpi .d')].map((x) => x.textContent)).toEqual([
      '▼ 17% vs previous',
      '50 operations',
      'saved 42% of income',
      'over 28 days',
      'WSPÓLNOTA MIESZKANIOWA · 02.02.2025',
    ]);
    expect(el.querySelector('.kpi .dn')).toBeTruthy();
    expect(p.table?.rows).toEqual([
      ['Spent', money(556962)],
      ['Income', money(958000)],
      ['Net', money(401038)],
      ['Avg / day', money(19892)],
      ['Days', '28'],
      ['Operations', '50'],
      ['Largest expense', money(-285000)],
    ]);
    // the largest expense locates its row in the list
    fireEvent.click(screen.getByRole('button', { name: fmtMoney({ amountMinor: -285000, currency: 'PLN' }) }));
    expect(e.actions.locate).toHaveBeenCalledWith('tx-1');
  });

  it('words the delta like the legacy', () => {
    expect(deltaText(omit(s, 'period')).text).toBe('all time');
    expect(deltaText(omit(s, 'spentDelta')).text).toBe('no spending in previous period');
    expect(deltaText({ ...s, spentDelta: 0.004 }).text).toBe('same as previous');
    expect(deltaText({ ...s, spentDelta: 0.256 })).toEqual({ text: '▲ 26%', dir: 'up' });
  });
});

const cats: InsightCategories = {
  currency: 'PLN',
  totalMinor: 100000,
  categories: [
    { category: { id: 'c1', name: 'Mieszkanie' }, spentMinor: 50000, share: 0.5, colorSlot: 1 },
    { spentMinor: 20000, share: 0.2, colorSlot: 2 },
    { category: { id: 'c3', name: 'Kino' }, spentMinor: 20000, share: 0.2 },
    { category: { id: 'c4', name: 'Książki' }, spentMinor: 10000, share: 0.1 },
  ],
};

describe('categories', () => {
  it('draws a donut of the coloured categories plus Other, with a full legend', () => {
    const e = env();
    const p = categoriesParts(cats, e);
    const el = draw(p);
    expect(p.sub).toBe('4 categories');
    expect(el.querySelectorAll('.donut path')).toHaveLength(3);
    const rows = [...el.querySelectorAll('.lg')].map((r) => [...r.querySelectorAll('span')].map((x) => x.textContent));
    expect(rows).toEqual([
      ['Mieszkanie', whole(50000), '50%'],
      [UNCATEGORIZED, whole(20000), '20%'],
      ['Other (2)', whole(30000), '30%'],
    ]);
    expect(el.querySelector('svg text')?.textContent).toBe(
      new Intl.NumberFormat(undefined, { notation: 'compact' }).format(1000),
    );
    expect(p.table?.rows).toEqual([
      ['Mieszkanie', money(50000), '50.0%'],
      [UNCATEGORIZED, money(20000), '20.0%'],
      ['Kino', money(20000), '20.0%'],
      ['Książki', money(10000), '10.0%'],
    ]);
    // a click filters on the category; the uncategorised entry filters on an empty category; Other does nothing
    const legend = [...el.querySelectorAll('.lg')];
    legend.forEach((r) => fireEvent.click(r));
    expect(e.actions.toggleFilter).toHaveBeenNthCalledWith(1, 'category', '=Mieszkanie');
    expect(e.actions.toggleFilter).toHaveBeenNthCalledWith(2, 'category', '=');
    expect(e.actions.toggleFilter).toHaveBeenCalledTimes(2);
  });

  it('dims the other slices and highlights the filtered category', () => {
    const e = env({ filters: { category: { cond: '=Mieszkanie', excluded: [] } } });
    const el = draw(categoriesParts(cats, e));
    expect(el.querySelectorAll('.donut path.dim')).toHaveLength(2);
    expect(el.querySelector('.lg.on .nm')?.textContent).toBe('Mieszkanie');
    expect(activeCategory({ filters: { category: { cond: '=', excluded: [] } } })).toBe(UNCATEGORIZED);
    expect(activeCategory({ filters: { category: { cond: 'kino', excluded: [] } } })).toBeNull();
  });

  it('draws a ring for a single category and bars for all of them', () => {
    const one: InsightCategories = {
      currency: 'PLN',
      totalMinor: 500,
      categories: [{ category: { id: 'c', name: 'Kawa' }, spentMinor: 500, share: 1, colorSlot: 3 }],
    };
    const ring = draw(categoriesParts(one, env()));
    expect(ring.querySelectorAll('.donut circle')).toHaveLength(1);
    expect(ring.querySelectorAll('.donut path')).toHaveLength(0);

    const e = env({ catMode: 'bars' });
    const bars = draw(categoriesParts(cats, e));
    expect(bars.querySelectorAll('.hb')).toHaveLength(4);
    expect([...bars.querySelectorAll('.hb-f')].map((b) => (b as HTMLElement).style.width)).toEqual([
      '100%',
      '40%',
      '40%',
      '20%',
    ]);
    fireEvent.click(within(bars).getByText('Kino'));
    expect(e.actions.toggleFilter).toHaveBeenCalledWith('category', '=Kino');
  });

  it('switches donut and bars from the legend controls', () => {
    const e = env();
    const p = categoriesParts(cats, e);
    render(<div>{p.legend}</div>);
    fireEvent.click(screen.getByRole('button', { name: 'Bars' }));
    expect(e.actions.setCatMode).toHaveBeenCalledWith('bars');
  });

  it('says so when nothing was spent', () => {
    const p = categoriesParts({ currency: 'PLN', totalMinor: 0, categories: [] }, env());
    expect(draw(p).textContent).toBe('No spending in this selection');
    expect(p.table).toBeNull();
  });
});

const flow: InsightCashflow = {
  currency: 'PLN',
  months: [
    { month: '2024-11', incomeMinor: 900000, expenseMinor: 500000, count: 60, inPeriod: false },
    { month: '2024-12', incomeMinor: 0, expenseMinor: 0, count: 0, inPeriod: false },
    { month: '2025-01', incomeMinor: 950000, expenseMinor: 600000, count: 70, inPeriod: true },
    { month: '2025-02', incomeMinor: 960000, expenseMinor: 550000, count: 50, inPeriod: false },
  ],
};

describe('cash flow', () => {
  it('draws income up and expenses down, dims months outside the period, and keeps a table twin', () => {
    const e = env({ period: 'm:2025-01', cfg: { ...DEFAULT_CFG, flowMonths: 'all' } });
    const p = cashflowParts(flow, e);
    const el = draw(p);
    expect(p.sub).toBe('4 months');
    const bars = [...el.querySelectorAll('svg path')].filter((x) => x.getAttribute('d'));
    expect(bars).toHaveLength(6); // the empty month draws nothing
    expect(bars.filter((b) => (b as SVGElement).style.opacity === '1')).toHaveLength(2);
    expect([...el.querySelectorAll('text.ax')].map((t) => t.textContent)).toContain(`${fmtMonthShort('2024-11')} '24`);
    expect(p.table?.rows[0]).toEqual([fmtMonthLong('2025-02'), money(960000), money(-550000), money(410000), '50']);
    expect(p.table?.rows).toHaveLength(4);
    // clicking the period's month shows all time again; another month focuses it
    fireEvent.click(el.querySelector('rect[data-month="2025-01"]') as Element);
    expect(e.actions.setPeriod).toHaveBeenLastCalledWith('all');
    fireEvent.click(el.querySelector('rect[data-month="2025-02"]') as Element);
    expect(e.actions.setPeriod).toHaveBeenLastCalledWith('m:2025-02');
  });

  it('cuts the months to the range setting and the width', () => {
    const many = Array.from({ length: 40 }, (_, i) => ({
      month: String(i),
      incomeMinor: 0,
      expenseMinor: 0,
      count: 0,
      inPeriod: true,
    }));
    expect(visibleMonths(many, 12, 400, false)).toHaveLength(12);
    expect(visibleMonths(many, 'all', 400, false)).toHaveLength(40);
    expect(visibleMonths(many, 'auto', 400, false)).toHaveLength(22); // (400 − 46) / 16
    expect(visibleMonths(many, 'auto', 400, true)).toHaveLength(39); // (400 − 46) / 9
    expect(visibleMonths(many, 'all', 150, false)).toHaveLength(20); // never more than one per 5 units
    expect(visibleMonths(many, 6, 400, false).at(-1)?.month).toBe('39');
  });
});

const pace: InsightPace = {
  currency: 'PLN',
  target: '2025-03',
  daysInMonth: 31,
  lastDay: 17,
  current: Array.from({ length: 31 }, (_, i) => (i + 1) * 1000),
  compare: {
    mode: 'prev',
    month: '2025-02',
    days: 28,
    values: Array.from({ length: 31 }, (_, i) => Math.min(i + 1, 28) * 900),
  },
};

describe('month pace', () => {
  it('draws both lines with the end marker and a table up to the last data day', () => {
    const p = paceParts(pace, env());
    const el = draw(p);
    expect(p.sub).toBe('cumulative spend');
    const lines = [...el.querySelectorAll('svg path')];
    expect(lines).toHaveLength(2);
    expect(lines[1]?.getAttribute('d')?.match(/[ML]/g)).toHaveLength(17);
    expect(lines[0]?.getAttribute('d')?.match(/[ML]/g)).toHaveLength(28);
    expect(p.table?.cols).toEqual(['Day', fmtMonthYear('2025-03'), fmtMonthShort('2025-02')]);
    expect(p.table?.rows).toHaveLength(31);
    expect(p.table?.rows[16]).toEqual(['17', money(-17000), money(-15300)]);
    expect(p.table?.rows[17]).toEqual(['18', '', money(-16200)]);
    expect(p.table?.rows[30]).toEqual(['31', '', '']);
  });

  it('shows a crosshair with the day under the pointer', () => {
    const el = draw(paceParts(pace, env()));
    const ov = el.querySelector('rect.ov') as Element;
    fireEvent.pointerMove(ov, { clientX: 38 + 150 });
    expect(el.querySelector('.xh line')).toBeTruthy();
    expect(el.querySelectorAll('.xh circle')).toHaveLength(2);
    fireEvent.pointerLeave(ov);
    expect(el.querySelector('.xh')).toBeNull();
  });

  it('labels the comparisons', () => {
    expect(compareLabel(pace)).toBe(fmtMonthShort('2025-02'));
    expect(compareLabel({ ...pace, compare: { mode: 'yoy', month: '2024-03', days: 31, values: [] } })).toBe(
      fmtMonthYear('2024-03'),
    );
    expect(compareLabel({ ...pace, compare: { mode: 'avg3', months: 2, days: 31, values: [] } })).toBe('2-mo avg');
    const none = paceParts(omit(pace, 'compare'), env());
    expect(none.table?.cols).toHaveLength(2);
    expect(draw(paceParts({ currency: 'PLN', current: [] }, env())).textContent).toBe('No dated amounts');
  });
});

describe('top payees', () => {
  const d: InsightPayees = {
    currency: 'PLN',
    total: 23,
    payees: [
      { payee: 'WSPÓLNOTA MIESZKANIOWA', spentMinor: 285000, count: 1 },
      { payee: 'KAWIARNIA PODWÓRKO', spentMinor: 156720, count: 32 },
    ],
  };

  it('draws bars, averages and filters on click', () => {
    const e = env({ filters: { payee: { cond: 'KAWIARNIA PODWÓRKO', excluded: [] } } });
    const p = payeesParts(d, e);
    const el = draw(p);
    expect(p.sub).toBe('top 2 of 23');
    expect(el.querySelectorAll('.hb')).toHaveLength(2);
    expect(el.querySelector('.hb.on')?.textContent).toContain('KAWIARNIA PODWÓRKO');
    expect(p.table?.rows[1]).toEqual(['KAWIARNIA PODWÓRKO', money(156720), '32', money(4898)]); // 48.975 rounds up
    fireEvent.click(within(el).getByText('WSPÓLNOTA MIESZKANIOWA'));
    expect(e.actions.toggleFilter).toHaveBeenCalledWith('payee', 'WSPÓLNOTA MIESZKANIOWA');
    expect(payeesParts(d, env({ cfg: { ...DEFAULT_CFG, payeeScope: 'all' } })).sub).toBe('top 2 of 23 · all time');
  });
});

describe('daily heatmap', () => {
  const d: InsightDaily = {
    currency: 'PLN',
    from: '2025-02-01',
    to: '2025-03-01',
    days: [
      { date: '2025-02-03', spentMinor: 1000, count: 1 },
      { date: '2025-02-10', spentMinor: 5000, count: 2 },
      { date: '2025-02-28', spentMinor: 20000, count: 3 },
    ],
  };

  it('lays out Monday-first weeks and fits the cells', () => {
    // 1 Feb 2025 is a Saturday: the first week starts on Monday 27 Jan
    expect(heatLayout('2025-02-01', '2025-03-01', 400, 200, false)).toMatchObject({
      start: '2025-01-27',
      weeks: 5,
      cs: 16,
    });
    // too narrow for every week: the most recent weeks stay
    const narrow = heatLayout('2024-01-01', '2025-03-01', 200, 200, false);
    expect(narrow.weeks).toBe(Math.floor((200 - 36) / 9));
    expect(narrow.start > '2024-01-01').toBe(true);
  });

  it('draws one cell per day of the range, binned, with a newest-first table', () => {
    const e = env();
    const p = dailyParts(d, e);
    const el = draw(p);
    expect(p.sub).toBe('5 weeks');
    const cells = el.querySelectorAll('rect[data-day]');
    expect(cells).toHaveLength(28);
    expect((el.querySelector('rect[data-day="2025-02-28"]') as SVGElement).style.fill).toBe('var(--h5)');
    expect((el.querySelector('rect[data-day="2025-02-04"]') as SVGElement).style.fill).toBe('var(--h0)');
    expect(p.table?.rows).toEqual([
      [fmtDayLong('2025-02-28'), money(20000), '3'],
      [fmtDayLong('2025-02-10'), money(5000), '2'],
      [fmtDayLong('2025-02-03'), money(1000), '1'],
    ]);
    fireEvent.click(el.querySelector('rect[data-day="2025-02-10"]') as Element);
    expect(e.actions.setPeriod).toHaveBeenCalledWith('custom', { from: '2025-02-10', to: '2025-02-10' });
    expect(dailyParts(d, env({ cfg: { ...DEFAULT_CFG, heatRange: 3 } })).sub).toBe('5 weeks · last 3 mo');
    expect(dailyParts(d, env({ cfg: { ...DEFAULT_CFG, heatRange: 'all' } })).sub).toBe('5 weeks · all data');
    expect(draw(dailyParts({ currency: 'PLN', days: [] }, e)).textContent).toBe('No dated operations');
  });
});
