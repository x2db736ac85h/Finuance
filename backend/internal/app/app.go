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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"golang.org/x/sync/errgroup"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
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
	pool    *pgxpool.Pool
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
	return &common{log: log, metrics: m, tracing: tr, pool: pool}, nil
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
	q := db.New(c.pool)
	h, err := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, Version: b.Version, Logger: log, Metrics: c.metrics, Tracing: c.tracing,
		Ready: []httpapi.Check{{Name: "database", Probe: store.Ping(q)}, {Name: "migrations", Probe: store.MigrationsCurrent(q)}},
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
	client, err := river.NewClient(riverpgxv5.New(c.pool), &river.Config{
		Logger:  log,
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers: jobs.Workers(),
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	q := db.New(c.pool)
	checks := []httpapi.Check{
		{Name: "database", Probe: store.Ping(q)},
		{Name: "migrations", Probe: store.MigrationsCurrent(q)},
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
