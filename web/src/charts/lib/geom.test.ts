import { describe, expect, it } from 'vitest';

import { arc, barPath, heatBin, quintiles, roundNice, spark } from './geom';

describe('barPath', () => {
  it('rounds the data end of a bar growing up', () => {
    expect(barPath(10, 20, 8, 30, true)).toBe('M10 50V23Q10 20 13 20H15Q18 20 18 23V50Z');
  });
  it('rounds the data end of a bar hanging down', () => {
    expect(barPath(10, 20, 8, 30, false)).toBe('M10 20V47Q10 50 13 50H15Q18 50 18 47V20Z');
  });
  it('limits the radius by half the width and the height', () => {
    expect(barPath(0, 0, 2, 10, true)).toBe('M0 10V1Q0 0 1 0H1Q2 0 2 1V10Z');
    expect(barPath(0, 0, 10, 1, true)).toBe('M0 1V1Q0 0 1 0H9Q10 0 10 1V1Z');
  });
  it('draws nothing under half a unit', () => {
    expect(barPath(0, 0, 10, 0.4, true)).toBe('');
  });
});

describe('arc', () => {
  it('uses the small-arc flag up to half a turn', () => {
    const d = arc(50, 50, 40, 20, -Math.PI / 2, 0);
    expect(d).toBe('M50.00 10.00A40 40 0 0 1 90.00 50.00L70.00 50.00A20 20 0 0 0 50.00 30.00Z');
  });
  it('uses the large-arc flag beyond half a turn', () => {
    expect(arc(0, 0, 10, 5, 0, Math.PI * 1.5)).toContain('A10 10 0 1 1');
  });
  it('shortens a full turn so it stays drawable', () => {
    const d = arc(0, 0, 10, 5, 0, Math.PI * 2);
    expect(d).toContain('A10 10 0 1 1');
    expect(d.startsWith('M10.00 0.00')).toBe(true);
    expect(d).not.toContain('NaN');
  });
});

describe('spark', () => {
  it('scales the values into the box with gaps for missing values', () => {
    const s = spark([0, null, 10]);
    expect(s?.d).toBe('M3.0 26.0L107.0 4.0');
    expect(s?.last).toEqual([107, 4]);
  });
  it('draws a flat line for equal values and nothing for one point', () => {
    expect(spark([5, 5])?.d).toBe('M3.0 26.0L107.0 26.0');
    expect(spark([5, null])).toBeNull();
  });
});

describe('roundNice', () => {
  it.each([
    [0, 0],
    [-5, 0],
    [1, 5],
    [4, 5],
    [5, 5],
    [6, 10],
    [99, 100],
    [100, 100],
    [101, 100],
    [105, 110],
    [2849, 2850],
  ])('roundNice(%d) = %d', (v, want) => {
    expect(roundNice(v)).toBe(want);
  });
});

describe('heat bins', () => {
  it('bins by the 20/40/60/80 % values', () => {
    const q = quintiles([10, 20, 30, 40, 50, 60]);
    expect(q).toEqual([20, 30, 40, 50]);
    expect([0, 5, 20, 21, 35, 45, 50, 60].map((v) => heatBin(v, q))).toEqual([0, 1, 1, 2, 3, 4, 4, 5]);
  });
  it('has zero thresholds without values', () => {
    expect(quintiles([])).toEqual([0, 0, 0, 0]);
  });
});
