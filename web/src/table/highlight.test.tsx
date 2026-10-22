import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { norm, searchTokens } from '../lib/text';
import { hl } from './highlight';

/** The rendered markup with marks as [..]. */
function marked(text: string, search: string): string {
  const { container } = render(<span>{hl(text, searchTokens(search))}</span>);
  const nodes = [...(container.firstElementChild?.childNodes ?? [])];
  return nodes.map((n) => (n.nodeName === 'MARK' ? `[${n.textContent ?? ''}]` : (n.textContent ?? ''))).join('');
}

describe('norm', () => {
  it('folds case, accents and ł like the legacy', () => {
    expect(norm('PŁATNOŚĆ BLIK')).toBe('platnosc blik');
    expect(norm('Zażółć gęślą jaźń')).toBe('zazolc gesla jazn');
  });
});

describe('hl', () => {
  it('marks accent-insensitive hits on the original characters', () => {
    expect(marked('PŁATNOŚĆ BLIK', 'platnosc')).toBe('[PŁATNOŚĆ] BLIK');
    expect(marked('Zażółć gęślą jaźń', 'zolc jazn')).toBe('Za[żółć] gęślą [jaźń]');
  });

  it('marks every occurrence and merges touching hits', () => {
    expect(marked('kino KINO kinoteka', 'kino')).toBe('[kino] [KINO] [kino]teka');
    expect(marked('abcabc', 'abc')).toBe('[abcabc]');
  });

  it('leaves text alone without hits, tokens or text', () => {
    expect(marked('Sklep', 'kino')).toBe('Sklep');
    expect(marked('Sklep', '')).toBe('Sklep');
    expect(hl('', ['x'])).toBe('');
  });

  it('skips highlighting when normalising changes the length', () => {
    // an emoji is two UTF-16 units after normalising: positions would shift (legacy rule)
    expect(marked('Kino 🎬', 'kino')).toBe('Kino 🎬');
  });

  it('escapes by construction (React text nodes)', () => {
    expect(marked('<b>kino</b>', 'kino')).toBe('<b>[kino]</b>');
    const { container } = render(<span>{hl('<b>kino</b>', ['kino'])}</span>);
    expect(container.querySelector('b')).toBeNull();
    expect(container.querySelectorAll('mark')).toHaveLength(1);
  });
});
