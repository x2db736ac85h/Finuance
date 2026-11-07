// The confirmation card of an assistant proposal (PLAN 15.7, ADR 0019): what would change, the
// affected rows, and Apply. The server never applies a proposal; Apply sends the change through
// the normal endpoints with the user's session. An applied card offers Undo (ADR 0030), and an
// undone one Apply again.

import { useAIChanges, useApplyProposal, useRevertAIChange, type Proposal } from '../api/hooks/assistant';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtMoney, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { toast } from '../ui/toastStore';
import { useChat } from './chatStore';

/** The panel applies an opted-in proposal by itself only up to this many transactions (ADR 0030). */
export const AUTO_APPLY_MAX_ROWS = 50;

/** The card's headline, e.g. "Set the category Internet for 14 transactions". */
export function proposalTitle(p: Proposal): string {
  switch (p.action) {
    case 'set_category':
      return `Set the category “${p.categoryName ?? ''}” for ${plural(p.matched, 'transaction')}`;
    case 'set_group':
      return `Add ${plural(p.matched, 'transaction')} to the group “${p.groupName ?? ''}”`;
    case 'create_rule': {
      const parts = [
        p.match?.search ? `words “${p.match.search}”` : '',
        ...(p.match?.filters ?? []).map((f) => `payee contains “${f.cond ?? ''}”`),
      ].filter(Boolean);
      return `Create the rule “${p.ruleName ?? ''}”: ${parts.join(' and ')} → ${p.categoryName ?? ''}`;
    }
    case 'set_budget':
      return p.removeBudget || !p.budget
        ? `Remove the budget of “${p.categoryName ?? ''}” (back to the default from earlier months)`
        : `Set the monthly budget of “${p.categoryName ?? ''}” to ${fmtMoney(p.budget)}` +
            (p.budgetBefore ? ` (now ${fmtMoney(p.budgetBefore)})` : '');
    case 'add_expense':
      return (
        `Add ${p.amount ? fmtMoney(p.amount) : ''} paid to “${p.payee ?? ''}” on ${fmtDay(p.date)} to ${p.accountName ?? ''}` +
        (p.categoryName ? ` · ${p.categoryName}` : '')
      );
    case 'add_to_shared_group':
      return `Share ${plural(p.matched, 'transaction')} with “${p.sharedGroupName ?? ''}”`;
    case 'split_expense': {
      const members = p.split?.members ?? [];
      const who =
        p.split?.mode === 'exact'
          ? `exactly: ${members.map((m) => `${m.name} ${fmtMoney({ amountMinor: m.amountMinor ?? 0, currency: p.amount?.currency ?? 'PLN' })}`).join(', ')}`
          : p.split?.mode === 'shares'
            ? `by shares: ${members.map((m) => `${m.name} ${String(m.weight ?? 1)}`).join(', ')}`
            : members.length === 1
              ? `all for ${members[0]?.name ?? ''}`
              : `equally between ${members.map((m) => m.name).join(', ')}`;
      return `Split “${p.payee ?? ''}” (${fmtDay(p.date)}${p.amount ? `, ${fmtMoney(p.amount)}` : ''}) in “${p.sharedGroupName ?? ''}” ${who}`;
    }
    case 'create_goal':
      return `New goal “${p.goal?.name ?? ''}”: ${p.goal ? goalText(p.goal) : ''}`;
    case 'update_goal': {
      const g = p.goal;
      const b = p.goalBefore;
      if (!g || !b) return 'Change a goal';
      const changes = [
        g.name !== b.name ? `rename to “${g.name}”` : '',
        g.targetMinor !== b.targetMinor && g.targetMinor !== undefined
          ? `target ${fmtMoney({ amountMinor: g.targetMinor, currency: g.currency })}`
          : '',
        g.reducePct !== b.reducePct ? `${String(g.reducePct ?? 0)} % less` : '',
        g.categoryName !== b.categoryName ? `on ${g.categoryName ?? ''}` : '',
        g.deadline !== b.deadline ? `deadline ${fmtDay(g.deadline)}` : '',
        g.status !== b.status
          ? g.status === 'active'
            ? 'reopen it'
            : g.status === 'done'
              ? 'close it'
              : 'archive it'
          : '',
      ].filter(Boolean);
      return `Change the goal “${b.name}”: ${changes.join(', ')}`;
    }
    case 'attach_receipt':
      return (
        `Add the receipt to “${p.payee ?? ''}” (${fmtDay(p.date)}${p.amount ? `, ${fmtMoney(p.amount)}` : ''}) in ${p.accountName ?? ''}: ` +
        (p.receiptLine ?? '').replace(/^Receipt: /, '') +
        (p.categoryName ? ` · ${p.categoryName}` : '')
      );
  }
}

/** A goal in a few words: "save 10 000,00 PLN by 30.06.2027" or "spend 20 % less on Jedzenie until …". */
function goalText(g: NonNullable<Proposal['goal']>): string {
  return g.kind === 'save'
    ? `save ${fmtMoney({ amountMinor: g.targetMinor ?? 0, currency: g.currency })}${g.accountName ? ` on ${g.accountName}` : ''} by ${fmtDay(g.deadline)}`
    : `spend ${String(g.reducePct ?? 0)} % less on ${g.categoryName ?? ''} than the last ${plural(g.baselineMonths, 'month')}, until ${fmtDay(g.deadline)}`;
}

/** What Apply did, for the toast. */
function appliedText(p: Proposal, n: number): string {
  switch (p.action) {
    case 'create_rule':
      return `Rule “${p.ruleName ?? ''}” created · applying…`;
    case 'set_budget':
      return `Budget of “${p.categoryName ?? ''}” ${p.removeBudget ? 'removed' : 'set'}`;
    case 'add_expense':
      return `Added to ${p.accountName ?? 'the account'}`;
    case 'add_to_shared_group':
      return `Shared ${plural(n, 'transaction')} with “${p.sharedGroupName ?? ''}”`;
    case 'split_expense':
      return `Split of “${p.payee ?? ''}” changed`;
    case 'create_goal':
      return `Goal “${p.goal?.name ?? ''}” added — see Overview → Goals`;
    case 'update_goal':
      return `Goal “${p.goal?.name ?? ''}” changed`;
    case 'attach_receipt':
      return `Receipt added to “${p.payee ?? 'the transaction'}”`;
    default:
      return `Applied to ${plural(n, 'transaction')}`;
  }
}

type Props = { proposal: Proposal; chatId: string | null; onShow?: () => void };

export function ProposalCard({ proposal: p, chatId, onShow }: Props) {
  const apply = useApplyProposal();
  // the server records applied proposals in the activity log; the chat reads them back
  const done = useChat((s) => s.chatId === chatId && s.applied.includes(p.key));
  const markApplied = useChat((s) => s.markApplied);
  // the newest change of this proposal: open (Undo) or undone (Apply again); none for proposals
  // applied before changes were recorded
  const changes = useAIChanges(done);
  const revert = useRevertAIChange();
  const mine = changes.data?.pages[0]?.changes.find((c) => c.chatId === chatId && c.proposalKey === p.key);
  const undone = done && mine?.revertedAt !== undefined;
  const creates =
    p.action === 'set_group'
      ? p.groupId
        ? ''
        : 'The group is new and is created first.'
      : (p.action === 'set_category' ||
            p.action === 'create_rule' ||
            ((p.action === 'add_expense' || p.action === 'attach_receipt') && p.categoryName)) &&
          !p.categoryId
        ? 'The category is new and is created first.'
        : '';
  const run = () => {
    if (!chatId) return;
    apply.mutate(
      { proposal: p, chatId },
      {
        onSuccess: (n) => {
          markApplied(p.key);
          toast(appliedText(p, n));
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  const undo = () => {
    if (!mine) return;
    revert.mutate(mine.id, {
      onSuccess: (r) => {
        toast(r.rows > 0 ? `Undone for ${plural(r.rows, 'transaction')}` : 'Undone');
      },
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  return (
    <div className="proposal" role="group" aria-label="Proposed change">
      <div className="pr-h">
        <Icon name="sliders" />
        <b>{proposalTitle(p)}</b>
      </div>
      {p.reason && <p className="pr-why">{p.reason}</p>}
      {p.action === 'create_rule' && (
        <p className="hint">
          It matches {plural(p.matched, 'transaction')} now and future imports; categories you set by hand stay.
        </p>
      )}
      {creates && <p className="hint">{creates}</p>}
      {p.sample.length > 0 && (
        <table className="pr-rows">
          <tbody>
            {p.sample.map((r) => (
              <tr key={r.id}>
                <td>{fmtDay(r.bookedOn)}</td>
                <td className="tx">{r.payee}</td>
                <td className={r.amount.amountMinor < 0 ? 'num neg' : 'num'}>{fmtMoney(r.amount)}</td>
                <td className="muted">{r.category ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {p.matched > p.sample.length && <p className="hint">and {fmtInt(p.matched - p.sample.length)} more</p>}
      <div className="pr-a">
        {done && !undone ? (
          <>
            <span className="pr-done">{mine?.auto ? '✓ Applied automatically' : '✓ Applied'}</span>
            {mine && !mine.revertedAt && (
              <Button size="sm" disabled={revert.isPending} onClick={undo}>
                Undo
              </Button>
            )}
          </>
        ) : (
          <>
            {undone && <span className="pr-done">↺ Undone</span>}
            <Button size="sm" variant="primary" disabled={!chatId || apply.isPending} onClick={run}>
              {undone ? 'Apply again' : 'Apply'}
            </Button>
          </>
        )}
        {p.toolFilter && onShow && (
          <Button size="sm" variant="ghost" onClick={onShow}>
            Show in table
          </Button>
        )}
        {!done && <span className="hint">Nothing changes until you apply it.</span>}
      </div>
    </div>
  );
}
