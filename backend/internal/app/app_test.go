package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func testConfig(t *testing.T, tdb *testutil.DB) config.Config {
	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := map[string]string{
			"FINUANCE_ENV": "test", "FINUANCE_PUBLIC_ORIGIN": "http://localhost:8000",
			"FINUANCE_DATABASE_URL": tdb.AppDSN, "FINUANCE_MIGRATE_DATABASE_URL": tdb.OwnerDSN,
			"FINUANCE_HTTP_ADDR": freeAddr(t), "FINUANCE_ADMIN_ADDR": freeAddr(t),
		}[k]
		return v, ok
	}, config.ForServe)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	var last error
	for i := 0; i < 50; i++ { // the server starts asynchronously
		resp, err := http.Get(url)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return resp.StatusCode, string(b)
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("GET %s: %v", url, last)
	return 0, ""
}

func TestServeEndToEnd(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, cfg, Build{Version: "v-test", Commit: "abc"}, slog.New(slog.DiscardHandler))
	}()

	if code, body := get(t, "http://"+cfg.HTTPAddr+"/readyz"); code != 200 || !strings.Contains(body, `"migrations":"ok"`) {
		t.Errorf("readyz: %d %s", code, body)
	}
	if code, body := get(t, "http://"+cfg.HTTPAddr+"/api/v1/meta"); code != 200 || !strings.Contains(body, `"version":"v-test"`) {
		t.Errorf("meta: %d %s", code, body)
	}
	_, metrics := get(t, "http://"+cfg.AdminAddr+"/metrics")
	for _, want := range []string{`http_server_requests_total{method="GET",route="GET /api/v1/meta",status_class="2xx"} 1`,
		"pgxpool_max_conns 10", `finuance_build_info{commit="abc",version="v-test"} 1`} {
		if !strings.Contains(metrics, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
	if code, _ := get(t, "http://"+cfg.HTTPAddr+"/metrics"); code != 404 {
		t.Errorf("/metrics must not be served on the API port (got %d)", code)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestWorkerRunsJobs(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Worker(ctx, cfg, Build{Version: "v-test"}, slog.New(slog.DiscardHandler)) }()
	if code, body := get(t, "http://"+cfg.AdminAddr+"/readyz"); code != 200 || !strings.Contains(body, `"river":"ok"`) {
		t.Errorf("worker readyz: %d %s", code, body)
	}

	// insert-only client, as the API will use (transactional enqueue, ADR 0006)
	ins, err := river.NewClient(riverpgxv5.New(tdb.App), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ins.Insert(ctx, jobs.NoopArgs{Note: "phase 2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		j, err := ins.JobGet(ctx, res.Job.ID)
		if err == nil && j.State == rivertype.JobStateCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job not completed: %+v %v", j, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatalf("Worker: %v", err)
	}
}

func TestMigrateCommands(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	var out []string
	log := func(f string, a ...any) { out = append(out, f) }
	for _, cmd := range []string{"version", "status", "up"} {
		if err := Migrate(context.Background(), cfg, cmd, log); err != nil {
			t.Errorf("migrate %s: %v", cmd, err)
		}
	}
	if err := Migrate(context.Background(), cfg, "sideways", log); err == nil {
		t.Error("unknown command accepted")
	}
}
