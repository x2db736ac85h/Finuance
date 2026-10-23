// The rule window (PLAN task 15.2): a name, what the rule matches (a search and column filters in
// the table's filter language) and what it does (set a category, mark as transfer), with a live
// preview of the transactions it matches now. Used by Settings → Rules and by the toolbar's
// "Save filters as rule…".

import { useState } from 'react';

import type { Schemas } from '../api/client';
import { useCategories } from '../api/hooks/transactions';
import {
  matchesSomething,
  useCreateRule,
  useRulePreview,
  useUpdateRule,
  type Rule,
  type RuleMatch,
} from '../api/hooks/rules';
import { useDebounced } from '../lib/useDebounced';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { COLUMNS } from '../table/columns';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { Checkbox, Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

type Filter = { column: Schemas['FilterColumn']; cond: string; excluded: string[] };

export type RuleDraft = {
  name: string;
  enabled: boolean;
  search: string;
  filters: Filter[];
  setCategoryId: string;
  markTransfer: boolean;
};

export const emptyDraft = (): RuleDraft => ({
  name: '',
  enabled: true,
  search: '',
  filters: [{ column: 'payee', cond: '', excluded: [] }],
  setCategoryId: '',
  markTransfer: false,
});

export function draftOf(r: Rule): RuleDraft {
  return {
    name: r.name,
    enabled: r.enabled,
    search: r.match.search ?? '',
    filters: (r.match.filters ?? []).map((f) => ({ column: f.column, cond: f.cond ?? '', excluded: f.excluded ?? [] })),
    setCategoryId: r.setCategory?.id ?? '',
    markTransfer: r.markTransfer,
  };
}

export function matchOf(d: Pick<RuleDraft, 'search' | 'filters'>): RuleMatch {
  const filters = d.filters
    .filter((f) => f.cond.trim() || f.excluded.length)
    .map((f) => ({
      column: f.column,
      ...(f.cond.trim() ? { cond: f.cond.trim() } : {}),
      ...(f.excluded.length ? { excluded: f.excluded } : {}),
    }));
  return { ...(d.search.trim() ? { search: d.search.trim() } : {}), ...(filters.length ? { filters } : {}) };
}

const labelOf = (id: string) => COLUMNS.find((c) => c.id === id)?.label ?? id;

/** One line for a rule's match and actions: "Payee kawiarnia · search “blik” → Kawa, transfer". */
export function ruleSummary(r: Pick<Rule, 'match' | 'setCategory' | 'markTransfer'>): string {
  const parts: string[] = [];
  if (r.match.search) parts.push(`search “${r.match.search}”`);
  for (const f of r.match.filters ?? []) {
    if (f.cond) parts.push(`${labelOf(f.column)} ${f.cond}`);
    if (f.excluded?.length) parts.push(`${labelOf(f.column)} −${plural(f.excluded.length, 'value')}`);
  }
  const acts = [r.setCategory ? r.setCategory.name : null, r.markTransfer ? 'transfer' : null].filter(Boolean);
  return `${parts.join(' · ') || 'nothing'} → ${acts.join(', ') || 'no action'}`;
}

type Props = { title: string; initial: RuleDraft; rule?: Rule; onClose: () => void };

export function RuleEditor({ title, initial, rule, onClose }: Props) {
  const [d, setD] = useState(initial);
  const cats = useCategories();
  const create = useCreateRule();
  const update = useUpdateRule();
  const match = matchOf(d);
  const preview = useRulePreview(useDebounced(match, 300));
  const set = (change: Partial<RuleDraft>) => {
    setD((x) => ({ ...x, ...change }));
  };
  const setFilter = (i: number, change: Partial<Filter>) => {
    set({ filters: d.filters.map((f, j) => (j === i ? { ...f, ...change } : f)) });
  };
  const hasMatch = matchesSomething(match);
  const hasAction = !!d.setCategoryId || d.markTransfer;
  const busy = create.isPending || update.isPending;
  const save = () => {
    const body = {
      name: d.name.trim(),
      enabled: d.enabled,
      match,
      ...(d.setCategoryId ? { setCategoryId: d.setCategoryId } : {}),
      markTransfer: d.markTransfer,
    };
    const done = {
      onSuccess: () => {
        toast(rule ? `Rule “${body.name}” saved · applying…` : `Rule “${body.name}” created · applying…`);
        onClose();
      },
      onError: (e: unknown) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    };
    if (rule) update.mutate({ id: rule.id, ...body }, done);
    else create.mutate(body, done);
  };
  const p = preview.data;
  return (
    <Modal open onClose={onClose} title={title} size="md">
      <form
        className="modal-form rule-form"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <label className="rule-fld">
          <span>Name</span>
          <TextInput
            aria-label="Rule name"
            maxLength={80}
            value={d.name}
            placeholder="e.g. Coffee shops"
            onChange={(e) => {
              set({ name: e.target.value });
            }}
          />
        </label>
        <fieldset className="rule-fld">
          <legend>Matches transactions where</legend>
          <TextInput
            aria-label="Search all columns"
            placeholder="all of these words appear anywhere (optional)"
            value={d.search}
            onChange={(e) => {
              set({ search: e.target.value });
            }}
          />
          {d.filters.map((f, i) => (
            <div className="rule-filter" key={i}>
              <Select
                aria-label={`Column of condition ${String(i + 1)}`}
                value={f.column}
                options={COLUMNS.map((c) => ({ value: c.id, label: c.label }))}
                onChange={(column) => {
                  setFilter(i, { column });
                }}
              />
              <TextInput
                mono
                aria-label={`Condition ${String(i + 1)}`}
                placeholder="kawiarnia, =ZABKA, <-100, 2025-03 …"
                value={f.cond}
                onChange={(e) => {
                  setFilter(i, { cond: e.target.value });
                }}
              />
              {f.excluded.length > 0 && <span className="hint">−{plural(f.excluded.length, 'value')}</span>}
              <IconButton
                icon="x"
                size="sm"
                title={`Remove condition ${String(i + 1)}`}
                onClick={() => {
                  set({ filters: d.filters.filter((_, j) => j !== i) });
                }}
              />
            </div>
          ))}
          <Button
            size="sm"
            icon="plus"
            disabled={d.filters.length >= 20}
            onClick={() => {
              set({ filters: [...d.filters, { column: 'payee', cond: '', excluded: [] }] });
            }}
          >
            Add condition
          </Button>
        </fieldset>
        <fieldset className="rule-fld">
          <legend>Then</legend>
          <label className="rule-act">
            <span>Set category</span>
            <Select
              aria-label="Category to set"
              value={d.setCategoryId}
              options={[
                { value: '', label: '— keep the category —' },
                ...(cats.data ?? []).map((c) => ({ value: c.id, label: c.name })),
              ]}
              onChange={(setCategoryId) => {
                set({ setCategoryId });
              }}
            />
          </label>
          <Checkbox
            label="Mark as transfer (left out of charts and the overview)"
            checked={d.markTransfer}
            onChange={(markTransfer) => {
              set({ markTransfer });
            }}
          />
          <Checkbox
            label="Enabled"
            checked={d.enabled}
            onChange={(enabled) => {
              set({ enabled });
            }}
          />
        </fieldset>
        <section className="rule-preview" aria-label="Preview" aria-live="polite">
          {!hasMatch && <p className="hint">Add a search or a condition to see what the rule matches.</p>}
          {hasMatch && preview.isError && <p className="set-err">{errorMessage(preview.error)}</p>}
          {hasMatch && p && (
            <>
              <p>
                Matches <b>{plural(p.matched, 'transaction')}</b>
                {p.manual > 0 &&
                  d.setCategoryId &&
                  ` · ${fmtInt(p.manual)} with a category set by hand stay as they are`}
              </p>
              {p.sample.length > 0 && (
                <table className="rule-sample">
                  <tbody>
                    {p.sample.map((r) => (
                      <tr key={r.id}>
                        <td>{fmtDay(r.bookedOn)}</td>
                        <td className="tx">{r.payee || r.description}</td>
                        <td className={r.amount.amountMinor < 0 ? 'num neg' : 'num'}>{fmtMoney(r.amount)}</td>
                        <td className="muted">{r.category?.name ?? '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          )}
        </section>
        <p className="hint">
          Rules apply in order to every account, all time; a later rule wins. Manual categories are never changed.
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!d.name.trim() || !hasMatch || !hasAction || busy}>
            {rule ? 'Save rule' : 'Create rule'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
