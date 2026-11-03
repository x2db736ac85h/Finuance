// Package platform holds small helpers shared by every layer: error kinds, clock, ids, dates.
package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Error kinds. Domain packages wrap them (fmt.Errorf("account %s: %w", id, platform.ErrNotFound))
// or define sentinels that wrap them; httpapi/problems.go maps each kind to one problem code.
var (
	ErrUnauthenticated     = errors.New("unauthenticated")
	ErrForbidden           = errors.New("forbidden")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrDuplicateFile       = errors.New("duplicate file")
	ErrImportNotCommitable = errors.New("import not committable")
	ErrStaleVersion        = errors.New("stale version")
	ErrRateLimited         = errors.New("rate limited")
	ErrPayloadTooLarge     = errors.New("payload too large")
	ErrUnsupportedFile     = errors.New("unsupported file")
	ErrNotImplemented      = errors.New("not implemented")
	// Phase 19: instance administration (ADR 0025).
	ErrLimitReached           = errors.New("limit reached")
	ErrPasswordChangeRequired = errors.New("password change required")
	ErrAccountDisabled        = errors.New("account disabled")
	ErrLastAdmin              = errors.New("last administrator")
	ErrContrast               = errors.New("contrast too low")
)

// FieldError is one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError aggregates every invalid field of one request.
type ValidationError struct {
	Fields []FieldError
}

// Add records an invalid field.
func (v *ValidationError) Add(field, message string) {
	v.Fields = append(v.Fields, FieldError{Field: field, Message: message})
}

// Err returns v when any field was added, else nil, so callers can `return v.Err()`.
func (v *ValidationError) Err() error {
	if v == nil || len(v.Fields) == 0 {
		return nil
	}
	return v
}

func (v *ValidationError) Error() string {
	parts := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// RetryAfterError is a rate-limit rejection that knows when the client may retry. It wraps
// ErrRateLimited (429); the problem writer turns After into a Retry-After header.
type RetryAfterError struct {
	After time.Duration
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("rate limited; retry after %ds", e.Seconds())
}

// Unwrap maps the error to ErrRateLimited.
func (e *RetryAfterError) Unwrap() error { return ErrRateLimited }

// Seconds is After rounded up to whole seconds (at least 1), as Retry-After needs.
func (e *RetryAfterError) Seconds() int {
	s := int((e.After + time.Second - 1) / time.Second)
	return max(s, 1)
}
