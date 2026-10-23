# legacyimport

Migration from the single-file prototype (PLAN §4.10, ADR 0016). The browser reads and decrypts the
backup and sends a manifest plus the files; the `LegacyImport` job (`Migrator.Run`):

1. imports every file through the normal pipeline, in the legacy order (`migrate.go`);
2. rebuilds each legacy table and replays the legacy keys on it (`dataset.go`: header rule,
   `applyMapping`, typing, roles, `txKeys`, the merged view's de-dupe), so every legacy key points at
   the transaction of the same row;
3. recreates groups and labels, duplicate decisions, budgets, the opening balance, investments and
   chats, and writes the report.

The replay is tested against the legacy oracle (`dataset_test.go`); the whole run on the oracle's
plain and locked backups (`migrate_test.go`).
