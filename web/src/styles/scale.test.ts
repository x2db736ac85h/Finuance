import { describe, expect, it } from 'vitest';

import { applyScale, browserZoom, calcScale, sliderStep, uiScale } from './scale';

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
  it('moves auto by the slider: 10 % of the auto size per step, both ways', () => {
    expect(calcScale('auto', 2, -2)).toBe(0.9); // 1.12 × 0.8 = 0.896
    expect(calcScale('auto', 2, -1)).toBe(1.01); // 1.008
    expect(calcScale('auto', 2, 1)).toBe(1.23); // 1.232
    expect(calcScale('auto', 2, 2)).toBe(1.34); // 1.344
    // still follows the zoom: at 125 % the steps scale the smaller auto size
    expect(calcScale('auto', 2.5, 1)).toBe(0.99); // 0.896 × 1.1
  });
  it('clamps the moved size', () => {
    expect(calcScale('auto', 0.5, 2)).toBe(1.45); // 1.25 × 1.2 = 1.5
    expect(calcScale('auto', 3, -2)).toBe(0.75); // 0.85 × 0.8 = 0.68
  });
  it('ignores the step for a fixed size', () => {
    expect(calcScale(1.1, 2, 2)).toBe(1.1);
  });
});

describe('sliderStep', () => {
  it('is the step itself while auto', () => {
    expect(sliderStep('auto', -1, 2)).toBe(-1);
  });
  it('shows a fixed size from an older document at its nearest step', () => {
    expect(sliderStep(0.9, 0, 2)).toBe(-2); // auto 1.12: steps 0.90 1.01 1.12 1.23 1.34
    expect(sliderStep(1, 0, 2)).toBe(-1);
    expect(sliderStep(1.1, 0, 2)).toBe(0);
    expect(sliderStep(1.3, 0, 2)).toBe(2);
  });
});

describe('applyScale', () => {
  it('sets --ui-scale and remembers the value', () => {
    const el = document.createElement('div');
    expect(applyScale(1.2, el)).toBe(1.2);
    expect(el.style.getPropertyValue('--ui-scale')).toBe('1.2');
    expect(uiScale()).toBe(1.2);
  });
  it('applies the slider step to auto', () => {
    const el = document.createElement('div');
    const plain = applyScale('auto', el);
    expect(applyScale('auto', el, 1)).toBe(Math.round(plain * 1.1 * 100) / 100);
  });
});
