import { describe, expect, it } from 'vitest';

import { without } from './objects';

describe('without', () => {
  it('drops one key and leaves the input alone', () => {
    const o: Partial<Record<'a' | 'b', number>> = { a: 1, b: 2 };
    expect(without(o, 'a')).toEqual({ b: 2 });
    expect(without(o, 'b')).toEqual({ a: 1 });
    expect(o).toEqual({ a: 1, b: 2 });
  });
});
