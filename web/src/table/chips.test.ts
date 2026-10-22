import { describe, expect, it } from 'vitest';

import { newView } from '../stores/viewStore';
import { chipsOf, cleared, extraHidden, withoutChip } from './chips';

describe('chips', () => {
  it('derives one chip per narrowing, in legacy order', () => {
    const v = {
      ...newView('all'),
      period: 'l3m',
      search: ' kino ',
      sort: { column: 'amount', dir: 'desc' as const },
      filters: { payee: { cond: '!uber', excluded: ['A', 'B'] }, category: { cond: '', excluded: ['x'] } },
      hidden: [...newView('all').hidden, 'payee'],
    };
    expect(chipsOf('all', v, 'Last 3 months')).toEqual([
      { key: 'period', icon: 'cal', value: 'Last 3 months' },
      { key: 'search', icon: 'search', value: 'kino' },
      { key: 'sort', icon: 'down', value: 'Amount' },
      { key: 'f:payee', label: 'Payee', value: '!uber' },
      { key: 'x:payee', label: 'Payee', value: '−2 values' },
      { key: 'x:category', label: 'Category', value: '−1 value' },
      { key: 'hidden', icon: 'eyeoff', value: '1 hidden' },
    ]);
  });

  it('makes no chips for the defaults', () => {
    expect(chipsOf('all', newView('all'), undefined)).toEqual([]);
    expect(chipsOf('acc', newView('acc'), 'All time')).toEqual([]);
    expect(extraHidden('acc', newView('acc').hidden)).toEqual([]);
  });

  it('removes one chip at a time', () => {
    const v = { ...newView('all'), filters: { payee: { cond: 'x', excluded: ['A'] } } };
    expect(withoutChip('all', v, 'f:payee')).toEqual({ filters: { payee: { cond: '', excluded: ['A'] } } });
    expect(withoutChip('all', v, 'x:payee')).toEqual({ filters: { payee: { cond: 'x', excluded: [] } } });
    const onlyCond = { ...v, filters: { payee: { cond: 'x', excluded: [] } } };
    expect(withoutChip('all', onlyCond, 'f:payee')).toEqual({ filters: {} });
    expect(withoutChip('acc', v, 'hidden')).toEqual({ hidden: newView('acc').hidden });
  });

  it('clear all resets to the view defaults', () => {
    expect(cleared('acc')).toMatchObject({
      search: '',
      filters: {},
      sort: null,
      period: 'all',
      hidden: newView('acc').hidden,
    });
  });
});
