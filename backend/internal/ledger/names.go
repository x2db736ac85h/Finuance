package ledger

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
)

// nameKey is the normalised form used for uniqueness of account, category and group names, so
// "Żywność" and " zywnosc " are the same name (the same normalisation as search, PLAN §4.1).
func nameKey(name string) string { return ingest.Collapse(ingest.Norm(name)) }

// cleanName trims a user-entered name and checks it is 1…max characters.
func cleanName(what, name string, maxLen int) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxLen {
		return "", fmt.Errorf("%s name must be 1–%d characters: %w", what, maxLen, ErrInvalid)
	}
	return name, nil
}
