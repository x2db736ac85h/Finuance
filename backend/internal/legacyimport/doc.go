// Package legacyimport migrates backups of the single-file legacy app (PLAN §4.10, ADR 0016): it
// runs every legacy file through the import pipeline, replays the legacy keys on the legacy view of
// each file to find the transactions that labels and duplicate decisions belong to, and moves
// budgets, the opening balance, investments and chats.
package legacyimport
