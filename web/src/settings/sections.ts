import type { IconName } from '../ui/icons';

/** Settings pages (PLAN §7.3); categories and rules since Phase 15, activity since Phase 17, friends since Phase 19,
 * access tokens since Phase 20. */
export const SECTIONS = [
  { id: 'appearance', label: 'Appearance', icon: 'sun' },
  { id: 'profile', label: 'Profile', icon: 'pen' },
  { id: 'sessions', label: 'Sessions', icon: 'lock' },
  { id: 'tokens', label: 'Access tokens', icon: 'code' },
  { id: 'activity', label: 'Activity', icon: 'book' },
  { id: 'friends', label: 'Friends', icon: 'users' },
  { id: 'categories', label: 'Categories', icon: 'tag' },
  { id: 'rules', label: 'Rules', icon: 'sliders' },
  { id: 'data', label: 'Data', icon: 'download' },
] as const satisfies readonly { id: string; label: string; icon: IconName }[];

export type SectionId = (typeof SECTIONS)[number]['id'];
