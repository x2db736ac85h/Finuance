import { describe, expect, it } from 'vitest';

import { cardOrder, parsePrefs } from './prefs';

describe('parsePrefs', () => {
  it('keeps valid values', () => {
    const p = parsePrefs({
      v: 1,
      theme: 'light',
      ui: 1.1,
      vizH: '320px',
      noViz: true,
      mode: 'ov',
      cards: ['sum', 'heat'],
      cardOrder: ['heat', 'sum'],
      catMode: 'bars',
      cfg: { flowMonths: 12, paceCmp: 'yoy', payeeTop: 10, payeeScope: 'all', heatRange: 'all', ovMonths: 24 },
      sbOpen: true,
      ai: { open: true, width: 480, model: 'm', temp: 0.5 },
      importFastPath: false,
      views: {
        all: {
          hidden: ['note'],
          widths: { payee: 240 },
          statColumn: 'balanceAfter',
          sort: { column: 'amount', dir: 'desc' },
          period: 'm:2025-03',
          custom: null,
        },
      },
    });
    expect(p).toEqual({
      theme: 'light',
      ui: 1.1,
      vizH: '320px',
      noViz: true,
      mode: 'ov',
      cards: ['sum', 'heat'],
      cardOrder: ['heat', 'sum'],
      catMode: 'bars',
      cfg: { flowMonths: 12, paceCmp: 'yoy', payeeTop: 10, payeeScope: 'all', heatRange: 'all', ovMonths: 24 },
      sbOpen: true,
      ai: { open: true, width: 480, model: 'm', temperature: 0.5 },
      importFastPath: false,
      views: {
        all: {
          hidden: ['note'],
          widths: { payee: 240 },
          statColumn: 'balanceAfter',
          sort: { column: 'amount', dir: 'desc' },
          period: 'm:2025-03',
          custom: null,
        },
      },
    });
  });

  it('drops wrong types and unknown values', () => {
    const p = parsePrefs({
      theme: 'blue',
      ui: 0.42,
      vizH: 'calc(1px)',
      noViz: 'yes',
      mode: 'grid',
      cards: ['sum', 'nope'],
      catMode: 3,
      cfg: { paceCmp: 'later', payeeTop: '10' },
      sbOpen: 1,
      views: {
        a: { hidden: [1], widths: { x: -5, y: 'wide', z: 90 }, sort: { column: 'amount', dir: 'up' }, period: 'm:25' },
      },
      search: 'never persisted',
      filters: { payee: { cond: 'x' } },
    });
    expect(p).toEqual({ cards: ['sum'], cfg: {}, views: { a: { widths: { z: 90 } } } });
  });

  it('accepts the legacy string form of a manual size', () => {
    expect(parsePrefs({ ui: '1.2' }).ui).toBe(1.2);
    expect(parsePrefs({ ui: 'auto' }).ui).toBe('auto');
  });

  it('keeps the size slider step only when it is one of the five', () => {
    expect(parsePrefs({ uiStep: -2 }).uiStep).toBe(-2);
    expect(parsePrefs({ uiStep: 0 }).uiStep).toBe(0);
    expect(parsePrefs({ uiStep: 3 }).uiStep).toBeUndefined();
    expect(parsePrefs({ uiStep: 0.5 }).uiStep).toBeUndefined();
    expect(parsePrefs({ uiStep: '1' }).uiStep).toBeUndefined();
  });

  it('keeps a colour theme id and drops one that is not an id', () => {
    expect(parsePrefs({ themeId: 'fjord' }).themeId).toBe('fjord');
    expect(parsePrefs({ themeId: '' }).themeId).toBe('');
    expect(parsePrefs({ themeId: 'Bad Theme' }).themeId).toBeUndefined();
    expect(parsePrefs({ themeId: 7 }).themeId).toBeUndefined();
  });

  it('returns nothing for a non-object', () => {
    expect(parsePrefs(null)).toEqual({});
    expect(parsePrefs([1])).toEqual({});
    expect(parsePrefs('x')).toEqual({});
  });
});

describe('cardOrder', () => {
  it('drops unknown and duplicate keys and appends new cards (legacy cardOrder)', () => {
    expect(cardOrder(['heat', 'gone', 'sum', 'heat'])).toEqual(['heat', 'sum', 'cat', 'flow', 'pace', 'payee']);
    expect(cardOrder(null)).toEqual(['sum', 'cat', 'flow', 'pace', 'payee', 'heat']);
  });
});
