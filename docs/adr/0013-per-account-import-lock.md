# 0013. Serialise writes to an account's transactions with a row lock on the account

- Status: accepted
- Date: 2026-10-21

## Context

Several operations rewrite the transactions of one account (PLAN §4.4):
- committing an import (the `CommitImport` job);
- deleting an import (orphans + re-materialising fingerprints it was the source of);
- reprocessing a committed import (restage, then commit again);
- setting or clearing a duplicate override, and the `RematerializeFingerprint` job.

Each of these reads "which import is the source of this fingerprint" and then inserts, updates or deletes transactions accordingly. Two of them running at the same time on the same account can interleave badly.
- Two overlapping commits insert the same identities. The commit's `INSERT … ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO NOTHING` keeps one copy, but each transaction takes the unique-index entries in its own file order, so the two can deadlock. Under READ COMMITTED the second commit also can't see the first's uncommitted rows when it decides which fingerprints to re-materialise.
- A commit and a delete can each re-materialise the same fingerprint from a different "earliest import".

River runs up to 4 jobs at a time, and the API runs deletes and override changes in request transactions, so these do overlap in practice (two uploads of overlapping exports a second apart).

## Decision

Every ledger operation that writes an account's transactions first takes `SELECT … FROM accounts WHERE user_id = $1 AND id = $2 FOR NO KEY UPDATE` (`ledger.lockAccount`) inside its `WithUser` transaction, before reading any source information. Operations on the same account run one after another; different accounts and different users are unaffected.

`FOR NO KEY UPDATE` (not `FOR UPDATE`) still lets other transactions insert rows that reference the account by foreign key (imports, staged rows), so uploads and previews never wait for a commit.

## Consequences

- **Correct by construction:** the incremental result equals a from-scratch re-materialisation (the property test checks this), and `TestConcurrentOverlappingCommits` commits overlapping imports in parallel without duplicates, deadlocks or lost stats.
- **Throughput per account is serial.** One account's commits queue behind each other; a 100k-row commit holds the lock for seconds. That is fine for one person's bank exports.
- **Lock order:** the account row first, then transactions. New code that writes transactions must call `lockAccount` first; operations spanning several accounts must lock them in id order.

## Alternatives considered

- **`SERIALIZABLE` isolation with retries:** correct, but every overlapping commit aborts and is retried, and the retry logic spreads to every caller (request handlers too).
- **Advisory locks (`pg_advisory_xact_lock` on a hash of the account id):** same effect, but invisible in the schema, and hash collisions serialise unrelated accounts.
- **A River queue per account / unique jobs:** covers the job but not deletes and overrides, which run in request transactions.
