// Words for the Shared tab (Phase 19): group kinds and the "next time" hints. The server sends
// facts (kind, ratio, category); the sentences live here, next to the other UI text.

import type { SharedKind, TimelineGroup, TimelineHint } from '../api/hooks/shared';

export const KIND_LABEL: Record<SharedKind, string> = {
  trip: 'Trip',
  household: 'Household',
  event: 'Event',
  other: 'Other',
};

export const TIMELINE_KIND_LABEL: Record<TimelineGroup['kind'], string> = { personal: 'Personal group', ...KIND_LABEL };

const KIND_PLURAL: Record<TimelineGroup['kind'], string> = {
  personal: 'personal groups',
  trip: 'trips',
  household: 'households',
  event: 'events',
  other: 'groups',
};

const times = (r: number) => `${r.toFixed(1).replace('.', ',')}×`;

/** A hint as a sentence about its group. */
export function hintText(h: TimelineHint, g: TimelineGroup | undefined): string {
  const name = g?.name ?? 'This group';
  const others = KIND_PLURAL[g?.kind ?? 'other'];
  switch (h.kind) {
    case 'over_budget':
      return `${name} went over its budget: ${times(h.ratio)} of what was planned.`;
    case 'per_day_high':
      return `${name} cost ${times(h.ratio)} per day what your other ${others} usually do.`;
    case 'category_high':
      return `On ${name} you spent ${times(h.ratio)} your usual per day on ${h.category || 'uncategorised things'} — worth a look next time.`;
  }
}
