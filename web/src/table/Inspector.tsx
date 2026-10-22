// The inspector (PLAN §7.6, legacy renderInspector): every field of the selected transaction with
// type badges and per-field copy, the group pills, the category picker (pick, create by name, or back to the bank's
// category) and the note, the original row with the bank's headers, the imports it came from, Copy
// as JSON, and previous/next. Below 1100 px it overlays the table (CSS, as in the legacy app).

import { useId, useState } from 'react';
import { Link } from 'react-router';

import {
  useCategories,
  usePatchTransaction,
  useTransaction,
  type TransactionDetail,
  type TransactionPatch,
} from '../api/hooks/transactions';
import { useAssignGroup, useGroups, type Group } from '../api/hooks/groups';
import { GroupDots } from '../groups/GroupDots';
import { GroupForm } from '../groups/GroupForm';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt } from '../lib/format';
import { norm, searchTokens } from '../lib/text';
import { useViews, viewOf } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { toast } from '../ui/toastStore';
import { COLUMNS, type ColumnDef } from './columns';
import { useQueryContext } from './context';
import { escapeSelection, moveSelection } from './controller';
import { useHead } from './data';
import { copyText } from './Grid';
import { hl } from './highlight';

const SOURCE_LABEL: Record<TransactionDetail['categorySource'], string> = {
  none: '',
  bank: 'from the bank',
  rule: 'by a rule',
  manual: 'set by you',
};

export function Inspector({ viewId }: { viewId: string }) {
  const v = useViews((s) => viewOf(s.views, viewId));
  const q = useQueryContext(viewId);
  const total = useHead(q, v.statColumn).data?.total;
  const tx = useTransaction(v.selectedId);
  if (!v.selectedId) return null;
  const d = tx.data?.id === v.selectedId ? tx.data : undefined;
  const tokens = searchTokens(v.search);

  return (
    <aside className="inspector" aria-label="Transaction details">
      <div className="ins-h">
        <span className="ins-t">Transaction</span>
        <span className="muted">
          {v.selectedIndex !== null && total !== undefined ? `${fmtInt(v.selectedIndex + 1)} / ${fmtInt(total)}` : ''}
        </span>
        <span className="spacer" />
        <IconButton icon="up" title="Previous row (↑)" onClick={() => moveSelection(-1)} />
        <IconButton icon="down" title="Next row (↓)" onClick={() => moveSelection(1)} />
        <IconButton icon="x" title="Close (Esc)" onClick={() => escapeSelection()} />
      </div>
      {d && <GroupRow d={d} />}
      <div className="ins-b">
        {d ? (
          <Details d={d} tokens={tokens} />
        ) : tx.isError ? (
          <div className="fld">
            <span className="nil">Couldn't load this transaction. Select it again to retry.</span>
          </div>
        ) : (
          <div className="fld">
            <span className="nil">Loading…</span>
          </div>
        )}
      </div>
      <div className="ins-f">
        <Button
          icon="copy"
          disabled={!d}
          onClick={() => {
            if (d) void copyText(JSON.stringify(asJson(d), null, 2), 'Copied the transaction as JSON');
          }}
        >
          Copy as JSON
        </Button>
      </div>
    </aside>
  );
}

/** Copy as JSON (legacy "Copy row as JSON"): the fields by their names, then the original row. */
export function asJson(d: TransactionDetail): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const c of COLUMNS) {
    const text = c.text(d);
    if (text) out[c.label] = text;
  }
  if (d.raw.length) out['Original row'] = Object.fromEntries(d.raw.map((f) => [f.header, f.value]));
  return out;
}

function Details({ d, tokens }: { d: TransactionDetail; tokens: readonly string[] }) {
  const patch = usePatchTransaction(d.id);
  const save = (body: TransactionPatch, done: string) => {
    patch.mutate(body, {
      onSuccess: () => {
        toast(done);
      },
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  // The group has its own row above; Balance after only when the transaction has one.
  const fields = COLUMNS.filter((c) => c.id !== 'group' && (c.id !== 'balanceAfter' || d.balanceAfter));
  return (
    <>
      {fields.map((c) => {
        if (c.id === 'category') {
          return (
            <Field key={c.id} col={c} text={c.text(d)} hint={d.category ? SOURCE_LABEL[d.categorySource] : ''}>
              <CategoryPicker key={`${d.id}:${d.category?.id ?? ''}`} d={d} busy={patch.isPending} save={save} />
            </Field>
          );
        }
        if (c.id === 'note') {
          return (
            <Field key={c.id} col={c} text={d.note}>
              <NoteEditor key={`${d.id}:${d.note}`} d={d} save={save} />
            </Field>
          );
        }
        const text = c.text(d);
        const minor =
          c.id === 'amount' ? d.amount.amountMinor : c.id === 'balanceAfter' ? d.balanceAfter?.amountMinor : 0;
        const sign = (minor ?? 0) < 0 ? ' neg' : (minor ?? 0) > 0 ? ' pos' : '';
        return (
          <Field key={c.id} col={c} text={text}>
            <div className={`fv ${c.type}${sign}`}>{text ? hl(text, tokens) : <span className="nil">empty</span>}</div>
          </Field>
        );
      })}
      {d.raw.length > 0 && (
        <div className="fld">
          <div className="fk">
            <Icon name="file" />
            Original row
          </div>
          <dl className="ins-raw">
            {d.raw.map((f, i) => (
              <div key={i}>
                <dt>{f.header || `Column ${String(i + 1)}`}</dt>
                <dd>{f.value ? hl(f.value, tokens) : <span className="nil">empty</span>}</dd>
              </div>
            ))}
          </dl>
        </div>
      )}
      <div className="fld">
        <div className="fk">
          <Icon name="upload" />
          {d.sources.length > 1 ? `Sources (${String(d.sources.length)} imports)` : 'Source'}
        </div>
        {d.sources.length === 0 ? (
          <div className="fv">
            <span className="nil">{d.origin === 'manual' ? 'Added by hand' : 'No import'}</span>
          </div>
        ) : (
          <ul className="ins-src">
            {d.sources.map((s) => (
              <li key={`${s.importId}:${String(s.rowIndex)}`}>
                <Link to={`/imports/${s.importId}`} title="Open the import">
                  {s.filename}
                </Link>
                <span className="muted">
                  row {fmtInt(s.rowIndex + 1)}
                  {s.committedAt && ` · ${fmtDateTime(s.committedAt)}`}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

/** The group pills (legacy inspector "Group" row): pick one, click the current one to remove it. */
function GroupRow({ d }: { d: TransactionDetail }) {
  const groups = useGroups().data ?? [];
  const assign = useAssignGroup();
  const [form, setForm] = useState<HTMLElement | null>(null);
  const set = (g: Group | null) => {
    assign.mutate(
      { ids: [d.id], groupId: g?.id ?? null },
      {
        onSuccess: () => {
          toast(g ? `Added to “${g.name}”` : 'Removed from its group');
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  return (
    <div className="ins-grp">
      <div className="fk">
        <Icon name="tag" />
        Group
      </div>
      <div className="gdots" role="group" aria-label="Group">
        <GroupDots
          groups={groups}
          current={d.group?.id}
          disabled={assign.isPending}
          onPick={(g) => {
            set(g.id === d.group?.id ? null : g);
          }}
          onNew={setForm}
        />
      </div>
      {form && (
        <GroupForm
          anchor={form}
          onClose={() => {
            setForm(null);
          }}
          onDone={set}
        />
      )}
    </div>
  );
}

function Field({
  col,
  text,
  hint,
  children,
}: {
  col: ColumnDef;
  text: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="fld">
      <div className="fk">
        <span className={`tt ${col.type}`}>
          {col.id === 'group' ? (
            <Icon name="tag" />
          ) : col.type === 'number' ? (
            '#'
          ) : col.type === 'date' ? (
            <Icon name="cal" />
          ) : (
            'Aa'
          )}
        </span>
        {col.label}
        {hint && <span className="ins-hint">{hint}</span>}
        <IconButton
          icon="copy"
          size="sm"
          title={`Copy ${col.label}`}
          onClick={() => {
            void copyText(text);
          }}
        />
      </div>
      {children}
    </div>
  );
}

type Choice = { kind: 'pick'; id: string; name: string } | { kind: 'create'; name: string } | { kind: 'reset' };

/**
 * The category combobox: type to filter the user's categories, Enter or click picks; a name nobody
 * has yet offers "Create …"; a category set by hand offers "Use the bank's category". Esc or leaving
 * the field drops the text.
 */
function CategoryPicker({
  d,
  busy,
  save,
}: {
  d: TransactionDetail;
  busy: boolean;
  save: (body: TransactionPatch, done: string) => void;
}) {
  const current = d.category?.name ?? '';
  const [text, setText] = useState(current);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const cats = useCategories();
  const listId = useId();
  const typed = text.trim();
  const needle = norm(typed);
  const changed = typed !== current;
  const matches = (cats.data ?? []).filter((c) => !changed || !needle || norm(c.name).includes(needle)).slice(0, 50);
  const choices: Choice[] = matches.map((c) => ({ kind: 'pick', id: c.id, name: c.name }));
  if (typed && !matches.some((c) => norm(c.name) === needle)) choices.push({ kind: 'create', name: typed });
  if (d.categorySource === 'manual') choices.push({ kind: 'reset' });
  const at = Math.min(active, choices.length - 1);

  const choose = (c: Choice) => {
    setOpen(false);
    if (c.kind === 'reset') save({ resetCategory: true }, "Category set back to the bank's");
    else if (c.kind === 'create') save({ categoryName: c.name }, `Category “${c.name}” created and set`);
    else if (c.id !== d.category?.id) save({ categoryId: c.id }, `Category set to “${c.name}”`);
    else setText(current);
  };
  const close = () => {
    setOpen(false);
    setText(current);
  };

  return (
    <div className="combo">
      <input
        className="inp"
        role="combobox"
        aria-label="Category"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && choices.length ? `${listId}-${String(at)}` : undefined}
        placeholder="No category"
        maxLength={80}
        value={text}
        disabled={busy}
        spellCheck={false}
        autoComplete="off"
        onFocus={(e) => {
          e.currentTarget.select();
          setOpen(true);
        }}
        onBlur={close}
        onChange={(e) => {
          setText(e.target.value);
          setActive(0);
          setOpen(true);
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault();
            setOpen(true);
            const n = choices.length || 1;
            setActive((at + (e.key === 'ArrowDown' ? 1 : n - 1)) % n);
          } else if (e.key === 'Enter') {
            e.preventDefault();
            const c = choices[at];
            if (open && c) choose(c);
            e.currentTarget.blur();
          } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            close();
            e.currentTarget.blur();
          }
        }}
      />
      {open && choices.length > 0 && (
        <ul className="combo-list" role="listbox" id={listId} aria-label="Categories">
          {choices.map((c, i) => (
            <li
              key={c.kind === 'pick' ? c.id : c.kind}
              id={`${listId}-${String(i)}`}
              role="option"
              aria-selected={i === at}
              className={i === at ? 'on' : undefined}
              onMouseDown={(e) => {
                e.preventDefault(); // keep focus until the choice is made
              }}
              onMouseEnter={() => {
                setActive(i);
              }}
              onClick={() => {
                choose(c);
              }}
            >
              {c.kind === 'pick' && c.name}
              {c.kind === 'create' && (
                <>
                  <Icon name="plus" />
                  Create “{c.name}”
                </>
              )}
              {c.kind === 'reset' && (
                <>
                  <Icon name="left" />
                  Use the bank's category{d.bankCategory ? ` (${d.bankCategory})` : ''}
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** The note: saved when the field is left or with ⌘/Ctrl Enter; Esc drops the change. */
function NoteEditor({ d, save }: { d: TransactionDetail; save: (body: TransactionPatch, done: string) => void }) {
  const [text, setText] = useState(d.note);
  const commit = () => {
    if (text !== d.note) save({ note: text }, text ? 'Note saved' : 'Note removed');
  };
  return (
    <textarea
      className="inp ins-note"
      aria-label="Note"
      placeholder="Add a note…"
      rows={2}
      maxLength={2000}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          e.currentTarget.blur();
        } else if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          setText(d.note);
          // blur after the reset so the unchanged text isn't saved
          const el = e.currentTarget;
          setTimeout(() => {
            el.blur();
          });
        }
      }}
    />
  );
}
