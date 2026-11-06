package fincli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	accID   = "0c000000-0000-4000-8000-000000000001"
	cashID  = "0c000000-0000-4000-8000-000000000002"
	chatID  = "0d000000-0000-4000-8000-000000000001"
	importI = "0e000000-0000-4000-8000-000000000001"
	secret  = "fin_testtoken0123456789"
)

type seen struct {
	method, path, auth, body, contentType string
}

// fakeAPI answers like the Finuance API for the routes the commands use.
type fakeAPI struct {
	mu       sync.Mutex
	requests []seen
	polls    int
	answer   string // the SSE stream of POST …/messages
}

func (f *fakeAPI) last(method, path string) seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].method == method && f.requests[i].path == path {
			return f.requests[i]
		}
	}
	return seen{}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, seen{r.Method, r.URL.Path, r.Header.Get("Authorization"), string(body), r.Header.Get("Content-Type")})
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+secret {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not signed in","status":401,"code":"unauthenticated"}`))
		return
	}
	js := func(status int, v string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(v))
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		js(200, `{"id":"0b000000-0000-4000-8000-000000000001","email":"alex@example.com","displayName":"Alex","isDemo":false,
			"createdAt":"2026-10-01T10:00:00Z","isAdmin":false,"mustChangePassword":false,"limits":{"maxUploadBytes":20971520,"importsPerDay":0,
			"maxAccounts":0,"maxGroups":0,"maxSharedGroups":0,"assistantPerHour":30,"assistantTokensPerDay":200000}}`)
	case "GET /api/v1/accounts":
		js(200, `[{"id":"`+accID+`","name":"Konto","kind":"bank","currency":"PLN","openingBalance":{"amountMinor":0,"currency":"PLN"},
			"txCount":917,"firstOn":"2024-01-01","lastOn":"2025-03-17","isDemo":false,"archived":false,"createdAt":"2026-10-01T10:00:00Z"},
			{"id":"`+cashID+`","name":"Wallet","kind":"cash","currency":"EUR","openingBalance":{"amountMinor":0,"currency":"EUR"},
			"txCount":0,"isDemo":false,"archived":false,"createdAt":"2026-10-02T10:00:00Z"}]`)
	case "POST /api/v1/transactions/query":
		js(200, `{"offset":0,"total":12,"rows":[
			{"id":"0f000000-0000-4000-8000-000000000001","accountId":"`+accID+`","accountName":"Konto","bookedOn":"2025-03-02",
			"amount":{"amountMinor":-4500,"currency":"PLN"},"payee":"KINO POD GWIAZDAMI","description":"bilety","category":{"id":"0f000000-0000-4000-8000-0000000000c1","name":"Rozrywka"},
			"categorySource":"bank","bankCategory":"Rozrywka","txType":"","reference":"","counterpartyAccount":"","note":"z Bo\tna film","isTransfer":false,"source":"bank","ai":false},
			{"id":"0f000000-0000-4000-8000-000000000002","accountId":"`+accID+`","accountName":"Konto","bookedOn":"2025-03-01",
			"amount":{"amountMinor":1250000,"currency":"PLN"},"payee":"PRACODAWCA","description":"pensja","categorySource":"bank","bankCategory":"",
			"txType":"","reference":"","counterpartyAccount":"","note":"","isTransfer":false,"source":"bank","ai":false}]}`)
	case "POST /api/v1/transactions/export":
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("Date;Amount\n02.03.2025;-45,00\n"))
	case "POST /api/v1/imports":
		js(201, `{"import":{"id":"`+importI+`","accountId":"`+accID+`","filename":"march.csv","status":"previewed","createdAt":"2026-11-06T10:00:00Z",
			"sizeBytes":120,"mappingSource":"auto","stats":{},"mapping":{},"options":{}},"detected":{},"missing":[],"sampleRows":[],
			"stats":{"rows":3,"errors":0,"headerAt":1,"dropped":0,"newEstimate":2,"duplicatesEstimate":1,"dateFrom":"2025-03-01","dateTo":"2025-03-31"},
			"suggestedAccount":{"id":"`+accID+`"}}`)
	case "POST /api/v1/imports/" + importI + "/commit":
		js(202, `{"id":"`+importI+`","filename":"march.csv","status":"committing","createdAt":"2026-11-06T10:00:00Z","sizeBytes":120,
			"mappingSource":"auto","stats":{},"mapping":{},"options":{}}`)
	case "GET /api/v1/imports/" + importI:
		f.mu.Lock()
		f.polls++
		status, stats := "committing", `{}`
		if f.polls > 1 {
			status, stats = "committed", `{"new":2,"duplicates":1}`
		}
		f.mu.Unlock()
		js(200, `{"id":"`+importI+`","filename":"march.csv","status":"`+status+`","createdAt":"2026-11-06T10:00:00Z","sizeBytes":120,
			"mappingSource":"auto","stats":`+stats+`,"mapping":{},"options":{}}`)
	case "POST /api/v1/assistant/chats":
		js(201, `{"id":"`+chatID+`","title":"","kind":"chat","connection":"","model":"","createdAt":"2026-11-06T10:00:00Z","updatedAt":"2026-11-06T10:00:00Z"}`)
	case "POST /api/v1/assistant/chats/" + chatID + "/messages":
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(f.answer))
	case "GET /api/v1/budgets":
		js(200, `{"budgets":[{"category":{"id":"0f000000-0000-4000-8000-0000000000c1","name":"Rozrywka"},"amountMinor":30000}]}`)
	default:
		js(404, `{"type":"about:blank","title":"Not found","status":404,"code":"not_found"}`)
	}
}

type run struct {
	code           int
	stdout, stderr string
}

func newFake(t *testing.T) (*fakeAPI, *httptest.Server, string) {
	t.Helper()
	f := &fakeAPI{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv, t.TempDir()
}

func runFin(t *testing.T, dir, stdin string, env map[string]string, args ...string) run {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
		Getenv: func(k string) string { return env[k] }, ConfigDir: func() (string, error) { return dir, nil }, Version: "test",
		Sleep: func(time.Duration) {}})
	return run{code, out.String(), errb.String()}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	file := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file) //nolint:gosec // test data
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s:\n%s\nwant\n%s", name, got, want)
	}
}

func TestLoginAndConfig(t *testing.T) {
	f, srv, dir := newFake(t)
	if r := runFin(t, dir, "", nil, "accounts"); r.code != 1 || !strings.Contains(r.stderr, "not signed in") {
		t.Errorf("before login = %+v", r)
	}
	if r := runFin(t, dir, "sk-wrong\n", nil, "login", "--url", srv.URL); r.code != 1 || !strings.Contains(r.stderr, "start with fin_") {
		t.Errorf("not a token = %+v", r)
	}
	if r := runFin(t, dir, "fin_revoked\n", nil, "login", "--url", srv.URL); r.code != 1 || !strings.Contains(r.stderr, "Not signed in (is the token still valid?") ||
		!strings.Contains(r.stderr, "(401 unauthenticated)") {
		t.Errorf("a refused token = %+v", r)
	}
	r := runFin(t, dir, secret+"\n", nil, "login", "--url", srv.URL+"/")
	if r.code != 0 || !strings.Contains(r.stdout, "Signed in to "+srv.URL+" as alex@example.com") || strings.Contains(r.stdout, secret) {
		t.Fatalf("login = %+v", r)
	}
	p := filepath.Join(dir, "fin", "config.json")
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config file %v %v (only its user may read the token)", fi, err)
	}
	if r := runFin(t, dir, "", nil, "--output", "csv", "whoami"); r.code != 0 || r.stdout != "EMAIL,NAME,URL\nalex@example.com,Alex,"+srv.URL+"\n" {
		t.Errorf("whoami = %+v", r)
	}
	if got := f.last("GET", "/api/v1/me"); got.auth != "Bearer "+secret {
		t.Errorf("sent %+v", got)
	}
	// FIN_TOKEN wins over the file
	if r := runFin(t, dir, "", map[string]string{"FIN_TOKEN": "fin_other"}, "whoami"); r.code != 1 {
		t.Errorf("FIN_TOKEN ignored: %+v", r)
	}
	if r := runFin(t, dir, "", nil, "frobnicate"); r.code != 2 || !strings.Contains(r.stderr, "unknown command") {
		t.Errorf("unknown = %+v", r)
	}
	if r := runFin(t, dir, "", nil, "--output", "yaml", "accounts"); r.code != 2 {
		t.Errorf("bad output = %+v", r)
	}
	if r := runFin(t, dir, "", nil, "version"); r.stdout != "fin test\n" {
		t.Errorf("version = %+v", r)
	}
}

func signedIn(t *testing.T) (*fakeAPI, string, map[string]string) {
	t.Helper()
	f, srv, dir := newFake(t)
	return f, dir, map[string]string{"FIN_URL": srv.URL, "FIN_TOKEN": secret}
}

func TestAccounts(t *testing.T) {
	_, dir, env := signedIn(t)
	for _, out := range []string{"table", "csv", "json"} {
		r := runFin(t, dir, "", env, "--output", out, "accounts")
		if r.code != 0 {
			t.Fatalf("%s: %+v", out, r)
		}
		golden(t, "accounts-"+out, r.stdout)
	}
}

func TestTransactions(t *testing.T) {
	f, dir, env := signedIn(t)
	r := runFin(t, dir, "", env, "transactions", "--account", "konto", "--search", "kino", "--category", "Rozrywka", "--from", "2025-03-01",
		"--to", "2025-03-31", "--limit", "2")
	if r.code != 0 || r.stderr != "2 of 12 rows; --limit shows more\n" {
		t.Fatalf("transactions = %+v", r)
	}
	golden(t, "transactions-table", r.stdout)
	var body map[string]any
	if err := json.Unmarshal([]byte(f.last("POST", "/api/v1/transactions/query").body), &body); err != nil {
		t.Fatal(err)
	}
	ctx := body["context"].(map[string]any)
	if body["limit"] != float64(2) || ctx["accountIds"].([]any)[0] != accID || ctx["search"] != "kino" ||
		ctx["toolFilter"].(map[string]any)["category"] != "Rozrywka" || ctx["period"].(map[string]any)["from"] != "2025-03-01" ||
		ctx["period"].(map[string]any)["to"] != "2025-04-01" {
		t.Errorf("query = %v (the end day is inclusive here, exclusive in the API)", body)
	}
	for _, bad := range [][]string{{"--month", "2025-03", "--from", "2025-03-01"}, {"--from", "March"}, {"--limit", "0"}, {"--account", "Nope"}} {
		if r := runFin(t, dir, "", env, append([]string{"transactions"}, bad...)...); r.code == 0 {
			t.Errorf("%v accepted", bad)
		}
	}
	out := filepath.Join(t.TempDir(), "march.csv")
	if r := runFin(t, dir, "", env, "transactions", "--month", "2025-03", "--export", out); r.code != 0 {
		t.Fatalf("export = %+v", r)
	}
	if b, _ := os.ReadFile(out); string(b) != "Date;Amount\n02.03.2025;-45,00\n" { //nolint:gosec // test file
		t.Errorf("export file = %q", b)
	}
}

func TestImport(t *testing.T) {
	f, dir, env := signedIn(t)
	file := filepath.Join(t.TempDir(), "march.csv")
	if err := os.WriteFile(file, []byte("Data;Kwota\n2025-03-01;-1,00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runFin(t, dir, "", env, "import", file, "--account", "Konto", "--commit")
	if r.code != 0 || r.stdout != "march.csv: 3 rows, 0 with errors, about 2 new and 1 already imported, 2025-03-01 to 2025-03-31\nImported: 2 new, 1 already there\n" {
		t.Fatalf("import = %+v", r)
	}
	up := f.last("POST", "/api/v1/imports")
	_, params, _ := mime.ParseMediaType(up.contentType)
	mr := multipart.NewReader(strings.NewReader(up.body), params["boundary"])
	fields := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		fields[p.FormName()] = string(b)
		if p.FormName() == "file" && p.FileName() != "march.csv" {
			t.Errorf("file name %q", p.FileName())
		}
	}
	if fields["accountId"] != accID || fields["file"] != "Data;Kwota\n2025-03-01;-1,00\n" {
		t.Errorf("upload = %v", fields)
	}
	if r := runFin(t, dir, "", env, "import", file); r.code != 0 || !strings.Contains(r.stdout, "Not imported yet") {
		t.Errorf("preview only = %+v", r)
	}
	if r := runFin(t, dir, "", env, "import"); r.code != 2 {
		t.Errorf("no file = %+v", r)
	}
}

func TestAsk(t *testing.T) {
	f, dir, env := signedIn(t)
	f.answer = "event: reasoning\ndata: {\"delta\":\"thinking\"}\n\n" +
		"event: step\ndata: {\"index\":0,\"tool\":\"search_transactions\",\"summary\":\"query: kino → 10 matches\",\"args\":{},\"result\":\"{}\"}\n\n" +
		"event: step\ndata: {\"index\":1,\"tool\":\"propose_action\",\"summary\":\"proposed set budget\",\"args\":{},\"result\":\"{}\",\"proposal\":{\"action\":\"set_budget\"}}\n\n" +
		": keep-alive\n\n" +
		"event: content\ndata: {\"delta\":\"You went to the cinema \"}\n\n" +
		"event: content\ndata: {\"delta\":\"10 times.\"}\n\n" +
		"event: done\ndata: {\"messageSeq\":4,\"meta\":{}}\n\n"
	r := runFin(t, dir, "", env, "ask", "How", "often", "did", "I", "go", "to", "the", "cinema?")
	if r.code != 0 || r.stdout != "You went to the cinema 10 times.\n" ||
		r.stderr != "· search_transactions: query: kino → 10 matches\n· propose_action: proposed set budget\n1 proposed change(s): open the chat in the app to apply them\n" {
		t.Fatalf("ask = %+v", r)
	}
	if !strings.Contains(f.last("POST", "/api/v1/assistant/chats/"+chatID+"/messages").body, `"text":"How often did I go to the cinema?"`) {
		t.Errorf("question = %s", f.last("POST", "/api/v1/assistant/chats/"+chatID+"/messages").body)
	}
	f.answer = "event: error\ndata: {\"code\":\"unreachable\",\"message\":\"The model server can't be reached.\"}\n\n"
	if r := runFin(t, dir, "", env, "ask", "Hi"); r.code != 1 || !strings.Contains(r.stderr, "The model server can't be reached.") {
		t.Errorf("error event = %+v", r)
	}
	f.answer = "event: content\ndata: {\"delta\":\"cut\"}\n\n"
	if r := runFin(t, dir, "", env, "ask", "Hi"); r.code != 1 || !strings.Contains(r.stderr, "ended early") {
		t.Errorf("cut stream = %+v", r)
	}
	if r := runFin(t, dir, "", env, "ask"); r.code != 2 {
		t.Errorf("no question = %+v", r)
	}
}

func TestBudgetsAndProblems(t *testing.T) {
	_, dir, env := signedIn(t)
	if r := runFin(t, dir, "", env, "budgets"); r.code != 0 || r.stdout != "CATEGORY  MONTHLY BUDGET\nRozrywka  300.00\n" {
		t.Errorf("budgets = %q", r.stdout)
	}
	// a route the fake doesn't have answers with the API's problem
	if r := runFin(t, dir, "", env, "goals"); r.code != 1 || !strings.Contains(r.stderr, "Not found (404 not_found)") {
		t.Errorf("problem = %+v", r)
	}
}

func TestAmount(t *testing.T) {
	for _, c := range []struct {
		minor int64
		cur   string
		want  string
	}{{-4498, "PLN", "-44.98 PLN"}, {5, "EUR", "0.05 EUR"}, {-1, "XYZ", "-0.01 XYZ"}, {30000, "", "300.00"}} { // as the server stores them (money.Exponent)
		if got := amount(c.minor, c.cur); got != c.want {
			t.Errorf("amount(%d, %s) = %q, want %q", c.minor, c.cur, got, c.want)
		}
	}
	if got := reorder([]string{"file.csv", "--commit", "--account", "Konto"}); strings.Join(got, " ") != "--commit --account Konto file.csv" {
		t.Errorf("reorder = %v", got)
	}
}
