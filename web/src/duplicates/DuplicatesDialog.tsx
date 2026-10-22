// The duplicates window (PLAN §7.14, legacy #dupDlg / renderDups): every group of the view's
// accounts, newest first and 60 at a time, with a text filter. A group is a small table of all its
// versions — the import, the key fields and every field that differs (highlighted) — with ✓ on the
// version the data shows. Use this version prefers that import; Keep both / Merge again switch
// merging off and on. Each decision rebuilds the transactions before the window updates.

import { useEffect, useState } from 'react';

import { useAccounts } from '../api/hooks/accounts';
import {
  useDecideDuplicate,
  useDuplicates,
  useResetDuplicate,
  type DuplicateFields,
  type DuplicateGroup,
} from '../api/hooks/duplicates';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { columnById, type ColumnId } from '../table/columns';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { useDupDialog } from './store';

type Field = DuplicateGroup['differingFields'][number];

const KEY_FIELDS: readonly Field[] = ['bookedOn', 'amount', 'payee', 'description'];

/** A field as shown in the window (and compared for the highlight). */
export function fieldText(f: DuplicateFields, field: Field): string {
  switch (field) {
    case 'bookedOn':
    case 'valueOn':
      return fmtDay(f[field]);
    case 'amount':
    case 'balanceAfter': {
      const m = f[field];
      return m ? fmtMoney(m) : '';
    }
    default:
      return f[field];
  }
}

export function DuplicatesDialog() {
  const target = useDupDialog((s) => s.target);
  const close = useDupDialog((s) => s.close);
  return (
    <Modal
      open={target !== null}
      onClose={close}
      title="Duplicates"
      sub={target && `${plural(target.skipped, 'row')} skipped`}
    >
      {target && <Body accountIds={target.accountIds} />}
    </Modal>
  );
}

function Body({ accountIds }: { accountIds: string[] | undefined }) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  useEffect(() => {
    const t = setTimeout(() => {
      setQ(text.trim());
    }, 200);
    return () => {
      clearTimeout(t);
    };
  }, [text]);
  const list = useDuplicates(accountIds, q);
  const accounts = useAccounts().data ?? [];
  const groups = list.data?.pages.flatMap((p) => p.groups) ?? [];
  const total = list.data?.pages[0]?.total ?? 0;
  const many = (accountIds?.length ?? accounts.length) > 1;
  const nameOf = (id: string) => accounts.find((a) => a.id === id)?.name ?? '';

  return (
    <>
      <div className="dup-top">
        <div className="hint">
          When exports of an account overlap, a transaction appears in several imports. Your data keeps it once — the
          version marked ✓. Choose <b>Use this version</b> to show another import's copy, or <b>Keep both</b> if they
          are really different transactions. Differences are highlighted. Duplicates are found per account.
        </div>
        <TextInput
          aria-label="Filter duplicates"
          placeholder="Filter by payee, title, amount…"
          autoComplete="off"
          spellCheck={false}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
      </div>
      <div className="dup-b" aria-busy={list.isFetching}>
        {list.isError ? (
          <div className="viz-empty">{errorMessage(list.error)}</div>
        ) : list.data && !groups.length ? (
          <div className="viz-empty">
            {q ? 'No duplicates match the filter.' : 'No duplicates — nothing was skipped.'}
          </div>
        ) : (
          groups.map((g) => (
            <Group key={`${g.accountId}:${g.fingerprint}`} g={g} account={many ? nameOf(g.accountId) : ''} />
          ))
        )}
        {list.hasNextPage && (
          <div className="dup-more">
            <Button
              disabled={list.isFetchingNextPage}
              onClick={() => {
                void list.fetchNextPage();
              }}
            >
              Show {fmtInt(Math.min(60, total - groups.length))} more of {fmtInt(total - groups.length)}
            </Button>
          </div>
        )}
      </div>
    </>
  );
}

function Group({ g, account }: { g: DuplicateGroup; account: string }) {
  const decide = useDecideDuplicate();
  const reset = useResetDuplicate();
  const busy = decide.isPending || reset.isPending;
  const both = g.override?.mode === 'keep_both';
  const rows = g.versions.flatMap((v) => v.rows.map((r) => ({ v, r })));
  const base = rows.find((x) => x.r.inData) ?? rows[0];
  const cols = [...new Set<Field>([...KEY_FIELDS, ...g.differingFields])];
  const nd = g.differingFields.length;
  const amount = g.keyFields.amount;
  const target = { accountId: g.accountId, fingerprint: g.fingerprint };
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };

  return (
    <section className={both ? 'dg both' : 'dg'} aria-label={`${fmtDay(g.keyFields.bookedOn)} ${g.keyFields.payee}`}>
      <div className="dg-h">
        <b>{fmtDay(g.keyFields.bookedOn)}</b>
        <span className="dg-p">{g.keyFields.payee || g.keyFields.description}</span>
        <span className={amount && amount.amountMinor < 0 ? 'dg-a neg' : 'dg-a'}>{amount ? fmtMoney(amount) : ''}</span>
        {account && <span className="tag">{account}</span>}
        <span className="dg-s">
          {busy
            ? 'Updating…'
            : both
              ? 'Kept as separate transactions'
              : nd === 1
                ? '1 field differs'
                : nd
                  ? `${fmtInt(nd)} fields differ`
                  : 'Identical'}
        </span>
        <span className="spacer" />
        {g.override?.mode === 'prefer' && (
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            title="Show the earliest import's version again"
            onClick={() => {
              reset.mutate(target, { onError: fail });
            }}
          >
            Reset choice
          </Button>
        )}
        <Button
          size="sm"
          {...(both ? {} : { variant: 'ghost' as const })}
          disabled={busy}
          title={both ? 'Treat them as one transaction again' : 'These are different transactions — keep every copy'}
          onClick={() => {
            if (both) reset.mutate(target, { onError: fail });
            else decide.mutate({ ...target, mode: 'keep_both' }, { onError: fail });
          }}
        >
          {both ? 'Merge again' : 'Keep both'}
        </Button>
      </div>
      <div className="dg-tw">
        <table className="dg-t">
          <thead>
            <tr>
              <th aria-label="In your data" />
              <th>Import</th>
              {cols.map((c) => (
                <th key={c}>{columnById(c as ColumnId)?.label ?? c}</th>
              ))}
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map(({ v, r }, i) => (
              <tr key={`${v.importId}:${String(i)}`} className={r.inData ? 'k' : 's'}>
                <td className="ok">{r.inData ? '✓' : ''}</td>
                <td className="fn" title={v.filename}>
                  {v.filename}
                </td>
                {cols.map((c) => {
                  const text = fieldText(r.fields, c);
                  const df = base && text !== fieldText(base.r.fields, c);
                  const neg = c === 'amount' && (r.fields.amount?.amountMinor ?? 0) < 0;
                  const cls = [df && 'df', c === 'amount' && 'num', neg && 'neg'].filter(Boolean).join(' ');
                  return (
                    <td key={c} className={cls || undefined} title={text}>
                      {text}
                    </td>
                  );
                })}
                <td className="act">
                  {both ? null : r.inData ? (
                    <span className="muted">in your data</span>
                  ) : (
                    <button
                      type="button"
                      className="link"
                      disabled={busy}
                      onClick={() => {
                        decide.mutate({ ...target, mode: 'prefer', importId: v.importId }, { onError: fail });
                      }}
                    >
                      Use this version
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
