package platform

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSafeError(t *testing.T) {
	pg := &pgconn.PgError{
		Severity: "ERROR", Code: "23505", Message: `duplicate key value violates unique constraint "users_email_key"`,
		Detail: "Key (email)=(marker@example.com) already exists.", ConstraintName: "users_email_key", TableName: "users",
		Where: "SQL statement with MARKER", InternalQuery: "SELECT 'MARKER'",
	}
	syntax := &pgconn.PgError{Severity: "ERROR", Code: "22P02", Message: `invalid input syntax for type integer: "MARKER"`}
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"nil":          {nil, ""},
		"not postgres": {errors.New("read file: no such file"), "read file: no such file"},
		"bare":         {pg, "postgres error SQLSTATE 23505 constraint users_email_key table users"},
		"wrapped":      {fmt.Errorf("create user: %w", pg), "create user: postgres error SQLSTATE 23505 constraint users_email_key table users"},
		"twice":        {fmt.Errorf("signup: %w (retry later)", fmt.Errorf("create user: %w", pg)), "signup: create user: postgres error SQLSTATE 23505 constraint users_email_key table users (retry later)"},
		"message":      {fmt.Errorf("query: %w", syntax), "query: postgres error SQLSTATE 22P02"},
		"joined":       {errors.Join(errors.New("first"), syntax), "first\npostgres error SQLSTATE 22P02"},
		"two":          {fmt.Errorf("a: %w / b: %w", syntax, pg), "a: postgres error SQLSTATE 22P02 / b: postgres error SQLSTATE 23505 constraint users_email_key table users"},
		"hidden":       {hides{syntax}, "something failed"}, // its own text names no value
	} {
		got := SafeError(tc.err)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
		if strings.Contains(got, "MARKER") || strings.Contains(got, "marker@") {
			t.Errorf("%s: a value of the row is in %q", name, got)
		}
	}
}

func TestUserHash(t *testing.T) {
	id := uuid.MustParse("0193e5a0-0000-7000-8000-000000000001")
	h := UserHash(id)
	if len(h) != 16 || h != UserHash(id) || h == UserHash(uuid.MustParse("0193e5a0-0000-7000-8000-000000000002")) {
		t.Errorf("UserHash = %q", h)
	}
	if strings.Contains(id.String(), h) {
		t.Error("the hash is part of the id")
	}
}

// hides wraps an error with text of its own that doesn't repeat the inner error's.
type hides struct{ err error }

func (h hides) Error() string { return "something failed" }
func (h hides) Unwrap() error { return h.err }
