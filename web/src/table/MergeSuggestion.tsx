// The merge card under the toolbar (Phase 20, ADR 0032): a card payment typed into a bank account
// before its file was imported, and the bank's own row that arrived since. Merge keeps the bank's
// row with the typed row's category, note, group and shared copy; Keep both remembers the pair.
// One suggestion at a time, in the view of its account (or All accounts).

import { useMergeSuggestions, useMergeTransaction } from '../api/hooks/transactions';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtMoney } from '../lib/format';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { toast } from '../ui/toastStore';

export function MergeSuggestionCard({ viewId }: { viewId: string }) {
  const list = useMergeSuggestions().data;
  const merge = useMergeTransaction();
  const s = list?.find((x) => viewId === 'all' || x.account.id === viewId);
  if (!s) return null;
  const run = (keepApart: boolean) => {
    merge.mutate(
      { s, keepApart },
      {
        onSuccess: () => {
          toast(keepApart ? 'Kept both' : `Merged into the bank's ${fmtDay(s.importedOn)} row`);
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  return (
    <div className="rule-hint" role="region" aria-label="Merge suggestion">
      <Icon name="layers" />
      <span>
        You typed <b>{s.typedPayee}</b> ({fmtDay(s.typedOn)},{' '}
        {fmtMoney({ ...s.amount, amountMinor: Math.abs(s.amount.amountMinor) })}) into {s.account.name}; the bank's file
        now has <b>{s.importedPayee}</b> on {fmtDay(s.importedOn)}. The same payment?
      </span>
      <Button
        size="sm"
        variant="primary"
        disabled={merge.isPending}
        title="Keep the bank's row with the category, note, group and sharing of the typed one"
        onClick={() => {
          run(false);
        }}
      >
        Merge
      </Button>
      <Button
        size="sm"
        disabled={merge.isPending}
        onClick={() => {
          run(true);
        }}
      >
        Keep both
      </Button>
    </div>
  );
}
