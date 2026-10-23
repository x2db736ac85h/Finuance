# Moving over from the single-file app

Finuance started as one HTML file that kept everything in the browser. Its backups can be moved into
a Finuance server in one step: **Settings → Data → Import from the single-file app**.

## What you need

A backup made in the single-file app: *Settings → Back up* there downloads
`finuance-backup-YYYY-MM-DD.json` (older backups, made when the app was called CSV Inspector, work
too). If you had set a passphrase, you need it as well.

## What happens

1. **You choose the backup.** It is read in your browser. A locked backup asks for its passphrase;
   it is checked and the data decrypted in the browser. The passphrase and the encrypted data are
   never sent to the server.
2. **You choose the accounts.** The window lists every file with its size and the dates it covers.
   A file whose account number ends like one of your accounts goes there; the others get one new
   account per file layout (rename them, or pick existing accounts). You also choose where the old
   opening balance goes, and whether to bring the assistant chats.
3. **The server imports.** Each file goes through the normal import, in the order the old app had
   them, so overlapping exports are merged exactly as before. Then it recreates:
   - your **groups** and puts every labelled transaction back in its group;
   - your **duplicate decisions** (*Keep both* and *Use this version*);
   - **budgets** (by category name), the **opening balance**, **investments** (only if you have none
     here yet) and, if chosen, the **chats** (they then follow your chat retention, 2 days by default);
   - your appearance and chart settings, if that box is ticked.
4. **The report** says what was imported, what was there already, and which labels or decisions
   didn't find their transaction (with the row they belonged to and why). It stays under
   Settings → Data.

Running the same backup again is safe: files already imported are reused, and nothing is created
twice.

## What isn't moved

- The old app's AI server address and key: the assistant here uses the connections your admin set up.
- Per-file table layouts (hidden columns, widths, sorting): the table here has its own columns.
- A file the old app read with a different header row than Finuance (an mBank or PKO BP export
  imported without choosing the header row): its transactions are imported, but labels on it can't
  be matched; the report lists the file.

How it works inside: [ADR 0016](adr/0016-legacy-migration.md).
