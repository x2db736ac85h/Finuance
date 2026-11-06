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
    (p.action !== 'set_group' && !p.categoryId) || (p.action === 'set_group' && !p.groupId)
      ? p.action === 'set_group'
        ? 'The group is new and is created first.'
        : 'The category is new and is created first.'
      : '';
  const run = () => {
    if (!chatId) return;
    apply.mutate(
      { proposal: p, chatId },
      {
        onSuccess: (n) => {
          markApplied(p.key);
          toast(
            p.action === 'create_rule'
              ? `Rule “${p.ruleName ?? ''}” created · applying…`
              : `Applied to ${plural(n, 'transaction')}`,
          );
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
            <span className="pr-done">✓ Applied</span>
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
