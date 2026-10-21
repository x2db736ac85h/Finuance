package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// HTTP tests of the accounts and imports endpoints (PLAN Phase 6), on the real database with a
// River client working the commit jobs.

func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// upload posts one multipart file (and accountId when set) to /imports?query.
func (b *browser) upload(query, filename string, content []byte, accountID string) resp {
	b.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if accountID != "" {
		_ = mw.WriteField("accountId", accountID)
	}
	if filename != "" {
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = fw.Write(content)
	}
	_ = mw.Close()
	return b.raw(http.MethodPost, "/imports"+query, mw.FormDataContentType(), &body)
}

// raw sends a body with an explicit content type.
func (b *browser) raw(method, path, contentType string, body io.Reader) resp {
	b.e.t.Helper()
	req, _ := http.NewRequest(method, b.e.srv.URL+"/api/v1"+path, body)
	req.Header.Set("Content-Type", contentType)
	for k, v := range b.header {
		req.Header.Set(k, v)
	}
	res, err := b.c.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer res.Body.Close()
	rawBody, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(rawBody), header: res.Header}
	_ = json.Unmarshal(rawBody, &out.body)
	return out
}

// signedIn returns a browser with a fresh account of its own.
func signedIn(t *testing.T, e *authEnv, email string) *browser {
	t.Helper()
	b := e.browser()
	expect(t, b.do("POST", "/auth/signup", map[string]any{"email": email, "password": password}), 201, "")
	return b
}

// awaitStatus polls GET /imports/{id} until the import leaves "committing".
func (b *browser) awaitStatus(id string) resp {
	b.e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r := b.do("GET", "/imports/"+id, nil)
		expect(b.e.t, r, 200, "")
		if r.body["status"] != "committing" {
			return r
		}
		if time.Now().After(deadline) {
			b.e.t.Fatalf("import %s still committing", id)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// commit starts the commit job and waits for it to finish.
func (b *browser) commit(id string) resp {
	b.e.t.Helper()
	r := b.do("POST", "/imports/"+id+"/commit", map[string]any{})
	expect(b.e.t, r, 202, "")
	if r.body["status"] != "committing" {
		b.e.t.Fatalf("commit returned status %v", r.body["status"])
	}
	done := b.awaitStatus(id)
	if done.body["status"] != "committed" {
		b.e.t.Fatalf("import %s ended %v: %s", id, done.body["status"], done.raw)
	}
	return done
}

func obj(t *testing.T, v any, key string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %v", v)
	}
	o, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object: %v", key, m[key])
	}
	return o
}

func num(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func TestImportFlowHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	pekao := fixture(t, "real/pekao/pekao_example.csv")

	// The account's number hint matches the file's own account: the upload pre-fills it.
	acc := b.do("POST", "/accounts", map[string]any{"name": "Pekao", "numberHint": "4971"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)

	up := b.upload("", "C:\\exports\\pekao_example.csv", pekao, "")
	expect(t, up, 201, "")
	imp := obj(t, up.body, "import")
	id := imp["id"].(string)
	if imp["accountId"] != accID || imp["status"] != "previewed" || imp["mappingSource"] != "detected" {
		t.Fatalf("preview import = %v", imp)
	}
	if imp["filename"] != "pekao_example.csv" {
		t.Errorf("filename = %v, want the base name", imp["filename"])
	}
	stats := obj(t, up.body, "stats")
	if num(stats["rows"]) == 0 || num(stats["errors"]) != 0 || num(stats["duplicatesEstimate"]) != 0 {
		t.Errorf("stats = %v", stats)
	}
	if n := len(up.body["sampleRows"].([]any)); n == 0 || n > 50 {
		t.Errorf("%d sample rows", n)
	}
	if m := up.body["missing"].([]any); len(m) != 0 {
		t.Errorf("missing = %v", m)
	}

	// Lines: the header is record 1, data follows.
	ln := b.do("GET", "/imports/"+id+"/lines?limit=3", nil)
	expect(t, ln, 200, "")
	lines := ln.body["lines"].([]any)
	if len(lines) != 3 || lines[0].(map[string]any)["status"] != "header" || lines[1].(map[string]any)["status"] != "data" {
		t.Fatalf("lines = %s", ln.raw)
	}
	if ln.body["delimiter"] != ";" || ln.body["encoding"] != "utf-8" {
		t.Errorf("lines meta = %s", ln.raw)
	}

	// PATCH: no header row makes the first record data, and the mapping no longer fits (422) …
	mapping := imp["mapping"]
	bad := b.do("PATCH", "/imports/"+id, map[string]any{"options": map[string]any{"encoding": "auto", "delimiter": "auto",
		"headerRow": "none", "skipRows": 0, "dropShort": true, "dateOrder": "dmy", "stripQuote": true, "invertSign": false,
		"amountMode": "single", "currencyFallback": ""}, "mapping": mapping})
	expect(t, bad, 422, "validation_failed")
	// … a mapping pointing past the last column is rejected too …
	expect(t, b.do("PATCH", "/imports/"+id, map[string]any{"mapping": map[string]any{"bookedOn": map[string]any{"index": 99, "header": "x"},
		"ignore": []any{}}}), 422, "validation_failed")
	// … and invertSign re-stages the rows with the opposite sign.
	opts := imp["options"].(map[string]any)
	opts["invertSign"] = true
	inv := b.do("PATCH", "/imports/"+id, map[string]any{"options": opts})
	expect(t, inv, 200, "")
	first := func(r resp) float64 {
		return r.body["sampleRows"].([]any)[0].(map[string]any)["amount"].(map[string]any)["amountMinor"].(float64)
	}
	if first(inv) != -first(up) {
		t.Errorf("inverted amount %v, original %v", first(inv), first(up))
	}
	opts["invertSign"] = false
	expect(t, b.do("PATCH", "/imports/"+id, map[string]any{"options": opts}), 200, "")

	// The same bytes again: 409 with the existing import's id.
	dup := b.upload("", "again.csv", pekao, "")
	expect(t, dup, 409, "duplicate_file")
	if dup.body["existingImportId"] != id {
		t.Errorf("existingImportId = %v, want %s", dup.body["existingImportId"], id)
	}

	done := b.commit(id)
	st := done.body["stats"].(map[string]any)
	rows := num(stats["rows"])
	if num(st["new"]) != rows || num(st["duplicates"]) != 0 {
		t.Errorf("commit stats = %v (rows %d)", st, rows)
	}
	if done.body["committedAt"] == nil {
		t.Error("committedAt not set")
	}
	expect(t, b.do("POST", "/imports/"+id+"/commit", map[string]any{}), 409, "import_not_committable")

	// The account now has the transactions and the date range.
	list := b.do("GET", "/accounts", nil)
	expect(t, list, 200, "")
	var accounts []map[string]any
	_ = json.Unmarshal([]byte(list.raw), &accounts)
	if len(accounts) != 1 || num(accounts[0]["txCount"]) != rows || accounts[0]["firstOn"] == nil || accounts[0]["lastOn"] == nil {
		t.Fatalf("accounts = %s", list.raw)
	}

	// Rows: pages, totals, transaction links; the original file comes back byte for byte.
	page := b.do("GET", "/imports/"+id+"/rows?offset=0&limit=5", nil)
	expect(t, page, 200, "")
	if num(page.body["total"]) != rows || len(page.body["rows"].([]any)) != 5 {
		t.Fatalf("rows page = %s", page.raw)
	}
	if page.body["rows"].([]any)[0].(map[string]any)["transactionId"] == nil {
		t.Error("committed row has no transactionId")
	}
	errs := b.do("GET", "/imports/"+id+"/rows?onlyErrors=true", nil)
	expect(t, errs, 200, "")
	if num(errs.body["total"]) != 0 || len(errs.body["rows"].([]any)) != 0 {
		t.Errorf("error rows = %s", errs.raw)
	}
	file := b.do("GET", "/imports/"+id+"/file", nil)
	expect(t, file, 200, "")
	if file.raw != string(pekao) {
		t.Error("downloaded file differs from the upload")
	}
	if cd := file.header.Get("Content-Disposition"); !strings.Contains(cd, `filename=pekao_example.csv`) {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// Listing, filtered by account.
	il := b.do("GET", "/imports?accountId="+accID, nil)
	expect(t, il, 200, "")
	var imports []map[string]any
	_ = json.Unmarshal([]byte(il.raw), &imports)
	if len(imports) != 1 || imports[0]["id"] != id || imports[0]["profileLabel"] != nil {
		t.Errorf("imports = %s", il.raw)
	}

	// Reprocess with inverted signs: committing again, then committed with the same counts.
	rp := b.do("POST", "/imports/"+id+"/reprocess", map[string]any{"options": opts})
	expect(t, rp, 202, "")
	if r := b.awaitStatus(id); r.body["status"] != "committed" {
		t.Fatalf("reprocess ended %s", r.raw)
	}

	// Delete: the transactions go, the account stays.
	expect(t, b.do("DELETE", "/imports/"+id, nil), 204, "")
	list = b.do("GET", "/accounts", nil)
	_ = json.Unmarshal([]byte(list.raw), &accounts)
	if len(accounts) != 1 || num(accounts[0]["txCount"]) != 0 {
		t.Errorf("after delete accounts = %s", list.raw)
	}
	expect(t, b.do("GET", "/imports/"+id, nil), 404, "not_found")
}

// TestImportOverlapMatchesOracle commits the overlap set A, B, C through the API and checks the
// account against the legacy oracle's merged result; B is the second file of the layout, so it is
// read with the saved profile, and C goes through the autoCommit fast path.
func TestImportOverlapMatchesOracle(t *testing.T) {
	var oracle struct {
		Default struct {
			RowCount int `json:"rowCount"`
			Dupes    int `json:"dupes"`
		} `json:"default"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "oracle", "dedupe", "overlap-A-B-C.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}

	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Overlap"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)

	total := 0
	upA := b.upload("", "A_jan-mar.csv", fixture(t, "synthetic/overlap/A_jan-mar.csv"), accID)
	expect(t, upA, 201, "")
	total += num(obj(t, upA.body, "stats")["rows"])
	if src := obj(t, upA.body, "import")["mappingSource"]; src != "detected" {
		t.Errorf("A mapping source %v", src)
	}
	b.commit(obj(t, upA.body, "import")["id"].(string))

	upB := b.upload("", "B_mar-may.csv", fixture(t, "synthetic/overlap/B_mar-may.csv"), accID)
	expect(t, upB, 201, "")
	total += num(obj(t, upB.body, "stats")["rows"])
	if src := obj(t, upB.body, "import")["mappingSource"]; src != "profile" {
		t.Errorf("B mapping source %v, want the saved profile", src)
	}
	if num(obj(t, upB.body, "stats")["duplicatesEstimate"]) == 0 {
		t.Error("B overlaps A but the preview estimates no duplicates")
	}
	b.commit(obj(t, upB.body, "import")["id"].(string))

	upC := b.upload("?autoCommit=true", "C_q1_again.csv", fixture(t, "synthetic/overlap/C_q1_again.csv"), accID)
	expect(t, upC, 202, "")
	total += num(obj(t, upC.body, "stats")["rows"])
	cID := obj(t, upC.body, "import")["id"].(string)
	if r := b.awaitStatus(cID); r.body["status"] != "committed" {
		t.Fatalf("C ended %s", r.raw)
	}

	list := b.do("GET", "/accounts", nil)
	var accounts []map[string]any
	_ = json.Unmarshal([]byte(list.raw), &accounts)
	if got := num(accounts[0]["txCount"]); got != oracle.Default.RowCount {
		t.Errorf("transactions = %d, oracle %d", got, oracle.Default.RowCount)
	}
	if dupes := total - num(accounts[0]["txCount"]); dupes != oracle.Default.Dupes {
		t.Errorf("duplicates = %d, oracle %d", dupes, oracle.Default.Dupes)
	}
	var imports []map[string]any
	_ = json.Unmarshal([]byte(b.do("GET", "/imports", nil).raw), &imports)
	if len(imports) != 3 {
		t.Fatalf("%d imports", len(imports))
	}
	for _, i := range imports {
		if i["profileLabel"] != nil {
			t.Errorf("one layout but %s has label %v", i["filename"], i["profileLabel"])
		}
	}

	// A second layout labels both: b1 for the first seen, b2 for the new one.
	other := b.do("POST", "/accounts", map[string]any{"name": "Other bank"})
	upD := b.upload("", "bank-b.csv", fixture(t, "synthetic/43-bank-b-layout.csv"), other.body["id"].(string))
	expect(t, upD, 201, "")
	labels := map[string]any{}
	_ = json.Unmarshal([]byte(b.do("GET", "/imports", nil).raw), &imports)
	for _, i := range imports {
		labels[i["filename"].(string)] = i["profileLabel"]
	}
	if labels["A_jan-mar.csv"] != "b1" || labels["C_q1_again.csv"] != "b1" || labels["bank-b.csv"] != "b2" {
		t.Errorf("labels = %v", labels)
	}
}

func TestImportErrorsHTTP(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.MaxUploadBytes = 64 << 10 }, false)
	b := signedIn(t, e, testEmail(t))
	csv := fixture(t, "synthetic/overlap/A_jan-mar.csv")

	expect(t, b.upload("", "bin.csv", append([]byte("a;b\n1;2\n"), 0, 1, 2), ""), 415, "unsupported_file")
	// Over the file limit but within the multipart allowance: the handler's exact check …
	expect(t, b.upload("", "big.csv", bytes.Repeat([]byte("01.01.2025;x;1,00\n"), 4000), ""), 413, "payload_too_large")
	// … and far over it: the body limit stops the read.
	expect(t, b.upload("", "huge.csv", bytes.Repeat([]byte("01.01.2025;x;1,00\n"), 20000), ""), 413, "payload_too_large")
	expect(t, b.upload("", "", nil, ""), 422, "validation_failed")
	expect(t, b.upload("", "empty.csv", []byte{}, ""), 422, "validation_failed")
	expect(t, b.upload("", "a.csv", csv, "not-a-uuid"), 422, "validation_failed")
	expect(t, b.raw("POST", "/imports", "text/csv", bytes.NewReader(csv)), 415, "unsupported_file")

	// Without an account the import can be previewed but not committed.
	up := b.upload("", "A.csv", csv, "")
	expect(t, up, 201, "")
	id := obj(t, up.body, "import")["id"].(string)
	if obj(t, up.body, "import")["accountId"] != nil || obj(t, up.body, "suggestedAccount")["createName"] != nil {
		t.Errorf("no own-account column, yet a suggestion: %s", up.raw)
	}
	expect(t, b.do("POST", "/imports/"+id+"/commit", map[string]any{}), 422, "validation_failed")
	expect(t, b.do("POST", "/imports/"+id+"/reprocess", map[string]any{}), 409, "import_not_committable")
	expect(t, b.do("PATCH", "/imports/"+id, map[string]any{"accountId": "00000000-0000-0000-0000-000000000001"}), 404, "not_found")

	// Another user sees none of it.
	other := signedIn(t, e, "other-"+testEmail(t))
	for _, p := range []string{"", "/rows", "/lines", "/file"} {
		expect(t, other.do("GET", "/imports/"+id+p, nil), 404, "not_found")
	}
	expect(t, other.do("DELETE", "/imports/"+id, nil), 404, "not_found")
	expect(t, other.do("POST", "/imports/"+id+"/commit", map[string]any{}), 404, "not_found")
	var imports []any
	_ = json.Unmarshal([]byte(other.do("GET", "/imports", nil).raw), &imports)
	if len(imports) != 0 {
		t.Errorf("other user lists %d imports", len(imports))
	}
	// Account endpoints are scoped the same way.
	acc := b.do("POST", "/accounts", map[string]any{"name": "Mine"})
	expect(t, other.do("PATCH", "/accounts/"+acc.body["id"].(string), map[string]any{"name": "x"}), 404, "not_found")
	expect(t, other.do("DELETE", "/accounts/"+acc.body["id"].(string), nil), 404, "not_found")
}

func TestUploadRateLimit(t *testing.T) {
	e := newTestEnv(t, nil, false)
	b := signedIn(t, e, testEmail(t))
	var last resp
	for i := range 31 {
		last = b.upload("", fmt.Sprintf("f%d.csv", i), []byte(fmt.Sprintf("Data;Kwota\n01.01.2025;%d,00\n", i+1)), "")
		if i < 30 {
			expect(t, last, 201, "")
		}
	}
	expect(t, last, 429, "rate_limited")
	if last.header.Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
}
