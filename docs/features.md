# Features

What Finuance does, in detail. The short list is in the [README](../README.md); what is planned is in the [roadmap](ROADMAP.md).

## What you can do

- **Import bank exports.** Drag and drop CSV files, or press ⌘/Ctrl O. An import window shows the raw file and a preview before anything is saved. The encoding (UTF-8 or Windows-1250), the delimiter, the header row (even after a long preamble, as in mBank exports), date order, amount columns (one signed column or separate debit/credit) and summary rows are detected for you, and every guess can be changed. A layout you have mapped once is remembered, so the next file from the same bank goes in with one click, or straight away with the fast path.
- **Keep one history across overlapping exports.** Import January–March and then February–April: the overlap is recognised and kept once. A duplicates window shows every version side by side, with the differences highlighted, and lets you pick a version or keep both.
- **Explore every transaction.** A fast, keyboard-friendly table built for histories of 100,000 rows and more:
  - accent-insensitive search (`platnosc` finds `PŁATNOŚĆ`);
  - per-column conditions (`>100`, `-50..50`, `2025-03`, `!uber`, `=`) and an Excel-style value checklist;
  - sorting, resizable and hideable columns;
  - CSV export of exactly what you see;
  - an inspector with every field, the bank's original row, the category and a note.
- **See where the money goes.** Six charts above the table follow its search, filters and period: a summary with the change against the previous period, spending by category, cash flow by month, the month's spending pace against earlier months, top payees and a daily spending calendar. Click a category, payee, month or day to filter the table; open any chart larger to see its numbers as a table.
- **Review each month.** An overview page shows the month's income, spending, net saved and projected spending against the previous month, with highlights in plain sentences, budgets per category (a default from your last three months, editable in place), recurring payments, a money-flow diagram from income to categories, the largest expenses, savings rate, spending by weekday and the account balance. Investment, yield-on-cost and FI/RE cards track values you enter by hand.
- **Ask questions in plain language.** An optional assistant panel (⌘/Ctrl J) answers questions such as "how much did I spend on restaurants in March?" in English or Polish. It looks your data up through read-only tools (search, totals, month overview, recurring payments) and shows every lookup, with *Show in table* to see the same rows in the list. It can also propose changes, such as categorising matching rows or creating a rule, which only happen when you press *Apply*. It works with any model your admin connects: LM Studio, Ollama, vLLM or another OpenAI- or Anthropic-compatible server, local or in the cloud, and uses the model's thinking when it has it. Cloud services need your consent first; chats are deleted after 2 days unless you choose otherwise.
- **Organise categories and let rules do the sorting.** Rename, merge or delete categories (re-imports follow). Turn any search and filters into a rule ("payee contains Biedronka → Groceries"), with a live preview of what it matches; rules apply in order to every import, and categories you set by hand always win. After you categorise the same payee by hand a few times, Finuance suggests a rule. Set a category for many rows at once from the selection.
- **Recognise transfers between your own accounts.** Matching amounts going out of one account and into another within three days are marked as transfers: shown in the table with a badge (hide them with one click), and left out of the charts, the overview and the assistant's totals. You can unpair or mark them by hand.
- **Group transactions.** Select rows by dragging, Shift-click or ⌘/Ctrl-click, then add them to a colour-coded group such as "Italy trip". A quick bar shows the count and the total of the selection. Groups filter the table and are kept when you delete and re-import a file. Each group has its own report (totals, span, spending per day, categories, top payees, an optional budget) and a CSV export.
- **Keep cash too.** A cash account holds money that never reaches a bank file: add its expenses by hand (Add or `N`), change or delete them later. Rules categorise them like imported rows, and a cash withdrawal pairs with the cash it put in the wallet.
- **Add friends.** Show a short code (valid for the hour) or enter someone's in Settings → Friends. Friends see each other's display name, never the e-mail address; a bell in the title bar says when someone added or removed you, invited you or settled up.
- **Share a trip with friends.** Share a group from the Groups menu (or start an empty one in the Shared tab, `S`), invite friends, and each of you adds expenses: your own transactions put into the group, cash, or typed straight into the group. Friends see a copy of each shared expense (date, amount, payee, category, note, who paid), never your accounts or the bank's description. Split equally, by shares or as exact amounts; balances say who owes whom in as few payments as possible, and *Settle up* records a payment. The Shared tab lists your trips and groups over the years with per-day costs and "next time" hints. See [docs/sharing.md](sharing.md).
- **Move over from the prototype.** Settings → Data reads a backup of the single-file app (a locked one is decrypted in your browser) and brings its files, groups and labels, duplicate decisions, budgets, investments and chats, with a report of anything that didn't match. See [docs/migration.md](migration.md).
- **Share an instance.** Several people can use one server. Each person's data is isolated in the database itself (row-level security), not just in the application code.
- **Run the instance for others.** Administrators create and manage accounts (one-time passwords, disable, reset), set limits, choose where a new user's view starts, switch parts of the app off and see usage, all without seeing anyone's transactions. See [docs/admin.md](admin.md).
- **Pick a colour theme.** Indigo, Fjord, Grove or Ember, each in a dark and a light variant, all readable to WCAG AA; administrators can make and approve more.
- **Let the assistant do the typing**. Besides answering, it proposes changes: categories, groups and rules, a budget, a cash expense, sharing or a new split, a savings goal. You apply each with one click, or let chosen kinds apply themselves (never more than 50 rows); everything it changed is marked ✦ and can be undone from the card or Settings → Activity.
- **Snap a receipt**. With a model that reads images, photograph a receipt in the panel: the assistant finds the payment and proposes the receipt in its note, or proposes the expense. The photo is shrunk and stripped of its location in your browser. A card payment typed in before the bank's file arrives is merged into the bank's row when it does.
- **Set goals**: save an amount by a date, or spend a percentage less on a category than before; the Overview's *Goals* card shows each against a steady pace.
- **Get a digest**: a short daily or weekly summary written in the background (budgets and goals that need attention, payments due, changes worth making), behind the bell, with its proposals waiting for you.
- **Read the advice library**: short articles your admin publishes; the assistant uses them for general money questions and names them in its answer.
- **Script it**: personal access tokens (read only, or read and write) and `fin`, a command line for accounts, transactions, imports, the overview and the assistant, with table, CSV or JSON output ([docs/cli.md](cli.md)).
- **See what happened in your account.** Settings → Activity lists sign-ins (failed ones too), password changes, revoked sessions, imports, deletions, exports and applied assistant proposals, with the address and browser of each, for a year.

## Key concepts

**Accounts and imports.** An *account* is one bank account. An *import* is one CSV file brought into an account. The original file is stored, so an import can be downloaded again, re-mapped and reprocessed, or deleted (which removes its transactions unless another import also contains them).

**Import profiles.** When you confirm how a file's columns map to Finuance's fields (date, amount, payee, …), that mapping is remembered for that column layout. The next file with the same header uses it automatically, and a short label (`b1`, `b2`, …) tells layouts apart in the import history.

**Duplicates are found per account.** Each transaction gets a fingerprint from the bank's reference number and amount, or, when there is no reference, from its date, amount, payee and title. Within one account, a fingerprint found in two imports is one transaction; identical twins inside one file are counted and kept apart. The same purchase in two *different* accounts is never merged; that is usually a transfer between your own accounts.

**Your edits survive re-imports.** Categories you set, notes and group labels are stored against the transaction's fingerprint, not against a database row. Delete an import, import the file again, and your edits come back.

**Views and the filter language.** *All accounts* and every single account are separate views, each remembering its own columns, widths, sort and period. Filters are written per column:

| Write | Means |
|---|---|
| `kino` · `!kino` | contains · doesn't contain (accent- and case-insensitive) |
| `=Rachunki` · `=` · `!=` | exactly · empty · not empty |
| `>100` · `<=0` · `-50..50` | number comparisons and ranges |
| `2025-03` · `>=01.03.2025` · `01.2025..03.2025` | dates: a month, a day, a range |

**Categories, rules and transfers.** A transaction's category comes from the bank, from a rule, or from you, in rising order of priority: rules are re-applied from scratch whenever they change, and never touch a category you set by hand. Transfers between your own accounts are detected after every import and left out of spending totals.

**Groups** are your own colour-coded labels (one per transaction), independent of the bank's categories. They work like any other column: filter, sort, export.

**Privacy by design.** Data stays on your server. The database enforces that every query only sees the signed-in user's rows (PostgreSQL row-level security), passwords are hashed with Argon2id, sessions are server-side, the web app runs under a strict Content Security Policy, and an append-only activity log records every sign-in and change to your data. The optional AI assistant only reads data through read-only tools, sends a model only what it looks up (and a receipt photo you attach), and talks to a model your admin connects; a cloud model needs each user's consent. Details are in the [threat model](THREAT-MODEL.md).
