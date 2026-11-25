import { describe, expect, it } from 'vitest';

import { freeName, mayOffer, providerOf, PROVIDERS, providerTag } from './providers';

describe('providers', () => {
  it('offers Anthropic, OpenAI and Google by name, then a local server and other', () => {
    expect(PROVIDERS.map((p) => p.label)).toEqual(['Anthropic', 'OpenAI', 'Google', 'Local server', 'Other']);
  });

  it('reads a connection’s provider from its address and kind', () => {
    const c = (api: string, baseUrl: string, cloud: boolean) => ({ api, baseUrl, cloud });
    expect(providerOf(c('anthropic', 'https://api.anthropic.com', true))).toBe('anthropic');
    expect(providerOf(c('openai', 'https://api.openai.com/v1', true))).toBe('openai');
    expect(
      providerOf(
        c('openai', 'https://europe-west4-aiplatform.googleapis.com/v1/projects/p/locations/l/endpoints/openapi', true),
      ),
    ).toBe('google');
    expect(providerOf(c('openai', 'http://192.168.1.20:11434/v1', false))).toBe('local');
    // LM Studio speaks the Messages format too: local, whatever the format
    expect(providerOf(c('anthropic', 'http://192.168.1.20:1234', false))).toBe('local');
    expect(providerOf(c('openai', 'https://openrouter.ai/api/v1', true))).toBe('other');
    // a look-alike host is not the provider
    expect(providerOf(c('anthropic', 'https://api.anthropic.com.example.test', true))).toBe('other');
    expect(providerOf(c('openai', 'https://evilaiplatform.googleapis.com/v1', true))).toBe('other');
    expect(providerOf(c('openai', 'https://aiplatform.googleapis.com.example.test/v1', true))).toBe('other');
    expect(providerOf(c('openai', 'https://aiplatform.googleapis.com/v1', true))).toBe('google');
    expect(providerOf(c('openai', 'https://aiplatform.eu.rep.googleapis.com/v1', true))).toBe('google');
    expect(providerOf(c('openai', 'not an address', true))).toBe('other');
    expect(providerTag(c('openai', 'https://openrouter.ai/api/v1', true))).toBe('OpenAI-compatible');
    expect(providerTag(c('anthropic', 'https://proxy.example.test', true))).toBe('Anthropic-compatible');
    expect(providerTag(c('openai', 'http://10.0.0.5:1234/v1', false))).toBe('Local server');
  });

  it('suggests a name nobody has', () => {
    expect(freeName('openai', [])).toBe('openai');
    expect(freeName('openai', ['openai'])).toBe('openai-2');
    expect(freeName('openai', ['openai', 'openai-2'])).toBe('openai-3');
    expect(freeName('', ['x'])).toBe('');
  });

  it('refuses a model only when it certainly fits no pattern', () => {
    expect(mayOffer([], 'anything')).toBe(true);
    expect(mayOffer(['qwen3*'], 'qwen3:4b')).toBe(true);
    expect(mayOffer(['QWEN3*'], 'qwen3:4b')).toBe(true);
    expect(mayOffer(['qwen3*', 'gemma3*'], 'llama4')).toBe(false);
    expect(mayOffer(['claude-haiku*'], 'claude-sonnet-5-5')).toBe(false);
    expect(mayOffer(['gpt-5.5'], 'gpt-5x5')).toBe(false); // a dot is a dot
    // what only the server can decide passes here
    expect(mayOffer(['qwen?'], 'llama4')).toBe(true);
    expect(mayOffer(['[a-c]*'], 'llama4')).toBe(true);
    expect(mayOffer(['google/*'], 'google/gemini-3.8-flash')).toBe(true);
  });
});
