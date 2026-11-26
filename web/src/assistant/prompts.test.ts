import { describe, expect, it } from 'vitest';

import { PROMPT_GALLERY } from './prompts';

describe('the prompt gallery', () => {
  it('has the three groups, each with prompts that fit a question', () => {
    expect(PROMPT_GALLERY.map((g) => g.title)).toEqual(['Actions', 'Questions', 'Budget planning']);
    const all = PROMPT_GALLERY.flatMap((g) => g.prompts);
    expect(all.length).toBeGreaterThanOrEqual(15);
    for (const p of all) {
      expect(p.title.trim()).not.toBe('');
      // a question is at most 4,000 characters; these are a sentence or two
      expect(p.text.length).toBeGreaterThan(20);
      expect(p.text.length).toBeLessThan(300);
      // brackets come in pairs: a half-open one would be sent as it is
      expect((p.text.match(/\[/g) ?? []).length).toBe((p.text.match(/\]/g) ?? []).length);
    }
    expect(new Set(all.map((p) => p.title)).size).toBe(all.length);
  });
});
