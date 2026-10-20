// Package platform holds small helpers shared by every layer: error kinds, clock, ids, dates.
package platform

import (
	"errors"
	"strings"
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
