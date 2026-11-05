// Package httpapi maps HTTP to the domain: the generated OpenAPI server, middleware, health and
// problem+json errors. No SQL lives here.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/jackc/pgx/v5"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
)

// MaxJSONBody is the default request body limit (docs/PLAN.md §6.1).
const MaxJSONBody = 1 << 20

// multipartOverhead is room for the multipart framing and the accountId field around an upload;
// the file itself is checked against FINUANCE_MAX_UPLOAD_BYTES exactly.
const multipartOverhead = 64 << 10

// ShutdownGrace is how long in-flight requests may run after SIGTERM.
const ShutdownGrace = 20 * time.Second

// Deps are the API's collaborators.
type Deps struct {
	Config  config.Config
	Version string
	// Commit and BuildDate come from the build (ldflags), shown in the Developer section.
	Commit, BuildDate string
	// SchemaVersion reads the database's migration version for the Developer section; nil = 0.
	SchemaVersion func(context.Context) (int64, error)
	Logger        *slog.Logger
	Metrics       *observability.Metrics
	Tracing       *observability.Tracing
	Ready         []Check
	// Auth serves sessions and the /auth, /me and /preferences routes. Nil means every protected
	// route answers 401 (tests of the bare server).
	Auth *auth.Service
	// Clock drives the rate limiters (platform.SystemClock in production).
	Clock platform.Clock
	// Jobs is the api's insert-only River client for transactional enqueue.
	Jobs *river.Client[pgx.Tx]
	// Ledger serves accounts and imports. Nil in tests of the bare server.
	Ledger *ledger.Ledger
	// Query runs the transaction reads (query, distinct, export). Nil in tests of the bare server.
	Query *query.Engine
	// Assistant is the assistant (connections, chats, runner); nil or without connections it is off.
	Assistant *assistant.Assistant
	// Legacy runs migrations from the single-file app. Nil in tests of the bare server.
	Legacy *legacyimport.Migrator
	// Rules stores the categorisation rules. Nil in tests of the bare server.
	Rules *rules.Service
	// Admin runs the instance settings, limits, themes and the admin routes (ADR 0025). Nil in tests
	// of the bare server: every feature on, limits from the environment, admin routes 404.
	Admin *admin.Service
	// Friends runs friend codes and friendships, Notify the in-app notifications (Phase 19). Nil in
	// tests of the bare server: the routes answer 404.
	Friends *friends.Service
	Notify  *notify.Service
	// Sharing runs shared groups (ADR 0028). Nil in tests of the bare server: the routes answer 404.
	Sharing *sharing.Service
	// Extra registers additional routes on the mux (tests use it for a slow handler).
	Extra func(*http.ServeMux)
}

// NewHandler builds the full handler: middleware chain → mux (health, /api/v1).
func NewHandler(d Deps) (http.Handler, error) {
	prod := d.Config.Env == config.EnvProd
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)

	spec, err := gen.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		// The session layer (requireSession) authenticates; the validator only checks the shape.
		Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			if opts.StatusCode == http.StatusNotFound {
				WriteProblem(w, r, platform.ErrNotFound, prod)
				return
			}
			WriteProblem(w, r, requestProblem(err), prod)
		},
	})
	clock := d.Clock
	if clock == nil {
		clock = platform.SystemClock{}
	}
	mux.Handle("GET /readyz", readyz(d.Ready, clock))
	var ins *insights.Service
	if d.Query != nil {
		ins = insights.New(d.Query)
	}
	impl := &api{admin: d.Admin, friends: d.Friends, notify: d.Notify, sharing: d.Sharing, insights: ins, ai: d.Assistant, cfg: d.Config, version: d.Version, commit: d.Commit, buildDate: d.BuildDate, schemaVersion: d.SchemaVersion, prod: prod, auth: d.Auth, ledger: d.Ledger, legacy: d.Legacy, rules: d.Rules, query: d.Query, jobs: d.Jobs, cookies: newCookies(d.Config), limits: newLimits(clock, d.Config.AIRatePerHour)}
	strict := gen.NewStrictHandlerWithOptions(impl, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			ve := &platform.ValidationError{}
			ve.Add("body", err.Error())
			WriteProblem(w, r, ve, prod)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { WriteProblem(w, r, err, prod) },
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		// Per matched route; the last one runs first: CSRF → session → request validation → handler.
		Middlewares: []gen.MiddlewareFunc{
			validator,
			requireSession(d.Auth, d.Admin, prod),
			gen.MiddlewareFunc(mw.CSRF(d.Config.PublicOrigin, func(w http.ResponseWriter, r *http.Request, err error) {
				WriteProblem(w, r, fmt.Errorf("%w: %w", platform.ErrForbidden, err), prod)
			})),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			ve := &platform.ValidationError{}
			ve.Add("parameters", err.Error())
			WriteProblem(w, r, ve, prod)
		},
	})
	notFound := func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, fmt.Errorf("%s: %w", r.URL.Path, platform.ErrNotFound), prod)
	}
	mux.HandleFunc("/api/v1/", notFound)
	mux.HandleFunc("/", notFound)
	if d.Extra != nil {
		d.Extra(mux)
	}

	return mw.Chain(mux,
		mw.Recover(d.Logger, func(w http.ResponseWriter, r *http.Request, err error) { WriteProblem(w, r, err, prod) }),
		mw.AssignRequestID,
		mw.RealIP(d.Config.TrustedProxies),
		mw.Logging(d.Logger),
		mw.Metrics(d.Metrics),
		mw.Tracing(d.Tracing),
		mw.SecurityHeaders,
		mw.MaxBytes(MaxJSONBody, map[string]int64{"POST /api/v1/imports": d.Config.MaxUploadBytes + multipartOverhead,
			"POST /api/v1/legacy-imports": legacyBodyLimit(d.Config.MaxUploadBytes)}),
		mw.Route,
	), nil
}

// NewServer applies the timeouts from docs/PLAN.md task 2.4.
func NewServer(addr string, h http.Handler, l *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(l.Handler(), slog.LevelWarn),
	}
}

// Serve runs srv on ln until ctx is cancelled, then stops accepting connections and lets in-flight
// requests finish for up to grace.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// requestProblem classifies a request-validation error: a body over the size limit is 413, a body
// of a media type the operation doesn't accept is 415, anything else is a 422 on "request".
func requestProblem(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return fmt.Errorf("the request body is larger than %d bytes: %w", mbe.Limit, platform.ErrPayloadTooLarge)
	}
	var re *openapi3filter.RequestError
	if errors.As(err, &re) && re.RequestBody != nil && strings.HasPrefix(re.Reason, "header Content-Type has unexpected value") {
		return fmt.Errorf("%s: %w", re.Reason, platform.ErrUnsupportedFile)
	}
	ve := &platform.ValidationError{}
	ve.Add("request", err.Error())
	return ve
}
