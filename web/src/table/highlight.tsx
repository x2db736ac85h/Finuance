// Search-hit highlighting (port of legacy hl()): the tokens are found in the normalised text and
// the matching original characters are wrapped in <mark>. When normalising a character changes its
// length (e.g. "ß" or a ligature), positions no longer line up and the text is shown unmarked.

import type { ReactNode } from 'react';

import { norm } from '../lib/text';

export function hl(text: string, tokens: readonly string[]): ReactNode {
  if (!tokens.length || !text) return text;
  const chars = Array.from(text);
  const nc = chars.map((c) => norm(c));
  if (nc.some((x) => x.length !== 1)) return text;
  const ns = nc.join('');
  const marks = new Uint8Array(chars.length);
  let any = false;
  for (const t of tokens) {
    let p = ns.indexOf(t);
    while (p !== -1) {
      marks.fill(1, p, p + t.length);
      any = true;
      p = ns.indexOf(t, p + t.length);
    }
  }
  if (!any) return text;
  const out: ReactNode[] = [];
  let run = '';
  let open = false;
  const flush = () => {
    if (!run) return;
    out.push(open ? <mark key={out.length}>{run}</mark> : run);
    run = '';
  };
  chars.forEach((c, k) => {
    const m = marks[k] === 1;
    if (m !== open) {
      flush();
      open = m;
    }
    run += c;
  });
  flush();
  return out;
}
