// The raw-lines panel of the import window (legacy v7): the first 18 records of the file as
// decoded, with their status. Clicking a line makes it the header row; "File has no header row"
// and "Automatic" switch the mode. Preamble, skipped and dropped footer lines are struck through.

import { useEffect, useRef } from 'react';

import type { ImportLines, ImportOptions } from '../api/hooks/imports';
import { useImportLines } from '../api/hooks/imports';

type Props = {
  importId: string;
  /** The options of the server's current preview (the lines match what was staged). */
  options: ImportOptions;
  headerAt: number;
  disabled?: boolean;
  onOptions: (o: ImportOptions) => void;
};

const TAG: Partial<Record<ImportLines['lines'][number]['status'], [cls: string, tag: string, sk: boolean]>> = {
  header: ['hdr', 'header', false],
  preamble: ['skip', 'skipped', true],
  skipped: ['skip', 'skipped', true],
  footer_dropped: ['skip', 'summary', true],
};

export function RawLines({ importId, options, headerAt, disabled, onOptions }: Props) {
  const key = JSON.stringify([
    options.encoding,
    options.delimiter,
    options.headerRow,
    options.skipRows,
    options.dropShort,
  ]);
  // The legacy showed 18 lines; a header further down (mBank: record 26) is included and scrolled to.
  const lines = useImportLines(importId, key, Math.min(200, Math.max(18, headerAt + 4)));
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = box.current;
    const hdr = el?.querySelector<HTMLElement>('.rl.hdr');
    if (el && hdr) el.scrollTop = Math.max(0, hdr.offsetTop - el.clientHeight / 2);
  }, [lines.data]);
  const sep = lines.data?.delimiter === '\\t' || lines.data?.delimiter === '\t' ? '  ' : (lines.data?.delimiter ?? ';');
  const pick = (record: number) => {
    if (!disabled) onOptions({ headerRow: record });
  };
  return (
    <div className="imp-sec">
      <div className="lbl">
        Raw file — click a line to use it as the header row
        <span className="spacer" />
        <button
          type="button"
          className="link"
          disabled={disabled}
          onClick={() => {
            onOptions({ headerRow: 'none', skipRows: headerAt || 1 });
          }}
        >
          File has no header row
        </button>
        <button
          type="button"
          className="link"
          disabled={disabled}
          onClick={() => {
            onOptions({ headerRow: 'auto' });
          }}
        >
          Automatic
        </button>
      </div>
      <div ref={box} className="imp-raw" role="listbox" aria-label="Raw file lines">
        {lines.data?.lines.map((l) => {
          const [cls, tag, sk] = TAG[l.status] ?? ['', '', false];
          const text = l.fields.join(sep);
          return (
            <div
              key={l.record}
              className={cls ? `rl ${cls}` : 'rl'}
              role="option"
              aria-selected={l.status === 'header'}
              tabIndex={disabled ? -1 : 0}
              title={l.status === 'header' ? 'Header row' : `Use row ${String(l.record)} as header`}
              onClick={() => {
                pick(l.record);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  pick(l.record);
                }
              }}
            >
              <span className="ln">{l.record}</span>
              <span className="lt">
                {l.status === 'blank' || !text.trim() ? (
                  <span className="muted">(empty)</span>
                ) : text.length > 240 ? (
                  `${text.slice(0, 240)}…`
                ) : (
                  text
                )}
              </span>
              {tag && <span className={sk ? 'tagh sk' : 'tagh'}>{tag}</span>}
            </div>
          );
        })}
      </div>
    </div>
  );
}
