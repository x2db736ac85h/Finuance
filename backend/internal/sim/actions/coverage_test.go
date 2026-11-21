package actions_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/app"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/actions"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
	"github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

var seed = strings.Repeat("lab seed ", 4)

// logBuffer collects the lab's log lines from both processes.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// accessLines are the lab's access lines ("request").
func (l *logBuffer) accessLines(t *testing.T) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(l.b.Bytes()))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var line map[string]any
		if json.Unmarshal(sc.Bytes(), &line) == nil && line["msg"] == "request" {
			out = append(out, line)
		}
	}
	return out
}

type lab struct {
	url, admin string
	logs       *logBuffer
	cfg        config.Config
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// startLab runs a lab in this process: the api and the worker on the test database with
// FINUANCE_SIMULATION, sign-up off, the mock model as the assistant, and the ten animals made by
// finuance admin synthetic-users.
func startLab(t *testing.T) *lab {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	mock := httptest.NewServer(llmmock.New())
	t.Cleanup(mock.Close)
	httpAddr, adminAddr := freeAddr(t), freeAddr(t)
	env := map[string]string{
		"FINUANCE_ENV": "test", "FINUANCE_PUBLIC_ORIGIN": "http://" + httpAddr,
		"FINUANCE_DATABASE_URL": tdb.AppDSN, "FINUANCE_MIGRATE_DATABASE_URL": tdb.OwnerDSN,
		"FINUANCE_HTTP_ADDR": httpAddr, "FINUANCE_ADMIN_ADDR": adminAddr,
		"FINUANCE_SIMULATION": "true", "FINUANCE_ALLOW_SIGNUP": "false",
		"FINUANCE_AI_BASE_URL": mock.URL + "/v1", "FINUANCE_AI_DEFAULT_MODEL": "mock-tools",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok }, config.ForServe)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- app.Serve(ctx, cfg, app.Build{Version: "lab"}, log) }()
	worker := cfg
	worker.AdminAddr = freeAddr(t)
	go func() { done <- app.Worker(ctx, worker, app.Build{Version: "lab"}, log) }()
	t.Cleanup(func() {
		stop()
		for range 2 {
			if err := <-done; err != nil {
				t.Errorf("the lab stopped with %v", err)
			}
		}
	})
	l := &lab{url: "http://" + httpAddr, admin: "http://" + adminAddr, logs: logs, cfg: cfg}
	for i := 0; ; i++ {
		if r, err := http.Get(l.url + "/readyz"); err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		if i > 200 {
			t.Fatal("the lab did not get ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := app.SyntheticUsers(context.Background(), cfg, app.SyntheticInput{Count: len(identity.Animals),
		Seed: strings.NewReader(seed + "\n")}, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	return l
}

// requestsByTraffic sums http_server_requests_total by its traffic label.
func (l *lab) requestsByTraffic(t *testing.T) map[string]float64 {
	t.Helper()
	r, err := http.Get(l.admin + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	out := map[string]float64{}
	re := regexp.MustCompile(`(?m)^http_server_requests_total\{[^}]*traffic="(\w+)"[^}]*\} (\S+)$`)
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		v, _ := strconv.ParseFloat(m[2], 64)
		out[m[1]] += v
	}
	return out
}

// noUser are the routes a persona calls without being anyone yet: a request that carries no user
// is real traffic by definition (ADR 0041), and so is one the router turns away before it reads
// the token.
// pending are the areas whose actions come with pull request 4 of the phase; it empties the list.
var pending = []string{"imports", "labels", "groups", "rules", "budgets", "duplicates", "cash", "assistant", "sharing"}

var noUser = []string{"POST /api/v1/auth/login", "GET /api/v1/meta", "unmatched"}

// TestEveryActionRuns is the coverage test (PLAN 25.4): every action of the catalogue runs once
// against a lab and succeeds; every feature area of the plan's table has an action; and every
// request a persona made as itself is labelled synthetic, on its access line and in the request
// counter.
func TestEveryActionRuns(t *testing.T) {
	l := startLab(t)
	ctx := context.Background()
	e, err := sim.New(sim.Options{Target: l.url, Seed: []byte(seed), Version: "test", Actions: actions.All()})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CheckTarget(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := e.Actor(ctx, identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	from, to := platform.MustDate(2026, time.January, 1), platform.MustDate(2026, time.March, 31)
	for _, account := range []int{statements.Current, statements.Savings} {
		if err := actions.Import(ctx, a, account, from, to); err != nil {
			t.Fatal(err)
		}
	}
	before := l.requestsByTraffic(t)

	areas := map[string]bool{}
	for _, act := range actions.All() {
		areas[act.Area] = true
		t.Run(act.Name, func(t *testing.T) {
			if ev := e.Do(ctx, a, act); ev.Result != sim.ResultOK {
				t.Errorf("%s: %+v", act.Name, ev)
			}
		})
	}
	for _, area := range sim.Areas {
		if !areas[area] && !slices.Contains(pending, area) {
			t.Errorf("the area %q has no action", area)
		}
	}

	lines := l.logs.accessLines(t)
	mine := 0
	for _, line := range lines {
		id, _ := line["request_id"].(string)
		if !strings.HasPrefix(id, "sim-otter-") {
			continue
		}
		mine++
		route, _ := line["route"].(string)
		if line["traffic"] != "synthetic" && !slices.Contains(noUser, route) {
			t.Errorf("a persona's request is not labelled synthetic: %v", line)
		}
	}
	if mine < len(actions.All()) {
		t.Errorf("only %d access lines of the persona's requests", mine)
	}
	after := l.requestsByTraffic(t)
	if after["synthetic"] <= before["synthetic"] {
		t.Errorf("the request counter's synthetic series did not grow: %v → %v", before, after)
	}
	t.Logf("%d access lines of the persona; requests by traffic: %v", mine, after)
}
