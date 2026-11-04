package instance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Limits are per-user limits. In the instance document they are the defaults; on a user
// (users.limits) they override the defaults. A missing value means "the next level decides":
// user → instance → the environment's bound (or no limit where the environment sets none).
type Limits struct {
	MaxUploadBytes   *int64 `json:"maxUploadBytes,omitempty"`
	ImportsPerDay    *int64 `json:"importsPerDay,omitempty"`
	MaxAccounts      *int64 `json:"maxAccounts,omitempty"`
	MaxGroups        *int64 `json:"maxGroups,omitempty"`
	MaxSharedGroups  *int64 `json:"maxSharedGroups,omitempty"`
	AssistantPerHour *int64 `json:"assistantPerHour,omitempty"`
}

// Bounds are the environment's outer limits (FINUANCE_MAX_UPLOAD_BYTES, FINUANCE_AI_RATE_PER_HOUR):
// an administrator can only tighten them.
type Bounds struct {
	MaxUploadBytes   int64
	AssistantPerHour int64
}

// Effective is what applies to one user; 0 means no limit (counts only).
type Effective struct {
	MaxUploadBytes   int64 `json:"maxUploadBytes"`
	ImportsPerDay    int64 `json:"importsPerDay"`
	MaxAccounts      int64 `json:"maxAccounts"`
	MaxGroups        int64 `json:"maxGroups"`
	MaxSharedGroups  int64 `json:"maxSharedGroups"`
	AssistantPerHour int64 `json:"assistantPerHour"`
}

// Resolve combines a user's overrides, the instance defaults and the environment's bounds.
func Resolve(user, inst Limits, b Bounds) Effective {
	pick := func(u, i *int64, def int64) int64 {
		switch {
		case u != nil:
			return *u
		case i != nil:
			return *i
		}
		return def
	}
	capped := func(v, bound int64) int64 {
		if bound > 0 && (v == 0 || v > bound) {
			return bound
		}
		return v
	}
	return Effective{
		MaxUploadBytes:   capped(pick(user.MaxUploadBytes, inst.MaxUploadBytes, b.MaxUploadBytes), b.MaxUploadBytes),
		ImportsPerDay:    pick(user.ImportsPerDay, inst.ImportsPerDay, 0),
		MaxAccounts:      pick(user.MaxAccounts, inst.MaxAccounts, 0),
		MaxGroups:        pick(user.MaxGroups, inst.MaxGroups, 0),
		MaxSharedGroups:  pick(user.MaxSharedGroups, inst.MaxSharedGroups, 0),
		AssistantPerHour: capped(pick(user.AssistantPerHour, inst.AssistantPerHour, b.AssistantPerHour), b.AssistantPerHour),
	}
}

// ParseLimits decodes users.limits or a submitted override document.
func ParseLimits(raw []byte) (Limits, error) {
	var l Limits
	if len(raw) == 0 {
		return l, nil
	}
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		ve := &platform.ValidationError{}
		ve.Add("limits", fmt.Sprintf("not a limits document: %v", err))
		return Limits{}, ve
	}
	ve := &platform.ValidationError{}
	l.validate("limits", ve)
	return l, ve.Err()
}

func (l Limits) validate(prefix string, ve *platform.ValidationError) {
	check := func(name string, v *int64, minimum, maximum int64) {
		if v != nil && (*v < minimum || *v > maximum) {
			ve.Add(prefix+"."+name, fmt.Sprintf("must be between %d and %d", minimum, maximum))
		}
	}
	check("maxUploadBytes", l.MaxUploadBytes, 1024, 1<<31)
	check("importsPerDay", l.ImportsPerDay, 1, 100_000)
	check("maxAccounts", l.MaxAccounts, 1, 10_000)
	check("maxGroups", l.MaxGroups, 1, 100_000)
	check("maxSharedGroups", l.MaxSharedGroups, 1, 10_000)
	check("assistantPerHour", l.AssistantPerHour, 1, 10_000)
}

// LimitError is a request refused by a limit. It wraps platform.ErrLimitReached (422).
type LimitError struct {
	Limit string // the Effective field's JSON name
	Value int64
}

func (e *LimitError) Error() string { return fmt.Sprintf("limit %s (%d) reached", e.Limit, e.Value) }

// Unwrap maps the error to platform.ErrLimitReached.
func (e *LimitError) Unwrap() error { return platform.ErrLimitReached }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
