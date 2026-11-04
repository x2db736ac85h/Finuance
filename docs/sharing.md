# Shared groups

A shared group is a trip, a household or an event whose costs several friends split. Each member
adds expenses; Finuance keeps the balances and says who owes whom. The design is in
[ADR 0028](adr/0028-shared-groups.md); friends and their codes in [ADR 0027](adr/0027-friends-and-friend-codes.md).

## What the others see

A shared group holds **copies**: for each expense its date, amount, payee, category name, note and
who paid. Members never see your accounts, the bank's description, the original row or any of your
other transactions, and they see you by your display name, never your e-mail address. The
database enforces this with row-level security by membership; see [ARCHITECTURE](ARCHITECTURE.md#tenant-isolation).

## Sharing

- **From a personal group**: Groups menu in the table → the ⇄ button next to a group → *Share…*.
  Pick a kind (trip, household, event, other) and the friends to invite. The group's transactions
  are copied in, and every transaction you put into the group later is shared too.
- **An empty group**: the Shared tab (title bar ⇄, or `S`) → *New shared group*. Add expenses as you
  go, without transactions.
- Only friends can be invited (Settings → Friends). An invited friend sees the group and its
  members, and the invitation arrives in their notifications; once they **join**, a personal group
  of the same name is made for them (or "… (shared)" when the name is taken), so they can put their
  own transactions in.

## Expenses

- Your **transactions** in the linked personal group are shared as copies. A copy follows its
  transaction: a new category (by hand or by a rule), a note, a typed cash row's amount, date or
  payee. Taking a transaction out of the group takes its copy out of the shared group. Deleting the
  import keeps the copy (you see it marked *source deleted*); importing the file again finds it.
- **Typed expenses** are added in the group (*Add expense*): what for, amount, date, category, who
  paid, a note. You can change or delete your own; nobody can change anyone else's.
- One currency per group, the group's. A transaction in another currency can't go into it.

## Splitting

- **Equally** (the default) between everyone, or between the members you tick. The remainder cents
  go to the first members (by id).
- **By shares**: a weight per member (2 : 1 for a couple and a single, for example).
- **Exact amounts** that must add up to the expense. "For someone else" is one member taking all of
  it.
- **Joining re-splits**: when someone joins, every equal split that was between everyone is split
  again to include them. Share a trip first and invite friends afterwards, and your earlier
  expenses are split with them. Splits between some members, by shares or exact stay as they were.

## Balances and settling up

A member's balance is what they paid minus their share, plus the payments they made, minus the
payments they received; all balances add up to zero. *Who owes whom* lists the fewest payments that
settle everyone (at most one fewer than the number of members). *Settle up* records a payment you
made or received (Finuance doesn't move money); the other side is notified.

## Leaving, archiving, deleting accounts

- **Leaving** needs a zero balance: settle up first. What you shared stays in the group; your
  personal group is unlinked.
- The **owner** can withdraw invitations, remove a member whose balance is zero, change the group's
  dates and budget, **archive** it (read-only) or delete it while nobody else has joined.
- A member who **deletes their account** stays in the group as "Former member" with their
  expenses, so the others' balances don't change.

## The Shared tab

`/s` is a timeline of your personal groups and the shared groups you are in, newest first: dates,
days, total, per day and the largest category. Below it are sums per year, and **next time** hints
that compare a group with your other groups of the same kind (insights/trips.go): spending per day
at least 1.5× the median of the others, a category at least 1.5× its usual per day (when it is at
least 10 % of the group and at least two other groups have it), or a group over its budget. A
shared group's totals are everyone's expenses in it.

`/s/:groupId` is one group: members, dates and budget; balances with *Settle up*; cards (summary,
paid vs share, spending per day, categories, top payees, largest expenses), each with a table twin;
the expenses with search, *Add expense* and the split editor; CSV export (one column per member
with their share).

## Administration

Administrators can switch shared groups off for the instance (Admin → Defaults and features) and
limit how many shared groups a user may own (`maxSharedGroups`). Shared groups are off in demo
mode. Group actions (created, invited, joined, declined, left, member removed, archived, deleted,
payment recorded) appear in each user's Activity, without amounts or payees. The data export has
`shared-groups.json` with every group as you see it.
