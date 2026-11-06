import { describe, expect, it } from 'vitest';

import { fitWithin, MAX_SIDE } from './photos';

describe('photos', () => {
  it('scales the longer side down to 1,600 px and never up', () => {
    expect(fitWithin(4032, 3024)).toEqual({ w: MAX_SIDE, h: 1200 });
    expect(fitWithin(3000, 4000)).toEqual({ w: 1200, h: MAX_SIDE });
    expect(fitWithin(800, 600)).toEqual({ w: 800, h: 600 });
    expect(fitWithin(5000, 2)).toEqual({ w: MAX_SIDE, h: 1 });
  });
});
