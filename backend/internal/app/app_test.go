package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
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
	// The hourly session cleanup: an expired session is deleted, a live one stays.
	st := store.New(tdb.App)
	user := platform.NewID()
	expired, live := bytes.Repeat([]byte{0xE1}, 32), bytes.Repeat([]byte{0xA1}, 32)
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.CreateUser(ctx, db.CreateUserParams{ID: user, Email: user.String() + "@worker.test", PasswordHash: "x"}); err != nil {
			return err
		}
		long := time.Now().Add(-60 * 24 * time.Hour)
		if err := q.CreateSession(ctx, db.CreateSessionParams{TokenHash: expired, UserID: user, Now: long, ExpiresAt: long.Add(time.Hour)}); err != nil {
			return err
		}
		return q.CreateSession(ctx, db.CreateSessionParams{TokenHash: live, UserID: user, Now: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}
	res, err = ins.Insert(ctx, jobs.CleanupExpiredSessionsArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		j, err := ins.JobGet(ctx, res.Job.ID)
		if err == nil && j.State == rivertype.JobStateCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup job not completed: %+v %v", j, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var left [][]byte
	_ = st.WithoutUser(ctx, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT token_hash FROM sessions WHERE user_id = $1`, user)
		if err == nil {
			left, err = pgx.CollectRows(rows, pgx.RowTo[[]byte])
		}
		return err
	})
	if len(left) != 1 || !bytes.Equal(left[0], live) {
		t.Errorf("sessions after cleanup = %x", left)
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

func TestAdminCommands(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	cfg.AllowSignup = false // admin create-user works on closed instances
	ctx := context.Background()
	var out []string
	log := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	email := "admin-cli@example.test"
	if err := Admin(ctx, cfg, AdminInput{Command: "create-user", Email: email, DisplayName: "Admin", Password: strings.NewReader("first password 123\n")}, log); err != nil {
		t.Fatal(err)
	}
	svc := NewAuth(store.New(tdb.App), cfg)
	if _, _, err := svc.Login(ctx, email, "first password 123", "", auth.Client{}); err != nil {
		t.Fatalf("login with the created password: %v", err)
	}
	_, token, _ := svc.Login(ctx, email, "first password 123", "", auth.Client{})
	if err := Admin(ctx, cfg, AdminInput{Command: "set-password", Email: email, Password: strings.NewReader("second password 456\r\n")}, log); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, token); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("session survived set-password: %v", err)
	}
	if _, _, err := svc.Login(ctx, email, "second password 456", "", auth.Client{}); err != nil {
		t.Errorf("login with the new password (CRLF stripped): %v", err)
	}
	for _, bad := range []AdminInput{
		{Command: "create-user", Email: email, Password: strings.NewReader("another password\n")}, // taken
		{Command: "create-user", Email: "x@example.test", Password: strings.NewReader("short\n")}, // too short
		{Command: "set-password", Email: "nobody@example.test", Password: strings.NewReader("whatever password\n")},
		{Command: "create-user", Email: "y@example.test", Password: strings.NewReader("\n")},
		{Command: "rename-user", Email: "y@example.test", Password: strings.NewReader("whatever password\n")},
	} {
		if err := Admin(ctx, cfg, bad, log); err == nil {
			t.Errorf("Admin(%s, %s) succeeded", bad.Command, bad.Email)
		}
	}
	if len(out) != 2 || !strings.Contains(out[0], "created user "+email) || strings.Contains(strings.Join(out, " "), "password 123") {
		t.Errorf("output = %q", out)
	}
}

func TestSeedDemo(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	ctx := context.Background()
	var out []string
	log := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	email := "seed-demo@example.test"
	if err := Admin(ctx, cfg, AdminInput{Command: "create-user", Email: email, Password: strings.NewReader("seed password 123\n")}, log); err != nil {
		t.Fatal(err)
	}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: email}, log); err != nil {
		t.Fatal(err)
	}
	st := store.New(tdb.App)
	u, err := NewAuth(st, cfg).UserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := ledger.New(st, platform.SystemClock{}).ListAccounts(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Every demo row is distinct (unique DEMO references), so each one becomes a transaction.
	rows := strings.Count(demo.Recent(time.Now()), "\n")
	yesterday := platform.DateOf(time.Now().UTC().AddDate(0, 0, -1), time.UTC)
	if len(accounts) != 1 || !accounts[0].IsDemo || accounts[0].Name != ledger.DemoAccountName || int(accounts[0].TxCount) != rows ||
		accounts[0].LastOn == nil || *accounts[0].LastOn != yesterday {
		t.Fatalf("accounts = %+v, want one demo account with %d transactions up to %s", accounts, rows, yesterday)
	}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: email}, log); !errors.Is(err, ledger.ErrConflict) {
		t.Errorf("second seed: %v, want a conflict", err)
	}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: "nobody@example.test"}, log); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if len(out) != 2 || !strings.Contains(out[1], fmt.Sprintf("%d transactions", rows)) {
		t.Errorf("output = %q", out)
	}
}

// TestSeedLoadTestUsers: -users creates numbered users with the given password and gives each
// about -rows demo rows; a second run skips them (PLAN 17.5).
func TestSeedLoadTestUsers(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	cfg := testConfig(t, tdb)
	ctx := context.Background()
	var out []string
	log := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	in := SeedInput{Email: "load@example.test", Users: 2, Rows: 3000, Password: strings.NewReader("load password 123\n")}
	if err := SeedDemo(ctx, cfg, in, log); err != nil {
		t.Fatal(err)
	}
	st := store.New(tdb.App)
	svc := NewAuth(st, cfg)
	for _, email := range []string{"load+001@example.test", "load+002@example.test"} {
		u, err := svc.UserByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.Login(ctx, email, "load password 123", "", auth.Client{}); err != nil {
			t.Errorf("%s can't sign in: %v", email, err)
		}
		accounts, err := ledger.New(st, platform.SystemClock{}).ListAccounts(ctx, u.ID)
		if err != nil || len(accounts) != 1 || accounts[0].TxCount < 2700 || accounts[0].TxCount > 3300 {
			t.Errorf("%s accounts = %+v, %v (want about 3,000 rows)", email, accounts, err)
		}
	}
	in.Password = strings.NewReader("load password 123\n")
	out = nil
	if err := SeedDemo(ctx, cfg, in, log); err != nil || len(out) != 2 || !strings.Contains(out[0], "already has demo data") {
		t.Errorf("second run: %v, %q", err, out)
	}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: "no-domain", Users: 1, Password: strings.NewReader("x\n")}, log); err == nil {
		t.Error("an email without a domain was accepted")
	}
}
