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
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
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
	for _, want := range []string{`http_server_requests_total{method="GET",route="GET /api/v1/meta",status_class="2xx",traffic="real"} 1`,
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
	var logs bytes.Buffer
	log := observability.NewLogger(&logs, config.EnvProd, slog.LevelInfo)
	print := func(f string, a ...any) { out = append(out, f) }
	for _, cmd := range []string{"version", "status", "up"} {
		if err := Migrate(context.Background(), cfg, cmd, log, print); err != nil {
			t.Errorf("migrate %s: %v", cmd, err)
		}
	}
	// what `up` did is a JSON log line like the rest of the pod's output; the answers to a person are printed
	if !strings.Contains(logs.String(), `"msg":"schema up to date; River migrations applied"`) || len(out) == 0 {
		t.Errorf("log = %q, printed = %v", logs.String(), out)
	}
	if err := Migrate(context.Background(), cfg, "sideways", log, print); err == nil {
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

// TestSyntheticUsers: `finuance admin synthetic-users` makes the simulator's users on a lab only,
// with the passwords the simulator derives from the same seed; running it again changes nothing
// (it runs on every deploy), a new seed resets the passwords, and a person's account at a
// persona's address is left alone (PLAN 25.1).
func TestSyntheticUsers(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	tdb.Truncate(t)
	ctx := context.Background()
	var out []string
	log := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	seed := strings.Repeat("s", identity.MinSeedBytes)
	plain := testConfig(t, tdb)
	if err := SyntheticUsers(ctx, plain, SyntheticInput{Count: 3, Seed: strings.NewReader(seed + "\n")}, log); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("on an instance that is not a lab: %v", err)
	}
	svc := NewAuth(store.New(tdb.App), plain)
	if _, err := svc.UserByEmail(ctx, identity.Nth(0).Email); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("a refused run left a user behind: %v", err)
	}

	lab := testConfig(t, tdb)
	lab.Simulation, lab.AllowSignup = true, false
	labAuth := NewAuth(store.New(tdb.App), lab)
	for _, bad := range []SyntheticInput{
		{Count: 0, Seed: strings.NewReader(seed + "\n")},
		{Count: identity.Max + 1, Seed: strings.NewReader(seed + "\n")},
		{Count: 3, Seed: strings.NewReader("too short\n")},
		{Count: 3},
	} {
		if err := SyntheticUsers(ctx, lab, bad, log); err == nil {
			t.Errorf("SyntheticUsers(%d, …) succeeded", bad.Count)
		}
	}
	// a person already uses the fourth persona's address: the run changes nothing at all
	squatter := identity.Nth(3)
	if _, err := labAuth.CreateUser(ctx, auth.SignupInput{Email: squatter.Email, Password: "a person's password"}); err != nil {
		t.Fatal(err)
	}
	if err := SyntheticUsers(ctx, lab, SyntheticInput{Count: 4, Seed: strings.NewReader(seed + "\n")}, log); !errors.Is(err, platform.ErrConflict) {
		t.Errorf("over a person's account: %v", err)
	}
	if u, err := labAuth.UserByEmail(ctx, squatter.Email); err != nil || u.Synthetic {
		t.Errorf("the person's account: %+v, %v", u, err)
	}
	if _, err := labAuth.UserByEmail(ctx, identity.Nth(0).Email); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("a refused run created the first persona: %v", err)
	}
	if _, _, err := labAuth.Login(ctx, squatter.Email, "a person's password", "", auth.Client{}); err != nil {
		t.Errorf("the person's password changed: %v", err)
	}

	out = nil
	if err := SyntheticUsers(ctx, lab, SyntheticInput{Count: 3, Seed: strings.NewReader(seed + "\n")}, log); err != nil {
		t.Fatal(err)
	}
	personas, _ := identity.First(3)
	sessions := map[string]string{}
	for _, p := range personas {
		u, err := labAuth.UserByEmail(ctx, p.Email)
		if err != nil || !u.Synthetic || u.IsAdmin || u.DisplayName != p.Name {
			t.Fatalf("%s: %+v, %v", p.Slug, u, err)
		}
		pw, _ := identity.Password([]byte(seed), p.Slug)
		_, token, err := labAuth.Login(ctx, p.Email, pw, "", auth.Client{})
		if err != nil {
			t.Fatalf("%s signs in with the derived password: %v", p.Slug, err)
		}
		sessions[p.Slug] = token
	}
	// again with the same seed: nothing changes, and the sessions survive
	if err := SyntheticUsers(ctx, lab, SyntheticInput{Count: 3, Seed: strings.NewReader(seed + "\n")}, log); err != nil {
		t.Fatal(err)
	}
	for slug, token := range sessions {
		if _, err := labAuth.Resolve(ctx, token); err != nil {
			t.Errorf("%s's session ended on a second run: %v", slug, err)
		}
	}
	// a new seed: new passwords, the old sessions end
	other := strings.Repeat("t", identity.MinSeedBytes)
	if err := SyntheticUsers(ctx, lab, SyntheticInput{Count: 3, Seed: strings.NewReader(other + "\n")}, log); err != nil {
		t.Fatal(err)
	}
	for _, p := range personas {
		if _, err := labAuth.Resolve(ctx, sessions[p.Slug]); !errors.Is(err, platform.ErrUnauthenticated) {
			t.Errorf("%s's session survived a new seed: %v", p.Slug, err)
		}
		pw, _ := identity.Password([]byte(other), p.Slug)
		if _, _, err := labAuth.Login(ctx, p.Email, pw, "", auth.Client{}); err != nil {
			t.Errorf("%s with the new seed's password: %v", p.Slug, err)
		}
	}
	want := []string{
		"synthetic users: 3 created, 0 with a new password, 0 unchanged",
		"synthetic users: 0 created, 0 with a new password, 3 unchanged",
		"synthetic users: 0 created, 3 with a new password, 0 unchanged",
	}
	if strings.Join(out, "\n") != strings.Join(want, "\n") {
		t.Errorf("output = %q, want %q", out, want)
	}
	if strings.Contains(strings.Join(out, " "), seed) {
		t.Error("the seed was printed")
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
	// A fixed clock a few minutes after midnight in a zone ahead of UTC: the local day and the UTC
	// day differ there, which is where this test used to fail with the system's clock (between
	// 00:00 and 01:00 in Warsaw). The data ends the day before the clock's own day.
	now := time.Date(2026, 11, 20, 0, 3, 0, 0, time.FixedZone("ahead", 3600))
	clock := platform.FixedClock{T: now}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: email, Clock: clock}, log); err != nil {
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
	rows := strings.Count(demo.Recent(now), "\n")
	yesterday := platform.DateOf(now.AddDate(0, 0, -1), now.Location())
	if len(accounts) != 1 || !accounts[0].IsDemo || accounts[0].Name != ledger.DemoAccountName || int(accounts[0].TxCount) != rows ||
		accounts[0].LastOn == nil || *accounts[0].LastOn != yesterday {
		t.Fatalf("accounts = %+v, want one demo account with %d transactions up to %s", accounts, rows, yesterday)
	}
	if err := SeedDemo(ctx, cfg, SeedInput{Email: email, Clock: clock}, log); !errors.Is(err, ledger.ErrConflict) {
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

// TestSeedSyntheticLoadTestUsers: -synthetic marks the load-test users synthetic, so a load test
// stays out of the instance's service-level numbers (PLAN 25.10); only on a lab, and a lab-less
// instance creates nobody.
func TestSeedSyntheticLoadTestUsers(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	log := func(string, ...any) {}
	in := SeedInput{Email: "k6@example.test", Users: 2, Rows: 500, Synthetic: true}

	plain := testConfig(t, tdb)
	in.Password = strings.NewReader("load password 123\n")
	if err := SeedDemo(ctx, plain, in, log); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("-synthetic on an instance that is not a lab: %v", err)
	}
	svc := NewAuth(store.New(tdb.App), plain)
	if _, err := svc.UserByEmail(ctx, "k6+001@example.test"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("a refused run created a user: %v", err)
	}

	lab := testConfig(t, tdb)
	lab.Simulation = true
	in.Password = strings.NewReader("load password 123\n")
	if err := SeedDemo(ctx, lab, in, log); err != nil {
		t.Fatal(err)
	}
	labAuth := NewAuth(store.New(tdb.App), lab)
	for _, email := range []string{"k6+001@example.test", "k6+002@example.test"} {
		u, err := labAuth.UserByEmail(ctx, email)
		if err != nil || !u.Synthetic {
			t.Errorf("%s: %+v, %v", email, u, err)
		}
		if _, _, err := labAuth.Login(ctx, email, "load password 123", "", auth.Client{}); err != nil {
			t.Errorf("%s can't sign in on the lab: %v", email, err)
		}
	}
	// a second run on the lab skips them, still synthetic
	in.Password = strings.NewReader("load password 123\n")
	if err := SeedDemo(ctx, lab, in, log); err != nil {
		t.Errorf("second run: %v", err)
	}
}
