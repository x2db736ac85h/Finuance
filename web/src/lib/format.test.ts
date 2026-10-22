import { describe, expect, it } from 'vitest';

import { fmtDay } from './dates';
import { currencyExponent, fmtMoney, fmtSize, minorToDecimal } from './format';

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
});

describe('fmtDay / fmtSize', () => {
  it('formats', () => {
    expect(fmtDay('2025-03-07')).toBe('07.03.2025');
    expect(fmtDay(undefined)).toBe('');
    expect(fmtSize(512)).toBe('512 B');
    expect(fmtSize(2048)).toBe('2.0 KB');
  });
});
