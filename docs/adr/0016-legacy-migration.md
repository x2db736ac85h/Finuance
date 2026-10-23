# 0016. Migrate legacy backups by replaying the legacy keys on each imported file

- Status: accepted
- Date: 2026-10-23

## Context

A backup of the single-file app (PLAN §4.10) holds the raw files, their import settings, and the
user's work keyed by the legacy row identity (`txKeys`): group labels (`tags`), duplicate decisions
(`dupKeep`, `dupPick`), plus budgets, the opening balance, investments and chats. It may be encrypted
with a passphrase (PBKDF2-SHA-256, AES-GCM).

The legacy key of a row is built from the table the legacy app saw: its own header rule, the file's
`cfg` (role map, invert, month-first dates rewritten by `applyMapping`), the roles it detected on
that rewritten table, `amount.toFixed(2)`, the date as `Date.UTC` milliseconds, and a `#n` suffix for
twins — counted per file view, or across the merged "All data" view after its de-dupe. The new app
identifies a transaction by `(account, fingerprint, occurrence)` computed from the *mapped* roles.
The plan sketched translating each key into a fingerprint; that fails wherever the detected and the
mapped roles pick different columns, and the merged view's `#n` numbering can't be translated at all.

## Decision

1. **The browser reads the backup.** It parses it (`deser`), checks the passphrase and decrypts with
   WebCrypto. The passphrase and the ciphertext never reach the server. It sends a manifest (the
   decrypted private data without the legacy AI key, the file settings, the chosen accounts, the
   chats if wanted) and the plain file bytes to `POST /legacy-imports`.
2. **A River job (`LegacyImport`) migrates** (`internal/legacyimport`). Each file goes through the
   normal import pipeline (preview with the translated settings, commit inline), in the legacy file
   order, so the first file holding a transaction is its source as in the legacy merged view.
3. **The job rebuilds the legacy tables and replays the legacy keys** on them: the legacy header rule
   (`ingest.ShapeOptions.LegacyHeader`), a port of `applyMapping`, legacy column typing and role
   detection, `txKeys` per file and for the merged view (with its de-dupe, overrides and `#n`
   numbering). Row *i* of a legacy table is row *i* of the new import, so every legacy key points at
   the transaction and fingerprint of that row. Labels go to those transactions; `dupKeep`/`dupPick`
   become overrides on their fingerprints. The replay is checked against the oracle (every recorded
   table and setting, every key set and every de-dupe variant).
4. **Re-running is safe.** Identical files are reused (the import pipeline's SHA-256 check), accounts
   and groups are found by name, overrides and labels are idempotent, investments are written only
   when the user has none, and chats already there (same title) are skipped.
5. **The manifest is temporary.** It and the file bytes are deleted when the run ends; the report
   stays. One run per user at a time.

## Consequences

- Labels and decisions match whenever the legacy app and the new import read the same rows,
  including files whose legacy roles differ from the new mapping (the case the plan expected to
  lose). They can't match for a file whose header the legacy app found elsewhere (the Phase 9 header
  fix) unless it had an explicit header row; the report says so per file.
- The legacy key replay uses float64, as the legacy keys did; it never produces stored amounts.
- Preferences (theme, size, charts, panel settings) are applied by the browser through the normal
  preferences sync, not by the job: the server keeps the preferences document opaque.
