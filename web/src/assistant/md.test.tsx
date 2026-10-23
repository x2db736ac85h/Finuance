import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Markdown } from './md';

/** The rendered markup, serialised by hand (the lint rule keeps innerHTML out of the codebase). */
function serialise(n: Node): string {
  if (n.nodeType === Node.TEXT_NODE)
    return (n.textContent ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  if (!(n instanceof Element)) return '';
  const tag = n.tagName.toLowerCase();
  const cls = n.getAttribute('class');
  const open = `<${tag}${cls ? ` class="${cls}"` : ''}>`;
  if (tag === 'br') return open;
  return `${open}${[...n.childNodes].map(serialise).join('')}</${tag}>`;
}

const html = (text: string) => [...render(<Markdown text={text} />).container.childNodes].map(serialise).join('');

describe('Markdown', () => {
  it('escapes everything the model writes', () => {
    const out = html('<img src=x onerror=alert(1)> **<b>x</b>**');
    expect(out).not.toContain('<img');
    expect(out).toContain('&lt;img src=x onerror=alert(1)&gt;');
    expect(out).toContain('<b>&lt;b&gt;x&lt;/b&gt;</b>');
  });

  it('writes paragraphs with line breaks and inline formatting', () => {
    expect(html('You spent **5 569,62 PLN** in *February*.\nMostly `groceries`.\n\nNext.')).toBe(
      '<p>You spent <b>5 569,62 PLN</b> in <i>February</i>.<br>Mostly <code>groceries</code>.</p><p>Next.</p>',
    );
    // underscores inside words stay as they are
    expect(html('snake_case_name and _this_')).toBe('<p>snake_case_name and <i>this</i></p>');
  });

  it('renders tables with numbers aligned right', () => {
    const out = html('| Category | Spent |\n|---|---:|\n| Groceries | 1 095,12 PLN |\n| Rent | 2850 |\nafter');
    expect(out).toBe(
      '<div class="mdt"><table><thead><tr><th>Category</th><th>Spent</th></tr></thead><tbody>' +
        '<tr><td>Groceries</td><td class="r">1 095,12 PLN</td></tr><tr><td>Rent</td><td class="r">2850</td></tr>' +
        '</tbody></table></div><p>after</p>',
    );
  });

  it('renders lists, headings and code blocks', () => {
    expect(html('## Top\n- a\n- **b**\n\n1. one\n2) two')).toBe(
      '<h4>Top</h4><ul><li>a</li><li><b>b</b></li></ul><ol><li>one</li><li>two</li></ol>',
    );
    // like the legacy, a bullet right after a numbered line continues the same list
    expect(html('1. one\n- two')).toBe('<ol><li>one</li><li>two</li></ol>');
    expect(html('```sql\nSELECT *\n```\nok')).toBe('<pre><code>SELECT *</code></pre><p>ok</p>');
    // an unfinished fence (still streaming) is a code block too
    expect(html('```\npartial')).toBe('<pre><code>partial</code></pre>');
  });
});
