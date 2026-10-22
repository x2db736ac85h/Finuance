// Column meanings for the import preview (legacy MAP_ROLES, PLAN §4.3). A mapping assigns each
// canonical role at most one column; debit and credit live in the options (amountMode
// debit_credit), and "ignore" may hold many columns.

import type { ColRef, ImportOptions, Mapping } from '../api/hooks/imports';

export type SingleRole = Exclude<keyof Mapping, 'ignore'>;
export type Role = SingleRole | 'debit' | 'credit' | 'ignore';

/** Menu order and labels, as in the legacy role menu. */
export const ROLES: { role: Role; label: string; short: string }[] = [
  { role: 'bookedOn', label: 'Date', short: 'Date' },
  { role: 'valueOn', label: 'Value date', short: 'Value date' },
  { role: 'amount', label: 'Amount (signed)', short: 'Amount' },
  { role: 'debit', label: 'Amount — outflow / debit', short: 'Debit' },
  { role: 'credit', label: 'Amount — inflow / credit', short: 'Credit' },
  { role: 'payee', label: 'Name — payee / merchant', short: 'Name' },
  { role: 'description', label: 'Description / title', short: 'Description' },
  { role: 'category', label: 'Category', short: 'Category' },
  { role: 'currency', label: 'Currency', short: 'Currency' },
  { role: 'reference', label: 'Reference / transaction ID', short: 'Reference' },
  { role: 'txType', label: 'Operation type', short: 'Type' },
  { role: 'counterpartyAccount', label: 'Counterparty account', short: 'Counterparty' },
  { role: 'ownAccount', label: 'Own account', short: 'Account' },
  { role: 'balance', label: 'Balance after operation', short: 'Balance' },
  { role: 'ignore', label: "Ignore — don't import", short: 'Ignored' },
];

export const roleLabel = (r: Role) => ROLES.find((x) => x.role === r)?.short ?? r;

const SINGLE: SingleRole[] = ROLES.map((r) => r.role).filter(
  (r): r is SingleRole => r !== 'debit' && r !== 'credit' && r !== 'ignore',
);

/** The role column j has in this mapping, or null. */
export function roleOf(m: Mapping, o: ImportOptions, j: number): Role | null {
  if (o.amountMode === 'debit_credit') {
    if (o.debitColumn?.index === j) return 'debit';
    if (o.creditColumn?.index === j) return 'credit';
  }
  for (const r of SINGLE) if (m[r]?.index === j) return r;
  if (m.ignore?.some((c) => c.index === j)) return 'ignore';
  return null;
}

/**
 * Gives column `col` the role `role` (null: not mapped), taking the role away from any other
 * column (each meaning once, legacy rule). Debit/credit switch the amount mode on; a signed amount
 * switches it off.
 */
export function assignRole(
  m: Mapping,
  o: ImportOptions,
  col: ColRef,
  role: Role | null,
): { mapping: Mapping; options: ImportOptions } {
  // the column loses whatever role it had
  const mapping: Mapping = { ignore: (m.ignore ?? []).filter((c) => c.index !== col.index) };
  for (const r of SINGLE) {
    const c = m[r];
    if (c && c.index !== col.index) mapping[r] = c;
  }
  const options: ImportOptions = { ...o };
  if (options.debitColumn?.index === col.index) delete options.debitColumn;
  if (options.creditColumn?.index === col.index) delete options.creditColumn;
  if (role === 'ignore') mapping.ignore = [...(mapping.ignore ?? []), col];
  else if (role === 'debit' || role === 'credit') {
    options.amountMode = 'debit_credit';
    options[role === 'debit' ? 'debitColumn' : 'creditColumn'] = col;
    delete mapping.amount;
  } else if (role) {
    mapping[role] = col;
    if (role === 'amount') {
      options.amountMode = 'single';
      delete options.debitColumn;
      delete options.creditColumn;
    }
  }
  if (options.amountMode === 'debit_credit' && !options.debitColumn && !options.creditColumn)
    options.amountMode = 'single';
  return { mapping, options };
}

/**
 * Legacy "Use detected meanings": every column detection recognised gets its role, unless the
 * column already has one or the role is taken.
 */
export function applyDetected(m: Mapping, o: ImportOptions, detected: Mapping, headers: readonly string[]) {
  let cur = { mapping: m, options: o };
  for (const r of SINGLE) {
    const d = detected[r];
    if (!d || d.index >= headers.length) continue;
    if (cur.mapping[r] || roleOf(cur.mapping, cur.options, d.index)) continue;
    if (r === 'amount' && cur.options.amountMode === 'debit_credit') continue;
    cur = assignRole(cur.mapping, cur.options, d, r);
  }
  return cur;
}

/** The detection's role for column j (the "auto" hint), or null. */
export function detectedRole(detected: Mapping | null, j: number): SingleRole | null {
  if (!detected) return null;
  return SINGLE.find((r) => detected[r]?.index === j) ?? null;
}

/** Legacy required-fields footer: Date, Amount, Name, Description with ✓ or !. */
export function requiredChecks(m: Mapping, o: ImportOptions): { label: string; ok: boolean; required: boolean }[] {
  const amount = !!m.amount || (o.amountMode === 'debit_credit' && !!o.debitColumn && !!o.creditColumn);
  return [
    { label: 'Date', ok: !!m.bookedOn, required: true },
    { label: 'Amount', ok: amount, required: true },
    { label: 'Name', ok: !!m.payee, required: false },
    { label: 'Description', ok: !!m.description, required: false },
  ];
}
