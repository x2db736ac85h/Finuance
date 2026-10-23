// The legacy md(): a small, safe Markdown subset for the assistant's answers (PLAN task 13.7). It
// builds React nodes, so model text is never parsed as HTML: fenced code, tables (numbers right
// aligned), headings (as h4), bullet and numbered lists, paragraphs with line breaks, and inline
// `code`, **bold**, *italic* and _italic_.

import { Fragment, type ReactNode } from 'react';

const FENCE = /```[\w-]*\n?([\s\S]*?)(```|$)/g;
const TABLE_ROW = /^\s*\|.*\|\s*$/;
const TABLE_SEP = /^\s*\|?\s*:?-{2,}/;
const LIST = /^\s*([-*•]|\d+[.)])\s+/;
const HEADING = /^(#{1,4})\s+(.*)$/;
const NUMERIC = /^[-+−]?[\d\s.,]+(\s?[A-Z]{3}|%)?$/;

/** Inline formatting, in the legacy order: code spans first, then bold, then the two italics. */
export function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  const parts = text.split(/`([^`]+)`/);
  parts.forEach((p, i) => {
    if (i % 2 === 1) out.push(<code key={`c${String(i)}`}>{p}</code>);
    else out.push(...bold(p, `t${String(i)}`));
  });
  return out;
}

function bold(text: string, key: string): ReactNode[] {
  const parts = text.split(/\*\*([^*]+)\*\*/);
  return parts.flatMap((p, i) =>
    i % 2 === 1
      ? [<b key={`${key}b${String(i)}`}>{italics(p, `${key}b${String(i)}`)}</b>]
      : italics(p, `${key}i${String(i)}`),
  );
}

// `*x*` and `_x_` after the start, a space or "(" and before a space, punctuation or the end.
const ITALIC = /(^|[\s(])([*_])([^*_\n]+)\2(?=[\s).,!?:;]|$)/g;

function italics(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const m of text.matchAll(ITALIC)) {
    const [, pre = '', , inner = ''] = m;
    const start = m.index + pre.length;
    out.push(text.slice(last, start));
    out.push(<i key={`${key}-${String(n++)}`}>{inner}</i>);
    last = start + inner.length + 2;
  }
  out.push(text.slice(last));
  return out.filter((x) => x !== '');
}

const cells = (row: string) =>
  row
    .trim()
    .replace(/^\||\|$/g, '')
    .split('|')
    .map((c) => c.trim());

/** Renders an answer. */
export function Markdown({ text }: { text: string }) {
  const blocks: string[] = [];
  const src = text.replace(FENCE, (_m, code: string) => {
    blocks.push(code.replace(/\n$/, ''));
    return `\n\uE000${String(blocks.length - 1)}\uE000\n`;
  });
  const lines = src.split('\n');
  const out: ReactNode[] = [];
  let para: string[] = [];
  const flush = () => {
    if (!para.length) return;
    const p = para;
    out.push(
      <p key={`p${String(out.length)}`}>
        {p.map((l, i) => (
          <Fragment key={i}>
            {i > 0 && <br />}
            {inline(l)}
          </Fragment>
        ))}
      </p>,
    );
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i] ?? '';
    const ph = /^\uE000(\d+)\uE000$/.exec(l);
    if (ph) {
      flush();
      out.push(
        <pre key={`pre${String(out.length)}`}>
          <code>{blocks[Number(ph[1])]}</code>
        </pre>,
      );
      continue;
    }
    if (TABLE_ROW.test(l) && TABLE_SEP.test(lines[i + 1] ?? '')) {
      flush();
      const head = cells(l);
      i += 2;
      const body: string[][] = [];
      while (i < lines.length && TABLE_ROW.test(lines[i] ?? '')) body.push(cells(lines[i++] ?? ''));
      i--;
      out.push(
        <div className="mdt" key={`t${String(out.length)}`}>
          <table>
            <thead>
              <tr>
                {head.map((h, k) => (
                  <th key={k}>{inline(h)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {body.map((r, k) => (
                <tr key={k}>
                  {r.map((c, j) => (
                    <td key={j} className={NUMERIC.test(c) ? 'r' : undefined}>
                      {inline(c)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }
    const h = HEADING.exec(l);
    if (h) {
      flush();
      out.push(<h4 key={`h${String(out.length)}`}>{inline(h[2] ?? '')}</h4>);
      continue;
    }
    if (LIST.test(l)) {
      flush();
      const ordered = /^\s*\d/.test(l);
      const items: string[] = [];
      while (i < lines.length && LIST.test(lines[i] ?? '')) items.push((lines[i++] ?? '').replace(LIST, ''));
      i--;
      const lis = items.map((t, k) => <li key={k}>{inline(t)}</li>);
      out.push(ordered ? <ol key={`o${String(out.length)}`}>{lis}</ol> : <ul key={`u${String(out.length)}`}>{lis}</ul>);
      continue;
    }
    if (!l.trim()) {
      flush();
      continue;
    }
    para.push(l);
  }
  flush();
  return <>{out}</>;
}
