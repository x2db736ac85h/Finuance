// The providers a connection can be made for (Phase 26, U12). A connection itself only knows its
// wire format (`api`), its address and whether it is a cloud service; the provider is a way to
// fill those in without knowing them, and is read back from them for the list.

export type ProviderId = 'anthropic' | 'openai' | 'google' | 'local' | 'other';

export type Provider = {
  id: ProviderId;
  label: string;
  /** One line under the name on its card. */
  about: string;
  /** What choosing it fills in. */
  preset: { api: 'openai' | 'anthropic'; baseUrl: string; cloud: boolean; name: string; label: string };
  /** The address is the provider's own: shown under "More options", not asked for. */
  fixedAddress: boolean;
  /** Not offered yet, with the reason. */
  later?: string;
};

export const PROVIDERS: readonly Provider[] = [
  {
    id: 'anthropic',
    label: 'Anthropic',
    about: 'Claude models, with an API key',
    preset: { api: 'anthropic', baseUrl: 'https://api.anthropic.com', cloud: true, name: 'anthropic', label: 'Claude' },
    fixedAddress: true,
  },
  {
    id: 'openai',
    label: 'OpenAI',
    about: 'GPT models, with an API key',
    preset: { api: 'openai', baseUrl: 'https://api.openai.com/v1', cloud: true, name: 'openai', label: 'OpenAI' },
    fixedAddress: true,
  },
  {
    id: 'google',
    label: 'Google',
    about: 'Gemini models on Vertex AI, without a stored key',
    preset: { api: 'openai', baseUrl: '', cloud: true, name: 'google', label: 'Gemini' },
    fixedAddress: true,
    later: 'Arrives with the cloud AI part of this release (short-lived Google credentials).',
  },
  {
    id: 'local',
    label: 'Local server',
    about: 'Ollama, LM Studio or another server on your network',
    preset: { api: 'openai', baseUrl: '', cloud: false, name: 'local', label: 'Local models' },
    fixedAddress: false,
  },
  {
    id: 'other',
    label: 'Other',
    about: 'Any OpenAI- or Anthropic-compatible address',
    preset: { api: 'openai', baseUrl: '', cloud: false, name: '', label: '' },
    fixedAddress: false,
  },
];

export const providerById = (id: ProviderId): Provider =>
  PROVIDERS.find((p) => p.id === id) ?? (PROVIDERS[4] as Provider);

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return '';
  }
}

/** The provider a connection belongs to, from its address and kind. */
export function providerOf(c: { api: string; baseUrl: string; cloud: boolean }): ProviderId {
  const host = hostOf(c.baseUrl);
  if (host === 'api.anthropic.com') return 'anthropic';
  if (host === 'api.openai.com') return 'openai';
  if (host.endsWith('aiplatform.googleapis.com') || host === 'generativelanguage.googleapis.com') return 'google';
  return c.cloud ? 'other' : 'local';
}

/** The words for a connection's provider in the list ("Other" says which wire format). */
export function providerTag(c: { api: string; baseUrl: string; cloud: boolean }): string {
  const id = providerOf(c);
  if (id === 'other') return c.api === 'anthropic' ? 'Anthropic-compatible' : 'OpenAI-compatible';
  return providerById(id).label;
}

/** A name nobody has yet: the provider's, then `-2`, `-3`, … */
export function freeName(base: string, taken: readonly string[]): string {
  if (!base || !taken.includes(base)) return base;
  for (let n = 2; ; n++) if (!taken.includes(`${base}-${String(n)}`)) return `${base}-${String(n)}`;
}

/**
 * False only when a model id certainly fits none of the patterns, so a form can say so before
 * saving. The server decides (Go's path.Match, case-insensitive); this knows `*` alone and says
 * "may fit" for anything else (`?`, `[…]`, a `/` in the id). No patterns offer everything.
 */
export function mayOffer(patterns: readonly string[], model: string): boolean {
  if (!patterns.length || model.includes('/')) return true;
  return patterns.some((p) => {
    if (/[?[\]\\]/.test(p)) return true;
    const re = new RegExp(
      `^${p
        .split('*')
        .map((part) => part.replace(/[.+^${}()|]/g, '\\$&'))
        .join('.*')}$`,
      'i',
    );
    return re.test(model);
  });
}
