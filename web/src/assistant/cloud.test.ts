import { describe, expect, it } from 'vitest';

import { cloudWhere, cloudWho } from './cloud';

describe('who gets the data of a cloud connection', () => {
  it('names the provider and the place when the server says them', () => {
    expect(cloudWhere({ provider: 'Google Cloud (Vertex AI)', location: 'europe-west4' })).toBe(
      'Google Cloud (Vertex AI), europe-west4',
    );
    expect(cloudWhere({ provider: 'Anthropic' })).toBe('Anthropic');
    expect(cloudWhere({})).toBe('');
  });
  it('says what "global" means instead of naming it as a place', () => {
    expect(cloudWho({ label: 'Gemini', provider: 'Google Cloud (Vertex AI)', location: 'global' })).toBe(
      'is a cloud service run by Google Cloud (Vertex AI), wherever it chooses to process each request. It runs outside this server.',
    );
    expect(cloudWho({ label: 'Claude', provider: 'Anthropic' })).toBe(
      'is a cloud service run by Anthropic. It runs outside this server.',
    );
    // an older server names nobody: the sentence that was always there
    expect(cloudWho({ label: 'Cloud' })).toBe('runs outside this server.');
  });
});
