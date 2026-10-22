import { beforeEach, describe, expect, it } from 'vitest';

import { useViews, viewOf } from '../stores/viewStore';
import { activeGroup, renameGroupFilters, toggleGroupFilter } from './filters';
import { nextColor } from './colors';

beforeEach(() => {
  useViews.getState().reset();
});

describe('group filters', () => {
  it('toggles the =Name filter and shows the Group column', () => {
    useViews.getState().patch('all', { hidden: ['group', 'note'] });
    toggleGroupFilter('all', 'Italy');
    expect(activeGroup('all')).toBe('Italy');
    expect(viewOf(useViews.getState().views, 'all').hidden).toEqual(['note']);
    toggleGroupFilter('all', 'Italy');
    expect(viewOf(useViews.getState().views, 'all').filters.group).toBeUndefined();
  });

  it('renames the filter in every view and clears it on delete', () => {
    toggleGroupFilter('all', 'Italy');
    toggleGroupFilter('acc', 'Italy');
    useViews.getState().patch('other', { filters: { group: { cond: 'ital', excluded: [] } } });
    renameGroupFilters('Italy', 'Włochy');
    expect(activeGroup('all')).toBe('Włochy');
    expect(activeGroup('acc')).toBe('Włochy');
    expect(viewOf(useViews.getState().views, 'other').filters.group?.cond).toBe('ital');
    useViews.getState().patch('acc', { filters: { group: { cond: '=Włochy', excluded: ['x'] } } });
    renameGroupFilters('Włochy', null);
    expect(viewOf(useViews.getState().views, 'all').filters.group).toBeUndefined();
    expect(viewOf(useViews.getState().views, 'acc').filters.group).toEqual({ cond: '', excluded: ['x'] });
  });

  it('picks the next colour like the legacy', () => {
    expect(nextColor([])).toBe(0);
    expect(nextColor([0, 1, 3])).toBe(2);
    expect(nextColor([0, 1, 2, 3, 4, 5, 6, 7, 0])).toBe(1);
  });
});
