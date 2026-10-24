// Settings → Categories (PLAN task 15.1): every category with its transaction counts, new
// category, rename, merge into another one, delete (transactions become uncategorised). Changes
// keep working on later imports: the server remembers which bank names belong where.

import { useState } from 'react';

import {
  useCategoryStats,
  useCreateCategory,
  useDeleteCategory,
  useMergeCategory,
  useRenameCategory,
  type CategoryStat,
} from '../api/hooks/rules';
import { errorMessage } from '../lib/errors';
import { fmtInt, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { TextInput, Select } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

type Dialog = { kind: 'rename' | 'merge' | 'delete'; cat: CategoryStat } | null;

export function CategoriesSettings() {
  const stats = useCategoryStats();
  const create = useCreateCategory();
  const [name, setName] = useState('');
  const [dialog, setDialog] = useState<Dialog>(null);
  const cats = stats.data ?? [];
  const add = () => {
    const n = name.trim();
    if (!n) return;
    create.mutate(n, {
      onSuccess: () => {
        setName('');
        toast(`Category “${n}” created`);
      },
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  return (
    <>
      <div className="set-card">
        <div className="lbl">New category</div>
        <form
          className="set-row"
          onSubmit={(e) => {
            e.preventDefault();
            add();
          }}
        >
          <TextInput
            aria-label="New category name"
            placeholder="e.g. Groceries"
            maxLength={80}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />
          <Button type="submit" variant="primary" disabled={!name.trim() || create.isPending}>
            Create
          </Button>
        </form>
      </div>
      <div className="set-card">
        <div className="lbl">Categories</div>
        <p className="hint">
          Bank categories become categories when you import. Renaming, merging and deleting also apply to future imports
          of the same bank category.
        </p>
        {stats.isError && <p className="set-err">{errorMessage(stats.error)}</p>}
        {stats.isSuccess && !cats.length && <p className="hint">No categories yet.</p>}
        <ul className="set-list" aria-label="Categories">
          {cats.map((c) => (
            <li key={c.id}>
              <div className="set-li-main">
                <b>{c.name}</b>
                <span className="hint">
                  {plural(c.txCount, 'transaction')}
                  {c.manualCount > 0 && ` · ${fmtInt(c.manualCount)} set by hand`}
                  {c.ruleCount > 0 && ` · ${fmtInt(c.ruleCount)} by rules`}
                  {c.aliases.length > 0 && ` · also gets the bank's “${c.aliases.join('”, “')}”`}
                </span>
              </div>
              <Button
                size="sm"
                aria-label={`Rename ${c.name}`}
                onClick={() => {
                  setDialog({ kind: 'rename', cat: c });
                }}
              >
                Rename
              </Button>
              <Button
                size="sm"
                aria-label={`Merge ${c.name}`}
                disabled={cats.length < 2}
                onClick={() => {
                  setDialog({ kind: 'merge', cat: c });
                }}
              >
                Merge…
              </Button>
              <Button
                size="sm"
                variant="ghost"
                aria-label={`Delete ${c.name}`}
                onClick={() => {
                  setDialog({ kind: 'delete', cat: c });
                }}
              >
                Delete
              </Button>
            </li>
          ))}
        </ul>
      </div>
      {dialog?.kind === 'rename' && (
        <RenameDialog
          cat={dialog.cat}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
      {dialog?.kind === 'merge' && (
        <MergeDialog
          cat={dialog.cat}
          others={cats.filter((c) => c.id !== dialog.cat.id)}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
      {dialog?.kind === 'delete' && (
        <DeleteDialog
          cat={dialog.cat}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
    </>
  );
}

function RenameDialog({ cat, onClose }: { cat: CategoryStat; onClose: () => void }) {
  const rename = useRenameCategory();
  const [name, setName] = useState(cat.name);
  return (
    <Modal open onClose={onClose} title="Rename category" size="sm">
      <form
        className="modal-form"
        onSubmit={(e) => {
          e.preventDefault();
          rename.mutate(
            { id: cat.id, name: name.trim() },
            {
              onSuccess: (c) => {
                toast(`Renamed to “${c.name}”`);
                onClose();
              },
              onError: (err) => {
                toast(errorMessage(err), { kind: 'error' });
              },
            },
          );
        }}
      >
        <TextInput
          aria-label="Category name"
          maxLength={80}
          value={name}
          autoFocus
          onChange={(e) => {
            setName(e.target.value);
          }}
        />
        <p className="hint">The bank&apos;s “{cat.name}” keeps coming to this category on later imports.</p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            variant="primary"
            disabled={!name.trim() || name.trim() === cat.name || rename.isPending}
          >
            Rename
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function MergeDialog({ cat, others, onClose }: { cat: CategoryStat; others: CategoryStat[]; onClose: () => void }) {
  const merge = useMergeCategory();
  const [into, setInto] = useState(others[0]?.id ?? '');
  const target = others.find((o) => o.id === into);
  return (
    <Modal open onClose={onClose} title="Merge category" size="sm">
      <div className="modal-form">
        <p>
          Move every transaction of <b>{cat.name}</b> ({plural(cat.txCount, 'transaction')}), its rules and its budget
          into:
        </p>
        <Select
          aria-label="Merge into"
          value={into}
          options={others.map((o) => ({ value: o.id, label: o.name }))}
          onChange={setInto}
        />
        <p className="hint">
          “{cat.name}” is then deleted; the bank&apos;s “{cat.name}” goes to {target ? `“${target.name}”` : 'it'} on
          later imports.
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!into || merge.isPending}
            onClick={() => {
              merge.mutate(
                { id: cat.id, intoId: into },
                {
                  onSuccess: (r) => {
                    toast(
                      `Merged “${cat.name}” into “${target?.name ?? ''}” · ${plural(r.moved, 'transaction')} moved`,
                    );
                    onClose();
                  },
                  onError: (err) => {
                    toast(errorMessage(err), { kind: 'error' });
                  },
                },
              );
            }}
          >
            Merge
          </Button>
        </div>
      </div>
    </Modal>
  );
}

function DeleteDialog({ cat, onClose }: { cat: CategoryStat; onClose: () => void }) {
  const del = useDeleteCategory();
  return (
    <Modal open onClose={onClose} title="Delete category" size="sm">
      <div className="modal-form">
        <p>
          Delete <b>{cat.name}</b>? {plural(cat.txCount, 'transaction')} become uncategorised, its budget goes and rules
          that set it stop setting a category. No transactions are deleted.
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => {
              del.mutate(cat.id, {
                onSuccess: () => {
                  toast(`Category “${cat.name}” deleted`);
                  onClose();
                },
                onError: (err) => {
                  toast(errorMessage(err), { kind: 'error' });
                },
              });
            }}
          >
            Delete category
          </Button>
        </div>
      </div>
    </Modal>
  );
}
