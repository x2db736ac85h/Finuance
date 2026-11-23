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
	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
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
	tdb.Truncate(t) // every run starts without friendships, groups or chats
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

// repeatable is the default configuration with a fixed run seed: the same draws on every run.
func repeatable() sim.Config {
	c := sim.Defaults()
	c.RunSeed = 1
	return c
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
	// a route's label holds braces ({id}), so the labels end at the line's last "} "
	line := regexp.MustCompile(`(?m)^http_server_requests_total\{(.*)\} (\S+)$`)
	traffic := regexp.MustCompile(`traffic="(\w+)"`)
	for _, m := range line.FindAllStringSubmatch(string(body), -1) {
		v, _ := strconv.ParseFloat(m[2], 64)
		if tm := traffic.FindStringSubmatch(m[1]); tm != nil {
			out[tm[1]] += v
		}
	}
	return out
}

// noUser are the routes a persona calls without being anyone yet: a request that carries no user
// is real traffic by definition (ADR 0041), and so is one the router turns away before it reads
// the token.
var noUser = []string{"POST /api/v1/auth/login", "GET /api/v1/meta", "unmatched"}

// TestEveryActionRuns is the coverage test (PLAN 25.4): every action of the catalogue runs once
// against a lab and succeeds; every feature area of the plan's table has an action; and every
// request a persona made as itself is labelled synthetic, on its access line and in the request
// counter.
func TestEveryActionRuns(t *testing.T) {
	l := startLab(t)
	ctx := context.Background()
	e, err := sim.New(sim.Options{Target: l.url, Seed: []byte(seed), Version: "test", Actions: actions.All(), Config: repeatable()})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CheckTarget(ctx); err != nil {
		t.Fatal(err)
	}
	otter, err := e.Actor(ctx, identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	owl, err := e.Actor(ctx, identity.Nth(1))
	if err != nil {
		t.Fatal(err)
	}
	from, to := platform.MustDate(2026, time.January, 1), platform.MustDate(2026, time.March, 31)
	for _, account := range []int{statements.Current, statements.Savings} {
		if err := actions.Import(ctx, otter, account, from, to); err != nil {
			t.Fatal(err)
		}
	}
	before := l.requestsByTraffic(t)

	byName := map[string]sim.Action{}
	for _, act := range actions.All() {
		byName[act.Name] = act
	}
	who := map[string]*sim.Actor{"otter": otter, "owl": owl}
	ok := map[string]bool{}
	for _, step := range script {
		act, found := byName[step.action]
		if !found {
			t.Fatalf("the script names %q, which is not an action", step.action)
		}
		if step.before != nil {
			step.before(t, ctx, who[step.who])
		}
		var ev sim.Event
		for range 5 { // a draw can find nothing to act on (a search without rows): try again
			if ev = e.Do(ctx, who[step.who], act); ev.Result != sim.ResultSkipped {
				break
			}
		}
		if ev.Result != sim.ResultOK {
			t.Errorf("%s %s: %+v", step.who, step.action, ev)
			continue
		}
		ok[step.action] = true
	}
	areas := map[string]bool{}
	for _, act := range actions.All() {
		areas[act.Area] = true
		if !ok[act.Name] {
			t.Errorf("the action %s did not run successfully", act.Name)
		}
	}
	for _, area := range sim.Areas {
		if !areas[area] {
			t.Errorf("the area %q has no action", area)
		}
	}

	lines := l.logs.accessLines(t)
	synthetic := 0.0
	for _, line := range lines {
		if line["traffic"] == "synthetic" {
			synthetic++
		}
	}
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
	if mine < len(script) {
		t.Errorf("only %d access lines of the persona's requests", mine)
	}
	after := l.requestsByTraffic(t)
	if after["synthetic"] <= before["synthetic"] || after["synthetic"] != synthetic {
		t.Errorf("the request counter's synthetic series (%v → %v) does not match the %v synthetic access lines", before, after, synthetic)
	}
	t.Logf("%d access lines of the persona; requests by traffic: %v", mine, after)
}

// step is one action of the coverage script, by one persona; before prepares what it needs.
type step struct {
	who, action string
	before      func(t *testing.T, ctx context.Context, a *sim.Actor)
}

// script runs every action in an order where each finds what it needs: imports first, the
// group before it is used, the proposal before its undo, a friend code before it is redeemed, an
// invitation before it is accepted.
var script = []step{
	{who: "otter", action: "imports.month"}, {who: "otter", action: "imports.overlap"}, {who: "otter", action: "imports.remap"},
	{who: "otter", action: "imports.abandon"}, {who: "otter", action: "imports.again"}, {who: "otter", action: "imports.broken"},
	{who: "otter", action: "table.window"}, {who: "otter", action: "table.filter"}, {who: "otter", action: "table.search"},
	{who: "otter", action: "table.distinct"}, {who: "otter", action: "table.range"},
	{who: "otter", action: "insights.charts"}, {who: "otter", action: "insights.overview"},
	{who: "otter", action: "labels.category"}, {who: "otter", action: "labels.note"}, {who: "otter", action: "labels.bulk-category"},
	{who: "otter", action: "groups.create"}, {who: "otter", action: "labels.bulk-group"}, {who: "otter", action: "groups.rename"},
	{who: "otter", action: "groups.timeline"}, {who: "otter", action: "groups.report"}, {who: "otter", action: "groups.create"},
	{who: "otter", action: "groups.delete"},
	{who: "otter", action: "rules.suggestion"}, {who: "otter", action: "rules.preview"}, {who: "otter", action: "rules.apply"},
	{who: "otter", action: "rules.category"}, {who: "otter", action: "rules.merge", before: twoOwnCategories},
	{who: "otter", action: "budgets.read"}, {who: "otter", action: "budgets.set"}, {who: "otter", action: "budgets.goal"},
	{who: "otter", action: "budgets.goal-update"},
	{who: "otter", action: "duplicates.list"}, {who: "otter", action: "duplicates.prefer"}, {who: "otter", action: "duplicates.keep-both"},
	{who: "otter", action: "duplicates.clear"},
	{who: "otter", action: "cash.add"}, {who: "otter", action: "cash.edit"}, {who: "otter", action: "cash.add"},
	{who: "otter", action: "cash.delete"}, {who: "otter", action: "cash.receipt"},
	{who: "otter", action: "assistant.ask"}, {who: "otter", action: "assistant.stop"}, {who: "otter", action: "assistant.apply"},
	{who: "otter", action: "assistant.revert"}, {who: "otter", action: "assistant.digest"},
	{who: "owl", action: "sharing.code"}, {who: "otter", action: "sharing.befriend"}, {who: "otter", action: "sharing.group"},
	{who: "owl", action: "sharing.accept"}, {who: "otter", action: "sharing.expense"}, {who: "otter", action: "sharing.settle"},
	{who: "otter", action: "sharing.report"}, {who: "otter", action: "sharing.group"}, {who: "owl", action: "sharing.accept"},
	{who: "owl", action: "sharing.leave"},
	{who: "otter", action: "account.preferences"}, {who: "otter", action: "account.notifications"}, {who: "otter", action: "account.export"},
	{who: "otter", action: "account.wrong-password"}, {who: "otter", action: "account.unknown-path"},
	{who: "owl", action: "history.rebuild"}, // last: it makes Owl's accounts again
}

// twoOwnCategories makes sure two of the categories personas make exist, for a merge.
func twoOwnCategories(t *testing.T, ctx context.Context, a *sim.Actor) {
	t.Helper()
	for _, name := range []string{"Hobby", "Podróże"} {
		r, err := a.API.CreateCategoryWithResponse(ctx, nil, gen.CreateCategoryJSONRequestBody{Name: name})
		if err != nil || (r.StatusCode() != http.StatusCreated && r.StatusCode() != http.StatusConflict) {
			t.Fatalf("make %s: %v %v", name, r.StatusCode(), err)
		}
	}
}

// TestEmptyStatementIsNotCommitted (found in the day in the lab, 25.12): a week of a savings account
// can have no row at all, and the server rightly refuses to commit such a statement (409). Import
// deletes the empty preview instead, so the days count as imported and nothing is left behind.
func TestEmptyStatementIsNotCommitted(t *testing.T) {
	l := startLab(t)
	ctx := context.Background()
	e, err := sim.New(sim.Options{Target: l.url, Seed: []byte(seed), Version: "test", Actions: actions.All(), Config: repeatable()})
	if err != nil {
		t.Fatal(err)
	}
	otter, err := e.Actor(ctx, identity.Nth(0))
	if err != nil {
		t.Fatal(err)
	}
	var from, to platform.Date
	for d := platform.MustDate(2026, time.January, 1); ; d = sim.AddDays(d, 1) {
		if d.Time().Year() > 2026 {
			t.Fatal("no week of 2026 without a row in the savings account")
		}
		if len(statements.Rows(otter.Persona, statements.Savings, d, sim.AddDays(d, 6))) == 0 {
			from, to = d, sim.AddDays(d, 6)
			break
		}
	}
	if err := actions.Import(ctx, otter, statements.Savings, from, to); err != nil {
		t.Fatalf("an empty statement from %v to %v: %v", from, to, err)
	}
	list, err := otter.API.ListImportsWithResponse(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if list.JSON200 == nil || len(*list.JSON200) != 0 {
		t.Errorf("imports left behind: %+v", list.JSON200)
	}
	for _, line := range l.logs.accessLines(t) {
		if line["route"] == "POST /api/v1/imports/{id}/commit" {
			t.Errorf("the empty statement was sent to commit: %v", line)
		}
	}
}

// TestHistoryRebuildAndSeed (ADR 0044): seeding rebuilds each persona's history under the
// configured profile (its label in the accounts' names, about as many transactions as the page
// estimates); seeding again imports nothing new; another profile rebuilds again.
func TestHistoryRebuildAndSeed(t *testing.T) {
	l := startLab(t)
	ctx := context.Background()
	cfg := repeatable()
	cfg.History = statements.History{Density: 3, Years: 1}
	e, err := sim.New(sim.Options{Target: l.url, Seed: []byte(seed), Version: "test", Actions: actions.All(), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []sim.Event
	seedNow := func() []sim.Event {
		events = nil
		if err := actions.Seed(ctx, e, 2, 2, func(ev sim.Event) { mu.Lock(); events = append(events, ev); mu.Unlock() }); err != nil {
			t.Fatal(err)
		}
		return events
	}
	accounts := func() (names []string, txs int) {
		otter, err := e.Actor(ctx, identity.Nth(0))
		if err != nil {
			t.Fatal(err)
		}
		r, err := otter.API.ListAccountsWithResponse(ctx)
		if err != nil || r.JSON200 == nil {
			t.Fatal(err, r.StatusCode())
		}
		for _, acc := range *r.JSON200 {
			if acc.Kind == gen.AccountKindBank {
				names, txs = append(names, acc.Name), txs+int(acc.TxCount)
			}
		}
		slices.Sort(names)
		return names, txs
	}
	for _, ev := range seedNow() {
		if ev.Action != sim.RebuildAction || ev.Result != sim.ResultOK {
			t.Errorf("first seed: %+v", ev)
		}
	}
	h := cfg.History.Normal()
	names, txs := accounts()
	if want := []string{statements.AccountNameFor(statements.Current, h), statements.AccountNameFor(statements.Savings, h)}; !slices.Equal(names, want) {
		t.Errorf("accounts %q, want %q", names, want)
	}
	if est := statements.Estimate(h); txs < est*6/10 || txs > est*14/10 {
		t.Errorf("%d transactions, the estimate is %d", txs, est)
	}
	for _, ev := range seedNow() {
		if ev.Action != "history.top-up" || ev.Result == sim.ResultError {
			t.Errorf("second seed: %+v", ev)
		}
	}
	if _, again := accounts(); again != txs {
		t.Errorf("seeding again changed %d transactions to %d", txs, again)
	}
	cfg.History = statements.History{}
	e.SetConfig(cfg)
	for _, ev := range seedNow() {
		if ev.Action != sim.RebuildAction || ev.Result != sim.ResultOK {
			t.Errorf("seed with the default profile: %+v", ev)
		}
	}
	if names, _ := accounts(); !slices.Equal(names, []string{statements.AccountName(statements.Current), statements.AccountName(statements.Savings)}) {
		t.Errorf("after the default profile: %q", names)
	}
}
