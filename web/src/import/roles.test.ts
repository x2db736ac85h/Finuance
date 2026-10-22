import { describe, expect, it } from 'vitest';

import type { ImportOptions, Mapping } from '../api/hooks/imports';
import { applyDetected, assignRole, detectedRole, requiredChecks, roleOf } from './roles';

const col = (index: number, header = `c${index}`) => ({ index, header });
const base: Mapping = { bookedOn: col(0), payee: col(1), amount: col(2), ignore: [] };
const single: ImportOptions = { amountMode: 'single' };

describe('assignRole', () => {
  it('moves a role to another column (each meaning once)', () => {
    const { mapping } = assignRole(base, single, col(3), 'bookedOn');
    expect(mapping.bookedOn).toEqual(col(3));
    expect(roleOf(mapping, single, 0)).toBeNull();
  });

  it('replaces the role a column had', () => {
    const { mapping } = assignRole(base, single, col(1), 'description');
    expect(mapping.payee).toBeUndefined();
    expect(mapping.description).toEqual(col(1));
  });

  it('ignores several columns and un-ignores one', () => {
    let r = assignRole(base, single, col(4), 'ignore');
    r = assignRole(r.mapping, r.options, col(5), 'ignore');
    expect(r.mapping.ignore).toEqual([col(4), col(5)]);
    r = assignRole(r.mapping, r.options, col(4), null);
    expect(r.mapping.ignore).toEqual([col(5)]);
  });

  it('switches to debit/credit and back', () => {
    let r = assignRole(base, single, col(3), 'debit');
    expect(r.options).toMatchObject({ amountMode: 'debit_credit', debitColumn: col(3) });
    expect(r.mapping.amount).toBeUndefined();
    r = assignRole(r.mapping, r.options, col(4), 'credit');
    expect(roleOf(r.mapping, r.options, 4)).toBe('credit');
    expect(requiredChecks(r.mapping, r.options)[1]?.ok).toBe(true);
    r = assignRole(r.mapping, r.options, col(2), 'amount');
    expect(r.options.amountMode).toBe('single');
    expect(r.options.debitColumn).toBeUndefined();
    expect(roleOf(r.mapping, r.options, 3)).toBeNull();
  });

  it('falls back to a single amount when both debit and credit are removed', () => {
    let r = assignRole(base, single, col(3), 'debit');
    r = assignRole(r.mapping, r.options, col(3), null);
    expect(r.options.amountMode).toBe('single');
  });
});

describe('applyDetected', () => {
  it('fills unmapped roles from detection without taking mapped columns', () => {
    const user: Mapping = { bookedOn: col(1), ignore: [] }; // the user put the date on column 1
    const detected: Mapping = { bookedOn: col(0), payee: col(1), amount: col(2), category: col(3), ignore: [] };
    const { mapping } = applyDetected(user, single, detected, ['a', 'b', 'c', 'd']);
    expect(mapping.bookedOn).toEqual(col(1));
    expect(mapping.payee).toBeUndefined(); // column 1 already has a role
    expect(mapping.amount).toEqual(col(2));
    expect(mapping.category).toEqual(col(3));
  });

  it('gives the auto hint per column', () => {
    expect(detectedRole(base, 2)).toBe('amount');
    expect(detectedRole(base, 9)).toBeNull();
    expect(detectedRole(null, 0)).toBeNull();
  });
});

describe('requiredChecks', () => {
  it('marks missing fields', () => {
    const checks = requiredChecks({ ignore: [] }, single);
    expect(checks.map((c) => `${c.label}:${String(c.ok)}`)).toEqual([
      'Date:false',
      'Amount:false',
      'Name:false',
      'Description:false',
    ]);
  });
});
