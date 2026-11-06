// Package app is the composition root: config → dependencies → servers. Each subcommand of
// cmd/finuance has one entry point here.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"golang.org/x/sync/errgroup"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/advice"
	"github.com/x2db736ac85h/finuance/backend/internal/aichanges"
	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

// Build identifies the binary (set with -ldflags in cmd/finuance).
type Build struct {
	Version, Commit, Date string
}

// common holds what every long-running process needs.
type common struct {
	log     *slog.Logger
	metrics *observability.Metrics
	tracing *observability.Tracing
	pool    *pgxpool.Pool // for River; data access goes through store
	store   *store.Store
}

func setup(ctx context.Context, cfg config.Config, b Build, log *slog.Logger, service string) (*common, error) {
	m := observability.NewMetrics()
	m.BuildInfo.WithLabelValues(b.Version, b.Commit).Set(1)
	tr, err := observability.NewTracing(ctx, cfg.OTLPEndpoint, service, b.Version)
	if err != nil {
		return nil, err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns, tr.Provider)
	if err != nil {
		return nil, err
	}
	m.Registry.MustRegister(store.NewPoolCollector(pool))
	return &common{log: log, metrics: m, tracing: tr, pool: pool, store: store.New(pool)}, nil
}

func (c *common) close(ctx context.Context) {
	c.pool.Close()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := c.tracing.Shutdown(ctx); err != nil {
		c.log.Warn("trace flush", "error", err)
	}
}

// Serve runs the API and the admin server until ctx is cancelled (SIGINT/SIGTERM), then drains.
func Serve(ctx context.Context, cfg config.Config, b Build, log *slog.Logger) error {
	c, err := setup(ctx, cfg, b, log, "finuance-api")
	if err != nil {
		return err
	}
	defer c.close(ctx)
	authSvc := NewAuth(c.store, cfg)
	led := ledger.New(c.store, platform.SystemClock{})
	// Insert-only River client (no queues): handlers enqueue jobs in the same transaction as the
	// state change (ADR 0006); the worker process runs them.
	jobClient, err := river.NewClient(riverpgxv5.New(c.pool), &river.Config{Logger: log, Workers: jobs.Workers(jobs.Deps{Sessions: authSvc, Ledger: led}),
		// the request's trace context travels with every job it enqueues (PLAN 17.2)
		Middleware: []rivertype.Middleware{jobs.NewTelemetry(c.tracing.Provider, c.tracing.Propagator, nil)}})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	conns := assistant.NewConnections(cfg.AIConnections, cfg.AITimeout, cfg.AIIdleTimeout, platform.SystemClock{}, nil)
	warnings, err := conns.CheckHosts(ctx, nil)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		log.Warn(w)
	}
	notes := notify.New(c.store, platform.SystemClock{})
	adm := NewAdmin(c.store, authSvc, cfg)
	h, err := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, Version: b.Version, Commit: b.Commit, BuildDate: b.Date, SchemaVersion: c.store.SchemaVersion, Logger: log, Metrics: c.metrics, Tracing: c.tracing,
		Auth: authSvc, Admin: adm, Advice: advice.New(c.store, platform.SystemClock{}), Friends: friends.New(c.store, platform.SystemClock{}, notes), Notify: notes, Sharing: sharing.New(c.store, led, notes, platform.SystemClock{}), AIChanges: aichanges.New(c.store, led, platform.SystemClock{}), Goals: goals.New(c.store, platform.SystemClock{}), Clock: platform.SystemClock{}, Jobs: jobClient, Ledger: led, Query: query.New(c.store),
		Legacy: legacyimport.New(c.store, led, nil, platform.SystemClock{}),
		Rules:  rules.New(c.store, platform.SystemClock{}),
		Assistant: assistant.New(assistant.Config{Store: c.store, Query: query.New(c.store), Connections: conns, Clock: platform.SystemClock{},
			MaxChats: cfg.AIMaxChats, MaxConcurrent: cfg.AIMaxConcurrent, Metrics: assistant.NewMetrics(c.metrics.Registry),
			Tracer: c.tracing.Provider, Logger: log, Instructions: instructions(adm)}),
		Ready: []httpapi.Check{{Name: "database", Probe: c.store.Ping}, {Name: "migrations", Probe: c.store.MigrationsCurrent}},
	})
	if err != nil {
		return err
	}
	api := httpapi.NewServer(cfg.HTTPAddr, h, log)
	admin := observability.NewAdminServer(cfg.AdminAddr, c.metrics, nil)
	log.Info("finuance api starting", "version", b.Version, "http", cfg.HTTPAddr, "admin", cfg.AdminAddr, "env", cfg.Env)
	return serveAll(ctx, log, api, admin)
}

// Worker runs the River client plus an admin server with /metrics, /healthz and /readyz.
func Worker(ctx context.Context, cfg config.Config, b Build, log *slog.Logger) error {
	c, err := setup(ctx, cfg, b, log, "finuance-worker")
	if err != nil {
		return err
	}
	defer c.close(ctx)
	led := ledger.New(c.store, platform.SystemClock{})
	chats := assistant.NewChats(c.store, platform.SystemClock{}, cfg.AIMaxChats)
	authSvc := NewAuth(c.store, cfg)
	notes := notify.New(c.store, platform.SystemClock{})
	digests, err := newDigests(ctx, c, cfg, authSvc, notes, log)
	if err != nil {
		return err
	}
	client, err := river.NewClient(riverpgxv5.New(c.pool), &river.Config{
		Logger: log,
		// digests one at a time (ADR 0033): they share a small model server with the questions
		Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}, jobs.DigestQueue: {MaxWorkers: 1}},
		Workers: jobs.Workers(jobs.Deps{Sessions: authSvc, Demo: authSvc, Ledger: led, Audit: led, Chats: chats,
			Migrator: legacyimport.New(c.store, led, chats, platform.SystemClock{}),
			Rules:    rules.New(c.store, platform.SystemClock{}),
			Friends:  friends.New(c.store, platform.SystemClock{}, notes),
			Notify:   notes,
			Digests:  digests,
			Metrics:  jobs.NewImportMetrics(c.metrics.Registry)}),
		PeriodicJobs: jobs.PeriodicJobs(),
		Middleware: []rivertype.Middleware{jobs.NewTelemetry(c.tracing.Provider, c.tracing.Propagator,
			jobs.NewJobMetrics(c.metrics.Registry))},
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	checks := []httpapi.Check{
		{Name: "database", Probe: c.store.Ping},
		{Name: "migrations", Probe: c.store.MigrationsCurrent},
		{Name: "river", Probe: func(context.Context) error {
			select {
			case <-client.Stopped():
				return errors.New("river client stopped")
			default:
				return nil
			}
		}},
	}
	admin := observability.NewAdminServer(cfg.AdminAddr, c.metrics, func(mux *http.ServeMux) {
		mux.Handle("GET /healthz", httpapi.HealthzHandler())
		mux.Handle("GET /readyz", httpapi.ReadyzHandler(checks))
	})
	log.Info("finuance worker starting", "version", b.Version, "admin", cfg.AdminAddr)
	err = serveAll(ctx, log, admin)
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpapi.ShutdownGrace)
	defer cancel()
	if serr := client.Stop(stopCtx); serr != nil {
		err = errors.Join(err, fmt.Errorf("stop river: %w", serr))
	}
	return err
}

// newDigests is the worker's digest service (ADR 0033), or nil when no model connection is
// configured. The worker runs the assistant for digests only, one run at a time.
func newDigests(ctx context.Context, c *common, cfg config.Config, authSvc *auth.Service, notes *notify.Service, log *slog.Logger) (jobs.DigestWriter, error) {
	conns := assistant.NewConnections(cfg.AIConnections, cfg.AITimeout, cfg.AIIdleTimeout, platform.SystemClock{}, nil)
	if !conns.Enabled() {
		return nil, nil
	}
	warnings, err := conns.CheckHosts(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		log.Warn(w)
	}
	adm := NewAdmin(c.store, authSvc, cfg)
	ai := assistant.New(assistant.Config{Store: c.store, Query: query.New(c.store), Connections: conns, Clock: platform.SystemClock{},
		MaxChats: cfg.AIMaxChats, MaxConcurrent: 1, Metrics: assistant.NewMetrics(c.metrics.Registry), Tracer: c.tracing.Provider, Logger: log,
		Instructions: instructions(adm)})
	policy := func(ctx context.Context, id uuid.UUID) (assistant.DigestPolicy, error) {
		if cfg.DemoMode {
			return assistant.DigestPolicy{}, nil
		}
		u, err := authSvc.UserByID(ctx, id)
		if errors.Is(err, platform.ErrNotFound) { // deleted since the digest was queued
			return assistant.DigestPolicy{}, nil
		}
		if err != nil || u.IsDemo || u.Disabled {
			return assistant.DigestPolicy{}, err
		}
		f, err := adm.Features(ctx)
		if err != nil || !f.AssistantDigests {
			return assistant.DigestPolicy{}, err
		}
		l, err := adm.Limits(ctx, u)
		return assistant.DigestPolicy{Allowed: true, TokensPerDay: l.AssistantTokensPerDay}, err
	}
	return assistant.NewDigests(assistant.DigestsConfig{Assistant: ai, Policy: policy, Notify: notes, Clock: platform.SystemClock{},
		Logger: log}), nil
}

// instructions reads the administrator's addition to the assistant's system prompt (ADR 0034);
// unreadable settings mean none.
func instructions(adm *admin.Service) func(context.Context) string {
	return func(ctx context.Context) string {
		s, _, err := adm.Settings(ctx)
		if err != nil || s.Assistant == nil {
			return ""
		}
		return s.Assistant.Instructions
	}
}

// serveAll runs every server until ctx ends, then shuts them all down within the grace period.
func serveAll(ctx context.Context, log *slog.Logger, servers ...*http.Server) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, s := range servers {
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", s.Addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", s.Addr, err)
		}
		g.Go(func() error { return httpapi.Serve(gctx, s, ln, httpapi.ShutdownGrace) })
	}
	err := g.Wait()
	log.Info("finuance stopped")
	return err
}

// Migrate runs `finuance migrate up|down|status|version` as the owner role.
func Migrate(ctx context.Context, cfg config.Config, cmd string, out func(format string, args ...any)) error {
	pool, err := pgxpool.New(ctx, cfg.MigrateDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect: invalid FINUANCE_MIGRATE_DATABASE_URL")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect as owner: %w", err)
	}
	m, err := store.NewMigrator(pool)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }()
	switch cmd {
	case "up":
		res, err := m.Up(ctx)
		for _, r := range res {
			out("applied %s (%s)", r.Source.Path, r.Duration.Round(time.Millisecond))
		}
		if err == nil {
			v, _ := m.Version(ctx)
			out("schema at version %d; River migrations applied", v)
		}
		return err
	case "down":
		r, err := m.Down(ctx)
		if r != nil {
			out("rolled back %s", r.Source.Path)
		} else if err == nil {
			out("nothing to roll back")
		}
		return err
	case "status":
		st, err := m.Status(ctx)
		for _, s := range st {
			applied := "pending"
			if s.State == "applied" {
				applied = "applied " + s.AppliedAt.UTC().Format(time.RFC3339)
			}
			out("%-40s %s", s.Source.Path, applied)
		}
		return err
	case "version":
		v, err := m.Version(ctx)
		if err == nil {
			out("%d (binary expects %d)", v, store.ExpectedVersion())
		}
		return err
	}
	return fmt.Errorf("unknown migrate command %q (want up, down, status or version)", cmd)
}

// Healthcheck GETs /healthz on the local API port; distroless has no shell or curl.
func Healthcheck(ctx context.Context, cfg config.Config) error {
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

// NewAdmin returns the administration service with the environment's outer limits (ADR 0025).
func NewAdmin(st *store.Store, a *auth.Service, cfg config.Config) *admin.Service {
	return admin.New(st, a, platform.SystemClock{}, instance.Bounds{MaxUploadBytes: cfg.MaxUploadBytes, AssistantPerHour: int64(cfg.AIRatePerHour),
		AssistantTokensPerDay: cfg.AITokensPerDay})
}

// NewAuth builds the auth service from the configuration.
func NewAuth(st *store.Store, cfg config.Config) *auth.Service {
	return auth.New(st, platform.SystemClock{}, auth.Config{
		Params:         auth.Params{MemoryKiB: cfg.Argon2MemoryKiB, Time: cfg.Argon2Time, Threads: cfg.Argon2Threads},
		SessionTTL:     cfg.SessionTTL,
		SessionIdleTTL: cfg.SessionIdleTTL,
		AllowSignup:    cfg.AllowSignup,
	})
}
