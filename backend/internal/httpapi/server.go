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
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// MaxJSONBody is the default request body limit (docs/PLAN.md §6.1).
const MaxJSONBody = 1 << 20

// ShutdownGrace is how long in-flight requests may run after SIGTERM.
const ShutdownGrace = 20 * time.Second

// Deps are the API's collaborators.
type Deps struct {
	Config  config.Config
	Version string
	Logger  *slog.Logger
	Metrics *observability.Metrics
	Tracing *observability.Tracing
	Ready   []Check
	// Auth serves sessions and the /auth, /me and /preferences routes. Nil means every protected
	// route answers 401 (tests of the bare server).
	Auth *auth.Service
	// Clock drives the rate limiters (platform.SystemClock in production).
	Clock platform.Clock
	// Extra registers additional routes on the mux (tests use it for a slow handler).
	Extra func(*http.ServeMux)
}

// NewHandler builds the full handler: middleware chain → mux (health, /api/v1).
func NewHandler(d Deps) (http.Handler, error) {
	prod := d.Config.Env == config.EnvProd
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /readyz", readyz(d.Ready))

	spec, err := gen.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		// The session layer (requireSession) authenticates; the validator only checks the shape.
		Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			ve := &platform.ValidationError{}
			ve.Add("request", err.Error())
			if opts.StatusCode == http.StatusNotFound {
				WriteProblem(w, r, platform.ErrNotFound, prod)
				return
			}
			WriteProblem(w, r, ve, prod)
		},
	})
	clock := d.Clock
	if clock == nil {
		clock = platform.SystemClock{}
	}
	impl := &api{cfg: d.Config, version: d.Version, prod: prod, auth: d.Auth, cookies: newCookies(d.Config), limits: newLimits(clock)}
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
			requireSession(d.Auth, prod),
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
		mw.MaxBytes(MaxJSONBody),
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
