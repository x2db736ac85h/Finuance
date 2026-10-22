// The preview table of the import window (legacy #impBody): the original columns of the staged rows
// — first 50, a ⋯ gap row, last 5 — with each column's meaning under its name. Clicking a column
// name opens the role menu. The last column shows how the row will be imported, or its error.

import { useState } from 'react';

import type { ImportOptions, ImportPreview, Mapping, StagedRow } from '../api/hooks/imports';
import { useImportRows } from '../api/hooks/imports';
import { fmtDay } from '../lib/dates';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { Menu, MenuItem } from '../ui/Menu';
import { applyDetected, assignRole, detectedRole, ROLES, roleLabel, roleOf, type Role } from './roles';

type Props = {
  importId: string;
  preview: ImportPreview;
  mapping: Mapping;
  options: ImportOptions;
  /** Live preview (re-staged on every change) or the rows as imported. */
  live: boolean;
  disabled?: boolean;
  onMapping: (mapping: Mapping, options: ImportOptions) => void;
};

export function PreviewTable({ importId, preview, mapping, options, live, disabled, onMapping }: Props) {
  const [menu, setMenu] = useState<{ col: number; anchor: HTMLElement } | null>(null);
  const [onlyErrors, setOnlyErrors] = useState(false);
  const headers = preview.import.headers;
  const types = preview.import.columnTypes;
  const s = preview.stats;
  const shown = preview.sampleRows.length + preview.tailRows.length;
  const detected = live ? preview.detected : null;
  const canAuto = !!detected && applyDetectedChanges(mapping, options, detected, headers);

  const label = live
    ? `Preview — ${shown < s.rows ? `first ${fmtInt(preview.sampleRows.length)} and last ${fmtInt(preview.tailRows.length)} of ` : ''}${plural(s.rows, 'row')} · click a column name to set what it means`
    : `Rows as imported (${plural(s.rows, 'row')}) · click a column name to change what it means, then reprocess`;

  return (
    <div className="imp-sec grow">
      <div className="lbl">
        {onlyErrors ? `Rows with errors (${fmtInt(s.errors)})` : label}
        <span className="spacer" />
        {s.errors > 0 && (
          <button
            type="button"
            className="link"
            onClick={() => {
              setOnlyErrors(!onlyErrors);
            }}
          >
            {onlyErrors ? 'Show all rows' : `${plural(s.errors, 'row')} with errors`}
          </button>
        )}
        {canAuto && !disabled && (
          <button
            type="button"
            className="link"
            title="Map every column detection recognised to its meaning"
            onClick={() => {
              const r = applyDetected(mapping, options, detected, headers);
              onMapping(r.mapping, r.options);
            }}
          >
            Use detected meanings
          </button>
        )}
      </div>
      {onlyErrors ? (
        <ErrorRows importId={importId} headers={headers} />
      ) : (
        <div className="imp-tw">
          <table className="imp-t">
            <thead>
              <tr>
                <th className="ln">#</th>
                {headers.map((h, j) => {
                  const role = roleOf(mapping, options, j);
                  const auto = detectedRole(detected, j);
                  return (
                    <th key={j} className={role === 'ignore' ? 'ign' : undefined}>
                      <button
                        type="button"
                        className="mc"
                        disabled={disabled}
                        aria-haspopup="menu"
                        title={`${h} (${types[j] ?? 'text'}) — click to set what it means`}
                        onClick={(e) => {
                          setMenu({ col: j, anchor: e.currentTarget });
                        }}
                      >
                        <div className="mc-n">
                          <span className={`tt ${types[j] ?? 'text'}`}>{typeGlyph(types[j])}</span> {h}
                        </div>
                        <div className={role ? 'mc-r on' : 'mc-r'}>
                          {role === 'ignore'
                            ? 'ignored'
                            : role
                              ? `→ ${roleLabel(role)}`
                              : auto
                                ? `auto: ${roleLabel(auto)}`
                                : 'not mapped'}
                        </div>
                      </button>
                    </th>
                  );
                })}
                <th className="res">Imported as</th>
              </tr>
            </thead>
            <tbody>
              {preview.sampleRows.map((r) => (
                <Row key={r.rowIndex} row={r} headers={headers} mapping={mapping} options={options} />
              ))}
              {preview.tailRows.length > 0 && (
                <tr className="gap">
                  <td colSpan={headers.length + 2}>⋯ {plural(s.rows - shown, 'row')} in between ⋯</td>
                </tr>
              )}
              {preview.tailRows.map((r) => (
                <Row key={r.rowIndex} row={r} headers={headers} mapping={mapping} options={options} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {menu && (
        <RoleMenu
          anchor={menu.anchor}
          header={headers[menu.col] ?? ''}
          current={roleOf(mapping, options, menu.col)}
          auto={detectedRole(detected, menu.col)}
          onClose={() => {
            setMenu(null);
          }}
          onPick={(role) => {
            const r = assignRole(mapping, options, { index: menu.col, header: headers[menu.col] ?? '' }, role);
            setMenu(null);
            onMapping(r.mapping, r.options);
          }}
        />
      )}
    </div>
  );
}

/** Whether "Use detected meanings" would change anything. */
function applyDetectedChanges(m: Mapping, o: ImportOptions, detected: Mapping, headers: readonly string[]): boolean {
  const r = applyDetected(m, o, detected, headers);
  return JSON.stringify(r.mapping) !== JSON.stringify(m) || JSON.stringify(r.options) !== JSON.stringify(o);
}

const typeGlyph = (t: string | undefined) => (t === 'number' ? '#' : t === 'date' ? '◷' : 'Aa');

function cellsOf(row: StagedRow, headers: readonly string[]): string[] {
  return headers.map((h, j) => {
    const f = row.raw[j];
    return f && f.header === h ? f.value : (row.raw.find((x) => x.header === h)?.value ?? '');
  });
}

function Row({
  row,
  headers,
  mapping,
  options,
}: {
  row: StagedRow;
  headers: readonly string[];
  mapping: Mapping;
  options: ImportOptions;
}) {
  const cells = cellsOf(row, headers);
  return (
    <tr className={row.error ? 'err' : undefined}>
      <td className="ln">{row.rowIndex + 1}</td>
      {cells.map((v, j) => (
        <td
          key={j}
          className={roleOf(mapping, options, j) === 'ignore' ? 'ign' : undefined}
          title={v.length > 12 ? v : undefined}
        >
          {v}
        </td>
      ))}
      <td className={row.error ? 'res neg' : 'res'} title={row.error}>
        {row.error ? (
          <>
            <Icon name="x" />
            {row.error}
          </>
        ) : (
          <>
            {fmtDay(row.bookedOn)}
            {row.amount && (
              <>
                {' · '}
                <span className={row.amount.amountMinor < 0 ? 'neg' : undefined}>
                  {fmtMoney(row.amount, { currency: true })}
                </span>
              </>
            )}
            {row.duplicate && <span className="tag">duplicate</span>}
          </>
        )}
      </td>
    </tr>
  );
}

function RoleMenu({
  anchor,
  header,
  current,
  auto,
  onClose,
  onPick,
}: {
  anchor: HTMLElement;
  header: string;
  current: Role | null;
  auto: Role | null;
  onClose: () => void;
  onPick: (r: Role | null) => void;
}) {
  return (
    <Menu
      open
      anchor={anchor}
      onClose={onClose}
      label={`Meaning of ${header}`}
      width={270}
      header={
        <div className="pop-h">
          <Icon name="columns" />
          <b>{header}</b>
          <span className="muted">means…</span>
        </div>
      }
    >
      <MenuItem
        on={current === null}
        onSelect={() => {
          onPick(null);
        }}
      >
        Not mapped — keep original name
      </MenuItem>
      {ROLES.map((r) => (
        <MenuItem
          key={r.role}
          on={current === r.role}
          {...(auto === r.role ? { hint: 'auto' } : {})}
          onSelect={() => {
            onPick(r.role);
          }}
        >
          {r.label}
        </MenuItem>
      ))}
    </Menu>
  );
}

function ErrorRows({ importId, headers }: { importId: string; headers: readonly string[] }) {
  const q = useImportRows(importId, true);
  const rows = q.data?.pages.flatMap((p) => p.rows) ?? [];
  return (
    <div className="imp-tw">
      <table className="imp-t">
        <thead>
          <tr>
            <th className="ln">#</th>
            <th className="res">Error</th>
            {headers.map((h, j) => (
              <th key={j}>
                <div className="mc-n">{h}</div>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.rowIndex} className="err">
              <td className="ln">{r.rowIndex + 1}</td>
              <td className="res neg">{r.error}</td>
              {cellsOf(r, headers).map((v, j) => (
                <td key={j}>{v}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {q.hasNextPage && (
        <div className="imp-more">
          <Button size="sm" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            Show more
          </Button>
        </div>
      )}
    </div>
  );
}
