import { describe, expect, it } from 'vitest';

import { fmtDay } from './dates';
import { currencyExponent, fmtMoney, fmtSize, minorToDecimal, parseMoneyInput } from './format';

describe('money', () => {
  it('converts minor units exactly', () => {
    expect(minorToDecimal(-4498, 2)).toBe('-44.98');
    expect(minorToDecimal(5, 2)).toBe('0.05');
    expect(minorToDecimal(-5, 2)).toBe('-0.05');
    expect(minorToDecimal(0, 2)).toBe('0.00');
    expect(minorToDecimal(123456, 0)).toBe('123456');
    expect(minorToDecimal(9007199254740991, 2)).toBe('90071992547409.91');
  });

  it('knows currency exponents', () => {
    expect(currencyExponent('PLN')).toBe(2);
    expect(currencyExponent('JPY')).toBe(0);
    expect(currencyExponent('nope')).toBe(2);
  });

  it('formats with the locale and optionally the code', () => {
    const plain = fmtMoney({ amountMinor: -123456, currency: 'PLN' });
    expect(plain.replace(/[^\d,.-]/g, '')).toMatch(/^-1[,.]?234[.,]56$/);
    expect(fmtMoney({ amountMinor: 1000, currency: 'JPY' }, { currency: true })).toMatch(/1[,. ]?000 JPY$/);
  });

  it('rounds to whole units from the exact decimal', () => {
    const whole = (minor: number) =>
      fmtMoney({ amountMinor: minor, currency: 'PLN' }, { whole: true }).replace(/[^\d-]/g, '');
    expect(whole(-123456)).toBe('-1235');
    expect(whole(123449)).toBe('1234');
    expect(whole(50)).toBe('1');
  });
});

describe('fmtDay / fmtSize', () => {
  it('formats', () => {
    expect(fmtDay('2025-03-07')).toBe('07.03.2025');
    expect(fmtDay(undefined)).toBe('');
    expect(fmtSize(512)).toBe('512 B');
    expect(fmtSize(2048)).toBe('2.0 KB');
  });
});

describe('parseMoneyInput', () => {
  it('reads the legacy number forms exactly', () => {
    const cases: [string, number | null][] = [
      ['1 234,56', 123456],
      ['1,234.56', 123456],
      ['1.234,56', 123456],
      ['1234.5', 123450],
      ['-80', -8000],
      ['+12', 1200],
      ['900 zł', 90000],
      ['PLN 15,5', 1550],
      ['0,005', 1], // half away from zero
      ['0,004', 0],
      ['1.234.567', 123456700], // dots as thousands
      ['1,234,567', 123456700],
      ['0.1', 10],
      ['', null],
      ['abc', null],
      ['12-3', null],
      ['1234567890123456', null], // more than 15 digits
    ];
    for (const [s, want] of cases) expect(parseMoneyInput(s, 'PLN'), s).toBe(want);
  });
  it('follows the currency exponent', () => {
    expect(parseMoneyInput('1234,6', 'JPY')).toBe(1235);
    expect(parseMoneyInput('-0', 'PLN')).toBe(0);
  });
});
