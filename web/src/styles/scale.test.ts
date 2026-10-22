import { describe, expect, it } from 'vitest';

import { applyScale, browserZoom, calcScale, uiScale } from './scale';

describe('browserZoom', () => {
  it.each([
    [1, 1],
    [2, 1], // retina at 100 %
    [2.5, 1.25], // retina at 125 %
    [1.25, 1.25],
    [0.9, 0.9],
    [3, 1.5],
    [0, 1], // unknown
  ])('dpr %f → zoom %f', (dpr, zoom) => {
    expect(browserZoom(dpr)).toBeCloseTo(zoom, 5);
  });
});

describe('calcScale', () => {
  it('keeps manual sizes', () => {
    expect(calcScale(0.9, 2)).toBe(0.9);
    expect(calcScale(1.3, 1)).toBe(1.3);
  });
  it('auto looks the same physical size at 100 % and 125 % zoom', () => {
    expect(calcScale('auto', 2)).toBe(1.12);
    expect(calcScale('auto', 2.5)).toBe(0.9); // 1.12 / 1.25 = 0.896
  });
  it('auto is clamped', () => {
    expect(calcScale('auto', 0.5)).toBe(1.25);
    expect(calcScale('auto', 3)).toBe(0.85);
  });
});

describe('applyScale', () => {
  it('sets --ui-scale and remembers the value', () => {
    const el = document.createElement('div');
    expect(applyScale(1.2, el)).toBe(1.2);
    expect(el.style.getPropertyValue('--ui-scale')).toBe('1.2');
    expect(uiScale()).toBe(1.2);
  });
});
