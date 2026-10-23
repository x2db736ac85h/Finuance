// Toolbar chips (PLAN §7.6, legacy renderToolbar): one removable chip per thing that narrows or
// orders the view — period, search, sort, each condition, excluded values, hidden columns.

import type { IconName } from '../ui/icons';
import { without } from '../lib/objects';
import { newView, type ViewState } from '../stores/viewStore';
import { columnById, type ColumnId } from './columns';

export type Chip = { key: string; icon?: IconName; label?: string; value: string };

/** Columns the view hides beyond its defaults (the defaults never make a chip). */
export function extraHidden(viewId: string, hidden: readonly string[]): string[] {
  const defaults = newView(viewId).hidden;
  return hidden.filter((c) => !defaults.includes(c as ColumnId));
}

export function chipsOf(viewId: string, v: ViewState, periodLabel: string | undefined): Chip[] {
  const out: Chip[] = [];
  const label = (c: string) => columnById(c as ColumnId)?.label ?? c;
  if (periodLabel && v.period !== 'all') out.push({ key: 'period', icon: 'cal', value: periodLabel });
  if (v.toolFilter) out.push({ key: 'tool', label: '✦', value: v.toolFilter.label ?? 'assistant lookup' });
  if (v.search.trim()) out.push({ key: 'search', icon: 'search', value: v.search.trim() });
  if (v.sort) out.push({ key: 'sort', icon: v.sort.dir === 'asc' ? 'up' : 'down', value: label(v.sort.column) });
  for (const [col, f] of Object.entries(v.filters)) {
    if (!f) continue;
    if (f.cond.trim()) out.push({ key: `f:${col}`, label: label(col), value: f.cond.trim() });
    const n = f.excluded.length;
    if (n) out.push({ key: `x:${col}`, label: label(col), value: `−${String(n)} value${n > 1 ? 's' : ''}` });
  }
  const hidden = extraHidden(viewId, v.hidden).length;
  if (hidden) out.push({ key: 'hidden', icon: 'eyeoff', value: `${String(hidden)} hidden` });
  return out;
}

/** The view change that removes a chip. */
export function withoutChip(viewId: string, v: ViewState, key: string): Partial<ViewState> {
  if (key === 'period') return { period: 'all', custom: null };
  if (key === 'search') return { search: '' };
  if (key === 'tool') return { toolFilter: null };
  if (key === 'sort') return { sort: null };
  if (key === 'hidden') return { hidden: [...newView(viewId).hidden] };
  const col = key.slice(2);
  const f = v.filters[col];
  if (!f) return {};
  const next = key.startsWith('f:') ? { ...f, cond: '' } : { ...f, excluded: [] };
  if (!next.cond && !next.excluded.length) return { filters: without(v.filters, col) };
  return { filters: { ...v.filters, [col]: next } };
}

/** Legacy clearAll: search, conditions, values, sort, hidden columns and period. */
export function cleared(viewId: string): Partial<ViewState> {
  return {
    search: '',
    filters: {},
    sort: null,
    hidden: [...newView(viewId).hidden],
    period: 'all',
    custom: null,
    toolFilter: null,
  };
}
