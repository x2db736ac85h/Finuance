// /s/:groupId (Phase 19, ADR 0028): one shared group. The header (members, dates, budget), the
// cards, the balances with "who owes whom" and Settle up, and the expenses (search, add, change
// your own). An invited user sees the group and its members with Accept and Decline.

import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router';

import { useFriends } from '../api/hooks/friends';
import {
  exportShared,
  useAccept,
  useDecline,
  useDeleteExpense,
  useDeleteShared,
  useDeleteSettlement,
  useInvite,
  useLeave,
  useRemoveMember,
  useSharedGroup,
  useSharedReport,
  useUpdateShared,
  type SharedExpense,
  type SharedGroupDetail,
} from '../api/hooks/shared';
import { money } from '../charts/lib/money';
import { BudgetBar } from '../groups/GroupReport';
import { groupColor } from '../groups/colors';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { plural } from '../lib/format';
import { saveBlob } from '../table/Toolbar';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { TextInput } from '../ui/inputs';
import { Menu, MenuItem } from '../ui/Menu';
import { Modal } from '../ui/Modal';
import { usePopover } from '../ui/Popover';
import { toast } from '../ui/toastStore';
import { CategoriesCard, DailyCard, LargestCard, PaidShareCard, PayeesCard, SummaryCard } from './cards';
import { GroupSettingsDialog, SettleDialog } from './dialogs';
import { ExpenseDialog } from './ExpenseDialog';
import { KIND_LABEL } from './text';

const fail = (e: unknown) => {
  toast(errorMessage(e), { kind: 'error' });
};

export function GroupPage({ id }: { id: string }) {
  const q = useSharedGroup(id);
  const d = q.data;
  const activeMe = d?.group.me.state === 'active';
  const report = useSharedReport(id, activeMe);
  if (q.isError) {
    return (
      <div className="sh-page">
        <p className="set-err">{errorMessage(q.error)}</p>
      </div>
    );
  }
  if (!d) return <div className="sh-page" aria-busy="true" />;
  return (
    <div className="sh-page">
      <Header d={d} />
      {d.group.me.state === 'invited' ? (
        <Invitation d={d} />
      ) : (
        <>
          <Balances d={d} />
          {report.data && (
            <div className="sh-cards">
              <SummaryCard r={report.data} d={d} />
              <PaidShareCard r={report.data} d={d} />
              <DailyCard r={report.data} />
              <CategoriesCard r={report.data} />
              <PayeesCard r={report.data} />
              <LargestCard r={report.data} d={d} />
            </div>
          )}
          <Expenses d={d} />
        </>
      )}
    </div>
  );
}

function initials(name: string): string {
  return (
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() ?? '')
      .join('') || '?'
  );
}

function Header({ d }: { d: SharedGroupDetail }) {
  const g = d.group;
  const navigate = useNavigate();
  const menu = usePopover();
  const invite = usePopover();
  const [settings, setSettings] = useState(false);
  const [confirm, setConfirm] = useState<'leave' | 'delete' | null>(null);
  const friends = useFriends(g.me.role === 'owner').data ?? [];
  const inviteM = useInvite();
  const update = useUpdateShared();
  const leave = useLeave();
  const del = useDeleteShared();
  const remove = useRemoveMember();
  const owner = g.me.role === 'owner' && g.me.state === 'active';
  const spent = d.expenses.reduce((a, e) => a + e.amountMinor, 0);
  const invitable = friends.filter(
    (f) => !d.members.some((m) => m.displayName === f.displayName && m.state !== 'left'),
  );
  const dates = g.startsOn && g.endsOn ? `${fmtDay(g.startsOn)} – ${fmtDay(g.endsOn)}` : '';
  const doExport = async () => {
    try {
      const { blob, filename } = await exportShared(g.id);
      saveBlob(blob, filename);
    } catch (e) {
      fail(e);
    }
  };
  return (
    <header className="sh-head">
      <div className="sh-title">
        <Button
          size="sm"
          variant="ghost"
          icon="left"
          onClick={() => {
            void navigate('/s');
          }}
        >
          Shared
        </Button>
        <h1>
          <i className="gdot-i" style={{ background: groupColor(g.color) }} /> {g.name}
          <span className="sh-badge">{KIND_LABEL[g.kind]}</span>
          {g.archived && <span className="sh-badge">archived</span>}
        </h1>
        <span className="muted">{[dates, g.currency].filter(Boolean).join(' · ')}</span>
        <span className="sh-tools">
          {g.me.state === 'active' && (
            <Button size="sm" icon="download" onClick={() => void doExport()}>
              Export CSV
            </Button>
          )}
          {owner && !g.archived && (
            <Button size="sm" icon="plus" aria-expanded={invite.open} onClick={invite.toggle}>
              Invite
            </Button>
          )}
          {g.me.state === 'active' && (
            <Button size="sm" icon="cog" aria-label="Group menu" aria-expanded={menu.open} onClick={menu.toggle} />
          )}
        </span>
      </div>
      <ul className="sh-members" aria-label="Members">
        {d.members
          .filter((m) => m.state !== 'left' || m.former)
          .map((m) => (
            <li key={m.id} className={m.state === 'invited' ? 'inv' : undefined}>
              <span className="avatar sm" aria-hidden="true">
                {initials(m.displayName)}
              </span>
              <span>
                {m.former ? 'Former member' : m.displayName}
                {m.isMe && ' (you)'}
                {m.role === 'owner' && <span className="muted"> · owner</span>}
                {m.state === 'invited' && <span className="muted"> · invited</span>}
              </span>
              {owner && !m.isMe && m.state !== 'left' && (
                <button
                  type="button"
                  className="adm-link"
                  onClick={() => {
                    remove.mutate(
                      { id: g.id, memberId: m.id },
                      {
                        onSuccess: () => {
                          toast(m.state === 'invited' ? 'Invitation withdrawn' : `${m.displayName} removed`);
                        },
                        onError: fail,
                      },
                    );
                  }}
                >
                  {m.state === 'invited' ? 'Withdraw' : 'Remove'}
                </button>
              )}
            </li>
          ))}
      </ul>
      {g.budgetMinor !== undefined && (
        <div className="sh-budget" aria-label="Budget">
          <span className="muted">Budget {money(g.budgetMinor, g.currency)}</span>
          <BudgetBar spent={spent} budget={g.budgetMinor} cur={g.currency} />
        </div>
      )}
      <Menu open={invite.open} anchor={invite.anchor} onClose={invite.close} label="Invite a friend" width={240}>
        {invitable.length ? (
          invitable.map((f) => (
            <MenuItem
              key={f.id}
              icon="users"
              onSelect={() => {
                invite.close();
                inviteM.mutate(
                  { id: g.id, friendId: f.id },
                  {
                    onSuccess: () => {
                      toast(`Invited ${f.displayName}`);
                    },
                    onError: fail,
                  },
                );
              }}
            >
              {f.displayName}
            </MenuItem>
          ))
        ) : (
          <div className="hint gr-empty">Everyone is in already. Add friends in Settings → Friends.</div>
        )}
      </Menu>
      <Menu open={menu.open} anchor={menu.anchor} onClose={menu.close} label="Group" width={220}>
        {owner && (
          <MenuItem
            icon="pen"
            onSelect={() => {
              menu.close();
              setSettings(true);
            }}
          >
            Group settings…
          </MenuItem>
        )}
        {owner && (
          <MenuItem
            icon="folder"
            onSelect={() => {
              menu.close();
              update.mutate(
                {
                  id: g.id,
                  body: {
                    name: g.name,
                    kind: g.kind,
                    color: g.color,
                    archived: !g.archived,
                    ...(g.startsOn ? { startsOn: g.startsOn } : {}),
                    ...(g.endsOn ? { endsOn: g.endsOn } : {}),
                    ...(g.budgetMinor ? { budgetMinor: g.budgetMinor } : {}),
                  },
                },
                {
                  onSuccess: () => {
                    toast(g.archived ? 'Group reopened' : 'Group archived');
                  },
                  onError: fail,
                },
              );
            }}
          >
            {g.archived ? 'Reopen' : 'Archive'}
          </MenuItem>
        )}
        {owner ? (
          <MenuItem
            icon="x"
            onSelect={() => {
              menu.close();
              setConfirm('delete');
            }}
          >
            Delete group…
          </MenuItem>
        ) : (
          <MenuItem
            icon="left"
            onSelect={() => {
              menu.close();
              setConfirm('leave');
            }}
          >
            Leave group…
          </MenuItem>
        )}
      </Menu>
      {settings && (
        <GroupSettingsDialog
          group={g}
          onClose={() => {
            setSettings(false);
          }}
        />
      )}
      <Modal
        open={confirm !== null}
        onClose={() => {
          setConfirm(null);
        }}
        title={confirm === 'leave' ? 'Leave group' : 'Delete group'}
        size="sm"
      >
        <div className="modal-form">
          <p>
            {confirm === 'leave'
              ? `Leave ${g.name}? What you shared stays in the group; your balance must be zero.`
              : `Delete ${g.name}? This works only while nobody else has joined; otherwise archive it.`}
          </p>
          <div className="set-actions">
            <Button
              onClick={() => {
                setConfirm(null);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                const done = {
                  onSuccess: () => {
                    toast(confirm === 'leave' ? `You left ${g.name}` : `${g.name} deleted`);
                    setConfirm(null);
                    void navigate('/s');
                  },
                  onError: (e: unknown) => {
                    setConfirm(null);
                    fail(e);
                  },
                };
                if (confirm === 'leave') leave.mutate(g.id, done);
                else del.mutate(g.id, done);
              }}
            >
              {confirm === 'leave' ? 'Leave group' : 'Delete group'}
            </Button>
          </div>
        </div>
      </Modal>
    </header>
  );
}

function Invitation({ d }: { d: SharedGroupDetail }) {
  const accept = useAccept();
  const decline = useDecline();
  const navigate = useNavigate();
  const owner = d.members.find((m) => m.role === 'owner');
  return (
    <section className="sh-invite" aria-label="Invitation">
      <p>
        <b>{owner?.displayName ?? 'A friend'}</b> invited you to <b>{d.group.name}</b>. Once you join, you see its
        expenses and balances, and a personal group of the same name lets you add your own transactions.
      </p>
      <div className="set-actions">
        <Button
          onClick={() => {
            decline.mutate(d.group.id, {
              onSuccess: () => {
                toast('Invitation declined');
                void navigate('/s');
              },
              onError: fail,
            });
          }}
        >
          Decline
        </Button>
        <Button
          variant="primary"
          disabled={accept.isPending}
          onClick={() => {
            accept.mutate(d.group.id, {
              onSuccess: () => {
                toast(`You joined ${d.group.name}`);
              },
              onError: fail,
            });
          }}
        >
          Join
        </Button>
      </div>
    </section>
  );
}

function Balances({ d }: { d: SharedGroupDetail }) {
  const [settle, setSettle] = useState(false);
  const cur = d.group.currency;
  const name = (id: string) => {
    const m = d.members.find((x) => x.id === id);
    return m ? (m.former ? 'Former member' : m.isMe ? 'You' : m.displayName) : 'Former member';
  };
  const delSettlement = useDeleteSettlement();
  return (
    <section className="sh-bal" aria-label="Balances">
      <div className="sh-bal-h">
        <h2>Balances</h2>
        {!d.group.archived && (
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              setSettle(true);
            }}
          >
            Settle up
          </Button>
        )}
      </div>
      <ul className="sh-bal-list">
        {d.balances.map((b) => (
          <li key={b.memberId}>
            <span>{name(b.memberId)}</span>
            <span className={b.balanceMinor > 0 ? 'inc' : b.balanceMinor < 0 ? 'exp' : 'muted'}>
              {b.balanceMinor > 0 ? 'gets back ' : b.balanceMinor < 0 ? 'owes ' : ''}
              {b.balanceMinor === 0 ? 'settled' : money(Math.abs(b.balanceMinor), cur)}
            </span>
          </li>
        ))}
      </ul>
      {d.transfers.length > 0 ? (
        <ul className="sh-transfers" aria-label="Who owes whom">
          {d.transfers.map((t) => (
            <li key={`${t.from}-${t.to}`}>
              {name(t.from)} → {name(t.to)} <b>{money(t.amountMinor, cur)}</b>
            </li>
          ))}
        </ul>
      ) : (
        <p className="hint">Everyone is settled up.</p>
      )}
      {d.settlements.length > 0 && (
        <details className="sh-payments">
          <summary>{plural(d.settlements.length, 'payment')} recorded</summary>
          <ul>
            {d.settlements.map((s) => (
              <li key={s.id}>
                {fmtDay(s.settledOn)} · {name(s.from)} → {name(s.to)} {money(s.amountMinor, cur)}
                {s.note && <span className="muted"> · {s.note}</span>}
                {s.mine && (
                  <button
                    type="button"
                    className="adm-link"
                    onClick={() => {
                      delSettlement.mutate({ id: d.group.id, settlementId: s.id }, { onError: fail });
                    }}
                  >
                    Delete
                  </button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
      {settle && (
        <SettleDialog
          detail={d}
          onClose={() => {
            setSettle(false);
          }}
        />
      )}
    </section>
  );
}

function Expenses({ d }: { d: SharedGroupDetail }) {
  const [q, setQ] = useState('');
  const [edit, setEdit] = useState<SharedExpense | 'new' | null>(null);
  const del = useDeleteExpense();
  const cur = d.group.currency;
  const me = d.members.find((m) => m.isMe);
  const name = (id: string) => {
    const m = d.members.find((x) => x.id === id);
    return m ? (m.former ? 'Former member' : m.isMe ? 'You' : m.displayName) : 'Former member';
  };
  const shown = useMemo(() => {
    const words = q.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
    const list = [...d.expenses].sort((a, b) => b.bookedOn.localeCompare(a.bookedOn));
    if (!words.length) return list;
    return list.filter((e) => {
      const text = `${e.payee} ${e.category} ${e.note} ${name(e.paidBy)}`.toLocaleLowerCase();
      return words.every((w) => text.includes(w));
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- name() reads d, listed
  }, [d, q]);
  return (
    <section className="sh-exp" aria-label="Expenses">
      <div className="sh-bal-h">
        <h2>Expenses</h2>
        <TextInput
          type="search"
          placeholder="Search expenses"
          aria-label="Search expenses"
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
          }}
        />
        {!d.group.archived && (
          <Button
            size="sm"
            icon="plus"
            onClick={() => {
              setEdit('new');
            }}
          >
            Add expense
          </Button>
        )}
      </div>
      {shown.length ? (
        <table className="adm-table sh-table">
          <thead>
            <tr>
              <th>Day</th>
              <th>What for</th>
              <th>Category</th>
              <th>Paid by</th>
              <th className="num">Amount</th>
              <th className="num">Your share</th>
              <th>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {shown.map((e) => (
              <tr key={e.id}>
                <td>{fmtDay(e.bookedOn)}</td>
                <td>
                  {e.payee}
                  {e.note && <span className="muted"> · {e.note}</span>}
                  {e.sourceMissing && (
                    <span className="sh-badge" title="Its transaction was deleted; the copy stays in the group">
                      source deleted
                    </span>
                  )}
                </td>
                <td>{e.category}</td>
                <td>{name(e.paidBy)}</td>
                <td className="num">{money(e.amountMinor, cur)}</td>
                <td className="num">{money(e.shares.find((s) => s.memberId === me?.id)?.shareMinor ?? 0, cur)}</td>
                <td className="sh-acts">
                  {e.mine && !d.group.archived && (
                    <>
                      <button
                        type="button"
                        className="adm-link"
                        aria-label={`Change ${e.payee}`}
                        onClick={() => {
                          setEdit(e);
                        }}
                      >
                        <Icon name="pen" />
                      </button>
                      {e.origin === 'typed' && (
                        <button
                          type="button"
                          className="adm-link"
                          aria-label={`Delete ${e.payee}`}
                          onClick={() => {
                            del.mutate(
                              { id: d.group.id, expenseId: e.id },
                              {
                                onSuccess: () => {
                                  toast(`Deleted ${e.payee}`);
                                },
                                onError: fail,
                              },
                            );
                          }}
                        >
                          <Icon name="x" />
                        </button>
                      )}
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="hint">
          {d.expenses.length
            ? 'No expense matches the search.'
            : 'No expenses yet. Add one here, or put your own transactions into your group of the same name in the table.'}
        </p>
      )}
      {edit && (
        <ExpenseDialog
          detail={d}
          {...(edit === 'new' ? {} : { expense: edit })}
          onClose={() => {
            setEdit(null);
          }}
        />
      )}
    </section>
  );
}
