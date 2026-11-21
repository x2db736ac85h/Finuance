package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// The browser's errors (PLAN 24.8). Two senders, one counter (finuance_client_errors_total):
//
//   - the web app posts POST /api/v1/client-events for an uncaught error, an unhandled rejection
//     or a screen that failed to draw: the kind, the error's class name, the route pattern and the
//     app's version, each held to a pattern by the contract, so none of them can be a message;
//   - the browser itself posts content security policy violations to cspReportPath (the policy's
//     report-uri and report-to). Such a report comes without the client header and in the
//     browser's own format, so it has its own route outside the CSRF check; it changes no state,
//     and its body is read and thrown away: a report quotes URLs, which are not logged.
//
// Both work without a session and are limited per client address.
const (
	maxClientEvent = 1 << 10 // 1 KiB
	maxCSPReport   = 8 << 10 // a report with a long policy is a few KiB
	cspReportPath  = "/api/v1/csp-reports"
)

// ReportClientEvent counts one error of the web app and writes one line.
func (a *api) ReportClientEvent(ctx context.Context, req gen.ReportClientEventRequestObject) (gen.ReportClientEventResponseObject, error) {
	if err := a.limits.clientEvents.Allow(mw.ClientKey(mw.FromContext(ctx).ClientIP)); err != nil {
		return nil, err
	}
	kind := string(req.Body.Kind)
	a.clientError(kind)
	name, version := "", ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	if req.Body.Version != nil {
		version = *req.Body.Version
	}
	observability.Logger(ctx).WarnContext(ctx, "client error", "event", kind, "error_name", name, "screen", req.Body.Route, "web_version", version)
	return gen.ReportClientEvent204Response{}, nil
}

func (a *api) clientError(kind string) {
	if a.metrics != nil {
		a.metrics.ClientErrors.WithLabelValues(kind).Inc()
	}
}

// cspReport takes a content security policy report from a browser: counted, never parsed or logged.
func (a *api) cspReport(w http.ResponseWriter, r *http.Request) {
	if err := a.limits.clientEvents.Allow(mw.ClientKey(mw.FromContext(r.Context()).ClientIP)); err != nil {
		WriteProblem(w, r, err, a.prod)
		return
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil { // over the size limit, or a broken upload
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	a.clientError("csp")
	observability.Logger(r.Context()).WarnContext(r.Context(), "client error", "event", "csp")
	w.WriteHeader(http.StatusNoContent)
}
