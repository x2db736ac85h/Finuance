package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// SafeError is err's text for a log line or a span. A PostgreSQL error is reduced to its SQLSTATE
// and the names of the constraint, table and column: its message and detail can quote the values
// of the row ("Key (email)=(…) already exists", "invalid input syntax for type integer: …"), and
// those must not reach the logs (PLAN 23.4, DB-5). Text that wraps the error is kept.
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, pg := range pgErrors(err, nil) {
		text = strings.Replace(text, pg.Error(), safePg(pg), 1)
	}
	return text
}

// pgErrors collects every PostgreSQL error in err's tree (errors.Join and %w with several errors
// make trees, and errors.As would stop at the first).
func pgErrors(err error, found []*pgconn.PgError) []*pgconn.PgError {
	// this function is the walk over the tree, so it looks at each node itself
	switch e := err.(type) { //nolint:errorlint // not a comparison: every wrapped error is visited below
	case nil:
		return found
	case *pgconn.PgError:
		return append(found, e)
	case interface{ Unwrap() []error }:
		for _, inner := range e.Unwrap() {
			found = pgErrors(inner, found)
		}
		return found
	default:
		return pgErrors(errors.Unwrap(err), found)
	}
}

func safePg(pg *pgconn.PgError) string {
	var b strings.Builder
	b.WriteString("postgres error SQLSTATE ")
	b.WriteString(pg.Code)
	for _, f := range [...]struct{ name, value string }{
		{"constraint", pg.ConstraintName}, {"table", pg.TableName}, {"column", pg.ColumnName},
	} {
		if f.value != "" {
			b.WriteString(" " + f.name + " " + f.value)
		}
	}
	return b.String()
}

// UserHash is how a user appears in logs: hex of the first 8 bytes of SHA-256(user id), never the
// id itself (PLAN §8.2). One helper, so every line that names a user can be joined on it.
func UserHash(id uuid.UUID) string {
	sum := sha256.Sum256(id[:])
	return hex.EncodeToString(sum[:8])
}
