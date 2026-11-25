// Who gets the data of a cloud connection, and where (Phase 26, 26.5): the words the consent
// dialog, the "cloud" badge and the settings use, from what the server says about the connection.

import type { AssistantConnection } from '../api/hooks/assistant';

/** "Google Cloud (Vertex AI), europe-west4", "Anthropic", or "" when the server names nobody. */
export function cloudWhere(c: Pick<AssistantConnection, 'provider' | 'location'>): string {
  if (!c.provider) return '';
  return c.location ? `${c.provider}, ${c.location}` : c.provider;
}

/** The sentence that says who runs the connection: "… is a cloud service run by Anthropic." */
export function cloudWho(c: Pick<AssistantConnection, 'label' | 'provider' | 'location'>): string {
  if (!c.provider) return 'runs outside this server.';
  const where =
    c.location === 'global' ? ', wherever it chooses to process each request' : c.location ? `, in ${c.location}` : '';
  return `is a cloud service run by ${c.provider}${where}. It runs outside this server.`;
}
