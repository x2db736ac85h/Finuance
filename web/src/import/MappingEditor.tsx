// "Column meanings" (PLAN §7.10 mapping editor): one row per role with a column picker, the
// column's type, a detected/profile badge and a required marker. It edits the same mapping as the
// role menu on the preview table; field errors from the server show under their row.

import type { FieldError } from '../api/client';
import type { ImportOptions, ImportPreview, Mapping } from '../api/hooks/imports';
import { Select } from '../ui/inputs';
import { assignRole, ROLES, type Role } from './roles';

type Props = {
  preview: ImportPreview;
  mapping: Mapping;
  options: ImportOptions;
  live: boolean;
  disabled?: boolean;
  errors: readonly FieldError[];
  onMapping: (mapping: Mapping, options: ImportOptions) => void;
};

const REQUIRED: Partial<Record<Role, true>> = { bookedOn: true, amount: true, debit: true, credit: true };

/** The column a role points at, or undefined. */
function columnOf(m: Mapping, o: ImportOptions, role: Role): number | undefined {
  if (role === 'debit') return o.debitColumn?.index;
  if (role === 'credit') return o.creditColumn?.index;
  if (role === 'ignore') return undefined;
  return m[role]?.index;
}

export function MappingEditor({ preview, mapping, options, live, disabled, errors, onMapping }: Props) {
  const headers = preview.import.headers;
  const types = preview.import.columnTypes;
  const debitCredit = options.amountMode === 'debit_credit';
  const roles = ROLES.filter(
    (r) => r.role !== 'ignore' && (debitCredit ? r.role !== 'amount' : r.role !== 'debit' && r.role !== 'credit'),
  );
  const columns = [
    { value: '', label: '— not mapped —' },
    ...headers.map((h, j) => ({ value: String(j), label: `${h} · ${types[j] ?? 'text'}` })),
  ];
  return (
    <div className="map-ed">
      {roles.map(({ role, label }) => {
        const col = columnOf(mapping, options, role);
        const detectedCol =
          role === 'debit' || role === 'credit'
            ? undefined
            : preview.detected[role as Exclude<Role, 'debit' | 'credit' | 'ignore'>]?.index;
        const badge =
          col === undefined
            ? null
            : preview.import.mappingSource === 'profile'
              ? 'profile'
              : live && col === detectedCol
                ? 'detected'
                : null;
        const field =
          role === 'debit' ? 'options.debitColumn' : role === 'credit' ? 'options.creditColumn' : `mapping.${role}`;
        const err = errors.find((e) => e.field === field || (REQUIRED[role] && e.field.startsWith(field)));
        const missing = REQUIRED[role] && col === undefined;
        return (
          <label key={role} className={missing ? 'map-r miss' : 'map-r'}>
            <span className="map-l">
              {label}
              {REQUIRED[role] && (
                <b className="req" title="Required">
                  *
                </b>
              )}
            </span>
            <Select
              value={col === undefined ? '' : String(col)}
              options={columns}
              disabled={disabled}
              aria-label={label}
              onChange={(v) => {
                if (v === '') {
                  if (col === undefined) return;
                  const r = assignRole(mapping, options, { index: col, header: headers[col] ?? '' }, null);
                  onMapping(r.mapping, r.options);
                } else {
                  const j = Number(v);
                  const r = assignRole(mapping, options, { index: j, header: headers[j] ?? '' }, role);
                  onMapping(r.mapping, r.options);
                }
              }}
            />
            {badge && <span className="tag src">{badge}</span>}
            {err && <span className="map-err">{err.message}</span>}
          </label>
        );
      })}
    </div>
  );
}
