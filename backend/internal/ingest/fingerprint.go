package ingest

import (
	"crypto/sha256"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// FingerprintKey is the de-duplication identity of a staged row (port of the legacy merge key,
// PLAN §4.4). Two rows from overlapping exports of one account have the same key, while fields
// that banks re-label between exports (category, operation type) are deliberately left out:
//
//	with a reference:  "r|" + Norm(reference) + "|" + amount
//	without:           bookedOn + "|" + amount + "|" + Collapse(Norm(payee)) + "|" + Collapse(Norm(description))
//
// amount is money.FormatMinor in the row's currency exponent ("-127.13"); bookedOn is YYYY-MM-DD.
// Changing this function changes every stored fingerprint: treat it as a data migration.
func FingerprintKey(r *StagedRow, exp int) string {
	amount := money.FormatMinor(r.AmountMinor, exp)
	if r.Reference != "" {
		return "r|" + Norm(r.Reference) + "|" + amount
	}
	return strings.Join([]string{r.BookedOn.String(), amount, Collapse(Norm(r.Payee)), Collapse(Norm(r.Description))}, "|")
}

// Fingerprint is SHA-256 of the key (stored as bytea).
func Fingerprint(key string) [32]byte { return sha256.Sum256([]byte(key)) }

// AssignOccurrences numbers rows that share a fingerprint, in file order: the first is 1, the
// second 2, … Errored rows get 0. With the unique constraint on (account, fingerprint, occurrence)
// this keeps a transaction max(count in any single import) times: two identical coffees in one
// export stay two, the same coffee in two overlapping exports is stored once.
func AssignOccurrences(rows []StagedRow) {
	seen := map[[32]byte]int{}
	for i := range rows {
		if rows[i].Error != "" {
			rows[i].Occurrence = 0
			continue
		}
		seen[rows[i].Fingerprint]++
		rows[i].Occurrence = seen[rows[i].Fingerprint]
	}
}

// HeaderSignature identifies a file layout for import profiles (PLAN §4.4): files with the same
// normalised headers and delimiter reuse the remembered mapping.
func HeaderSignature(headers []string, delim rune) [32]byte {
	n := make([]string, len(headers))
	for i, h := range headers {
		n[i] = Norm(h)
	}
	return sha256.Sum256([]byte(strings.Join(n, "\u0001") + "|" + string(delim)))
}
