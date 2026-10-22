import type { IconName } from '../ui/icons';

/** Settings pages (PLAN §7.3); accounts, categories and rules join in later phases. */
export const SECTIONS = [
  { id: 'appearance', label: 'Appearance', icon: 'sun' },
  { id: 'profile', label: 'Profile', icon: 'pen' },
  { id: 'sessions', label: 'Sessions', icon: 'lock' },
  { id: 'data', label: 'Data', icon: 'download' },
] as const satisfies readonly { id: string; label: string; icon: IconName }[];

export type SectionId = (typeof SECTIONS)[number]['id'];
