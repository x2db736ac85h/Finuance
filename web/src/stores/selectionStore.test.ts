import { beforeEach, describe, expect, it } from 'vitest';

import { selectedIn, useSelection } from './selectionStore';

beforeEach(() => {
  useSelection.getState().clear();
  useSelection.setState({ viewId: null });
});

describe('selectionStore', () => {
  it('belongs to one view at a time', () => {
    const s = useSelection.getState();
    s.setAnchor('all', 3);
    s.set('all', new Map([['a', { amountMinor: -100, grouped: false }]]));
    expect(useSelection.getState().anchor).toBe(3);
    expect(selectedIn(useSelection.getState(), 'all').size).toBe(1);
    expect(selectedIn(useSelection.getState(), 'acc').size).toBe(0);
    // another view takes over and drops the anchor
    s.set('acc', new Map([['b', { amountMinor: 5, grouped: true }]]));
    expect(useSelection.getState().anchor).toBeNull();
    expect(selectedIn(useSelection.getState(), 'all').size).toBe(0);
  });

  it('clears only the view it is asked to', () => {
    const s = useSelection.getState();
    s.set('all', new Map([['a', { amountMinor: 1, grouped: false }]]));
    s.clear('acc');
    expect(useSelection.getState().rows.size).toBe(1);
    s.clear('all');
    expect(useSelection.getState().rows.size).toBe(0);
    expect(useSelection.getState().anchor).toBeNull();
  });
});
