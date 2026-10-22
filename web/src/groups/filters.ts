// Group filters in views (legacy renameGroupFilters/toggleFilter): the Group column's `=Name`
// condition follows a rename and goes away with the group.

import { without } from '../lib/objects';
import { useViews, viewOf } from '../stores/viewStore';

/** Renames `=old` group conditions in every view to `=next`, or clears them (next = null). */
export function renameGroupFilters(old: string, next: string | null): void {
  const { views, patch } = useViews.getState();
  for (const [id, v] of Object.entries(views)) {
    const f = v.filters.group;
    if (f?.cond !== `=${old}`) continue;
    const cond = next === null ? '' : `=${next}`;
    if (!cond && !f.excluded.length) patch(id, { filters: without(v.filters, 'group') });
    else patch(id, { filters: { ...v.filters, group: { ...f, cond } } });
  }
}

/** The group a view is filtered to (`=Name`), if any. */
export function activeGroup(viewId: string): string | null {
  const cond = viewOf(useViews.getState().views, viewId).filters.group?.cond ?? '';
  return cond.startsWith('=') ? cond.slice(1) : null;
}

/** Shows only the group's transactions, or everything again when it is already the filter. */
export function toggleGroupFilter(viewId: string, name: string): void {
  const { patch, views } = useViews.getState();
  const v = viewOf(views, viewId);
  if (activeGroup(viewId) === name) {
    patch(viewId, { filters: without(v.filters, 'group') });
    return;
  }
  patch(viewId, {
    filters: { ...v.filters, group: { cond: `=${name}`, excluded: [] } },
    hidden: v.hidden.filter((c) => c !== 'group'),
  });
}
