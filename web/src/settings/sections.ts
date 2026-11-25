import type { IconName } from '../ui/icons';

/** Settings pages (PLAN §7.3); categories and rules since Phase 15, activity since Phase 17, friends since Phase 19,
 * access tokens and General (the first, where /settings opens) since Phase 20, security (two-step sign-in) since
 * Phase 23. Since Phase 26 Appearance is part of General, Sessions and Access tokens are parts of Security, and Friends are on the Shared page. */
export const SECTIONS = [
  { id: 'general', label: 'General', icon: 'layers' },
  { id: 'profile', label: 'Profile', icon: 'pen' },
  { id: 'security', label: 'Security', icon: 'shield' },
  { id: 'activity', label: 'Activity', icon: 'book' },
  { id: 'categories', label: 'Categories', icon: 'tag' },
  { id: 'rules', label: 'Rules', icon: 'sliders' },
  { id: 'data', label: 'Data', icon: 'download' },
] as const satisfies readonly { id: string; label: string; icon: IconName }[];

export type SectionId = (typeof SECTIONS)[number]['id'];
