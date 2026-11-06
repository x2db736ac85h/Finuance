// The grid's columns (PLAN §7.7): one fixed type per column, the cell text, the default width in
// logical pixels (× the interface size on screen) and the sort labels. Ids are the backend's
// FilterColumn names, so filters, sort and export use them directly.

import type { Schemas } from '../api/client';
import { fmtDay } from '../lib/dates';
import { fmtMoney } from '../lib/format';

export type ColumnId = Schemas['FilterColumn'];
export type ColumnType = 'date' | 'number' | 'text';
export type Row = Schemas['TransactionRow'];

export type ColumnDef = {
  id: ColumnId;
  label: string;
  type: ColumnType;
  width: number;
  /** The text shown (and copied, and searched for highlights). */
  text: (r: Row) => string;
  /** Sign of a number cell, for `.neg`. */
  negative?: (r: Row) => boolean;
};

const money = (m: Row['amount'] | undefined) => (m ? fmtMoney(m) : '');

export const COLUMNS: readonly ColumnDef[] = [
  { id: 'bookedOn', label: 'Date', type: 'date', width: 104, text: (r) => fmtDay(r.bookedOn) },
  { id: 'valueOn', label: 'Value date', type: 'date', width: 104, text: (r) => fmtDay(r.valueOn) },
  { id: 'payee', label: 'Payee', type: 'text', width: 240, text: (r) => r.payee },
  { id: 'description', label: 'Description', type: 'text', width: 300, text: (r) => r.description },
  {
    id: 'amount',
    label: 'Amount',
    type: 'number',
    width: 120,
    text: (r) => money(r.amount),
    negative: (r) => r.amount.amountMinor < 0,
  },
  {
    id: 'balanceAfter',
    label: 'Balance after',
    type: 'number',
    width: 120,
    text: (r) => money(r.balanceAfter),
    negative: (r) => (r.balanceAfter?.amountMinor ?? 0) < 0,
  },
  { id: 'currency', label: 'Currency', type: 'text', width: 80, text: (r) => r.amount.currency },
  { id: 'category', label: 'Category', type: 'text', width: 170, text: (r) => r.category?.name ?? '' },
  { id: 'bankCategory', label: 'Bank category', type: 'text', width: 170, text: (r) => r.bankCategory },
  { id: 'txType', label: 'Type', type: 'text', width: 150, text: (r) => r.txType },
  { id: 'reference', label: 'Reference', type: 'text', width: 170, text: (r) => r.reference },
  {
    id: 'counterpartyAccount',
    label: 'Counterparty account',
    type: 'text',
    width: 200,
    text: (r) => r.counterpartyAccount,
  },
  { id: 'account', label: 'Account', type: 'text', width: 150, text: (r) => r.accountName },
  { id: 'note', label: 'Note', type: 'text', width: 200, text: (r) => r.note },
  { id: 'group', label: 'Group', type: 'text', width: 140, text: (r) => r.group?.name ?? '' },
  // Bank, Manual or Assistant (a row the assistant added; ADR 0030)
  { id: 'source', label: 'Source', type: 'text', width: 100, text: (r) => r.source },
];

export const columnById = (id: ColumnId): ColumnDef | undefined => COLUMNS.find((c) => c.id === id);

/** Sort entry labels per type (legacy column menu). */
export const SORT_LABELS: Record<ColumnType, [asc: string, desc: string]> = {
  text: ['A → Z', 'Z → A'],
  number: ['1 → 9', '9 → 1'],
  date: ['Oldest first', 'Newest first'],
};

/** Condition help per type (legacy HELP): groups shown as "codes text", joined with " · ". */
export const HELP: Record<ColumnType, { codes: string[]; text?: string }[]> = {
  text: [
    { codes: ['abc'], text: 'contains' },
    { codes: ['!abc'], text: 'excludes' },
    { codes: ['=abc'], text: 'exact' },
    { codes: ['='], text: 'empty' },
    { codes: ['!='], text: 'not empty' },
  ],
  number: [{ codes: ['>100', '<=0', '-50..50', '=44,98', '!=0'] }, { codes: [], text: 'plain text = contains' }],
  date: [{ codes: ['>=01.03.2025', '2025-03', '2024', '01.2025..03.2025', '<2025'] }],
};

/** Whether a view's rows carry balances (Balance after is offered only then, legacy rule). */
export function hasBalance(rows: readonly Row[] | undefined): boolean {
  return !!rows?.some((r) => r.balanceAfter !== undefined);
}

export type Availability = { groups: boolean; balance: boolean };

/**
 * The columns a view can show: the Group column once the user has a group, Balance after when the
 * view's rows carry balances (legacy rule).
 */
export function availableColumns(a: Availability): ColumnDef[] {
  return COLUMNS.filter((c) => (c.id !== 'group' || a.groups) && (c.id !== 'balanceAfter' || a.balance));
}

/** Width of the row-number gutter for `total` rows (legacy gutterW), in logical pixels. */
export function gutterWidth(total: number): number {
  return Math.max(44, String(Math.max(1, total)).length * 7.5 + 22);
}
