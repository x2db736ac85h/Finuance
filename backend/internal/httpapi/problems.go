package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// problemKind is one row of the error → problem mapping.
type problemKind struct {
	err    error
	code   gen.ProblemCode
	status int
	title  string
}

// problemTable maps platform and ledger error kinds to stable problem codes (docs/PLAN.md §6.1).
// Order matters only when an error wraps several kinds: the first match wins.
var problemTable = []problemKind{
	{platform.ErrUnauthenticated, gen.Unauthenticated, http.StatusUnauthorized, "Authentication required"},
	{platform.ErrForbidden, gen.Forbidden, http.StatusForbidden, "Forbidden"},
	{platform.ErrNotFound, gen.NotFound, http.StatusNotFound, "Not found"},
	{platform.ErrDuplicateFile, gen.DuplicateFile, http.StatusConflict, "This file was already imported"},
	{platform.ErrImportNotCommitable, gen.ImportNotCommittable, http.StatusConflict, "The import cannot be committed"},
	{platform.ErrStaleVersion, gen.StaleVersion, http.StatusConflict, "The resource was changed by someone else"},
	{platform.ErrConflict, gen.Conflict, http.StatusConflict, "Conflict"},
	{platform.ErrRateLimited, gen.RateLimited, http.StatusTooManyRequests, "Too many requests"},
	{platform.ErrPayloadTooLarge, gen.PayloadTooLarge, http.StatusRequestEntityTooLarge, "Payload too large"},
	{platform.ErrUnsupportedFile, gen.UnsupportedFile, http.StatusUnsupportedMediaType, "Unsupported file"},
	{platform.ErrNotImplemented, gen.NotImplemented, http.StatusNotImplemented, "Not implemented yet"},
	{platform.ErrLimitReached, gen.LimitReached, http.StatusUnprocessableEntity, "A limit of your account was reached"},
	{platform.ErrPasswordChangeRequired, gen.PasswordChangeRequired, http.StatusForbidden, "Choose a new password first"},
	{platform.ErrAccountDisabled, gen.AccountDisabled, http.StatusForbidden, "This account is disabled"},
	{platform.ErrLastAdmin, gen.LastAdmin, http.StatusConflict, "The instance needs an administrator"},
	{platform.ErrContrast, gen.ContrastFailed, http.StatusUnprocessableEntity, "Some colours are too hard to read"},
	{platform.ErrDisplayNameRequired, gen.DisplayNameRequired, http.StatusUnprocessableEntity, "Set a display name first"},
	{platform.ErrChangedSince, gen.ChangedSince, http.StatusConflict, "Changed since the assistant's change"},
	{friends.ErrInvalid, gen.ValidationFailed, http.StatusUnprocessableEntity, "Validation failed"},
	{friends.ErrAlready, gen.Conflict, http.StatusConflict, "Conflict"},
	{assistant.ErrConsent, gen.ConsentRequired, http.StatusForbidden, "Agreement needed for a cloud connection"},
	{assistant.ErrRunning, gen.RunInProgress, http.StatusConflict, "A reply is still being written"},
	{assistant.ErrBusy, gen.AssistantBusy, http.StatusServiceUnavailable, "The assistant is busy"},
	{assistant.ErrDisabled, gen.AssistantUnavailable, http.StatusServiceUnavailable, "The assistant is unavailable"},
	{assistant.ErrUnavailable, gen.AssistantUnavailable, http.StatusServiceUnavailable, "The assistant is unavailable"},
	{legacyimport.ErrRunning, gen.MigrationRunning, http.StatusConflict, "A migration is already running"},
	{legacyimport.ErrNotFound, gen.NotFound, http.StatusNotFound, "Not found"},
	{ledger.ErrNotFound, gen.NotFound, http.StatusNotFound, "Not found"},
	{ledger.ErrConflict, gen.Conflict, http.StatusConflict, "Conflict"},
	{ledger.ErrState, gen.Conflict, http.StatusConflict, "Conflict"},
	{ledger.ErrInvalid, gen.ValidationFailed, http.StatusUnprocessableEntity, "Validation failed"},
}

// ProblemFor converts any error into problem details. In prod, details of internal errors are
// never exposed; the client gets the request id to quote instead.
func ProblemFor(err error, r *http.Request, prod bool) gen.Problem {
	p := gen.Problem{Code: gen.Internal, Status: http.StatusInternalServerError, Title: "Internal error"}
	var ve *platform.ValidationError
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &ve):
		p.Code, p.Status, p.Title = gen.ValidationFailed, http.StatusUnprocessableEntity, "Validation failed"
		fields := make([]gen.FieldError, 0, len(ve.Fields))
		for _, f := range ve.Fields {
			fields = append(fields, gen.FieldError{Field: f.Field, Message: f.Message})
		}
		p.Errors = &fields
	case errors.As(err, &mbe):
		p.Code, p.Status, p.Title = gen.PayloadTooLarge, http.StatusRequestEntityTooLarge, "Payload too large"
	default:
		for _, k := range problemTable {
			if errors.Is(err, k.err) {
				p.Code, p.Status, p.Title = k.code, k.status, k.title
				break
			}
		}
	}
	var le *instance.LimitError
	if errors.As(err, &le) {
		p.Limit = &gen.ProblemLimit{Name: le.Limit, Value: le.Value}
	}
	var ce *instance.ContrastError
	if errors.As(err, &ce) {
		fails := make([]gen.ContrastFailure, 0, len(ce.Failures))
		for _, f := range ce.Failures {
			fails = append(fails, gen.ContrastFailure{Mode: f.Mode, Fg: f.FG, Bg: f.BG, Ratio: f.Ratio})
		}
		p.Contrast = &fails
	}
	p.Type = "/problems/" + string(p.Code)
	if p.Code != gen.Internal || !prod {
		d := err.Error()
		p.Detail = &d
	}
	if r != nil {
		inst := r.URL.Path
		p.Instance = &inst
		if id := middleware.RequestID(r.Context()); id != "" {
			p.RequestId = &id
		}
	}
	return p
}

// StatusClientClosed is nginx's "client closed request": the client went away before the answer.
// It is not a server error, so the request log, the 5xx metrics and the availability SLO leave it
// out.
const StatusClientClosed = 499

// WriteProblem writes err as application/problem+json and logs internal errors.
func WriteProblem(w http.ResponseWriter, r *http.Request, err error, prod bool) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		w.WriteHeader(StatusClientClosed) // nobody reads a body
		return
	}
	p := ProblemFor(err, r, prod)
	if p.Status >= 500 {
		observability.Logger(r.Context()).LogAttrs(r.Context(), slog.LevelError, "request failed", slog.String("error", err.Error()))
	}
	var ra *platform.RetryAfterError
	if errors.As(err, &ra) {
		w.Header().Set("Retry-After", strconv.Itoa(ra.Seconds()))
	}
	writeProblem(w, p)
}

func writeProblem(w http.ResponseWriter, p gen.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// validation is a 422 validation_failed with one field error.
func validation(field, message string) error {
	ve := &platform.ValidationError{}
	ve.Add(field, message)
	return ve.Err()
}
