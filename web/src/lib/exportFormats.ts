// The table's export in other shapes (Phase 27). The server streams one format, CSV (ADR 0014:
// UTF-8 with BOM, `;`, CRLF, amounts as text like "-79,99"); the two others are made from it here,
// so all three hold exactly the same rows, columns and values.

export type ExportFormat = 'csv' | 'txt' | 'json';

export const EXPORT_FORMATS: readonly { id: ExportFormat; label: string; about: string }[] = [
  { id: 'csv', label: 'CSV', about: 'for spreadsheets' },
  { id: 'txt', label: 'TXT', about: 'an aligned table to read' },
  { id: 'json', label: 'JSON', about: 'for scripts' },
];

/** Reads delimited text with quoted fields ("a;b", "" for a quote, line breaks inside quotes). */
export function parseDelimited(text: string, delimiter = ';'): string[][] {
  const s = text.startsWith('﻿') ? text.slice(1) : text;
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < s.length; i++) {
    const ch = s.charAt(i);
    if (quoted) {
      if (ch === '"') {
        if (s.charAt(i + 1) === '"') {
          field += '"';
          i++;
        } else quoted = false;
      } else field += ch;
    } else if (ch === '"' && field === '') quoted = true;
    else if (ch === delimiter) {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && s.charAt(i + 1) === '\n') i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += ch;
  }
  // a last line without a line break; a file that ends with one adds no empty row
  if (field !== '' || row.length) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

/** An array of objects keyed by the header row; values stay the text the CSV has. */
export function toJson(rows: readonly string[][]): string {
  const [head = [], ...body] = rows;
  const out = body.map((r) => Object.fromEntries(head.map((h, i) => [h, r[i] ?? ''])));
  return JSON.stringify(out, null, 2) + '\n';
}

/** How many characters a cell takes in a fixed-width font (code points, so an accented letter is one). */
const len = (v: string): number => Array.from(v).length;

/** Columns padded to their widest value, a rule under the header; line breaks in a cell become spaces. */
export function toText(rows: readonly string[][]): string {
  const clean = rows.map((r) => r.map((c) => c.replace(/\s*[\r\n]+\s*/g, ' ')));
  const cols = Math.max(0, ...clean.map((r) => r.length));
  const width = Array.from({ length: cols }, (_, i) => Math.max(0, ...clean.map((r) => len(r[i] ?? ''))));
  const line = (r: readonly string[]) =>
    width
      .map((w, i) => {
        const v = r[i] ?? '';
        return v + ' '.repeat(w - len(v));
      })
      .join('  ')
      .trimEnd();
  const [head, ...body] = clean;
  if (!head) return '';
  return [line(head), width.map((w) => '-'.repeat(w)).join('  '), ...body.map(line)].join('\n') + '\n';
}

/** The exported CSV as the format asked for, with the file name's ending changed to match. */
export async function convertExport(
  blob: Blob,
  filename: string,
  format: ExportFormat,
): Promise<{ blob: Blob; filename: string }> {
  if (format === 'csv') return { blob, filename };
  const rows = parseDelimited(await blob.text());
  const name = filename.replace(/\.csv$/i, '') + '.' + format;
  return format === 'json'
    ? { blob: new Blob([toJson(rows)], { type: 'application/json' }), filename: name }
    : { blob: new Blob([toText(rows)], { type: 'text/plain;charset=utf-8' }), filename: name };
}
