package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/fincli"
)

// TestFinCLIAgainstTheAPI: the fin CLI's generated client against the real server with a token
// (20.13, ADR 0036): every command's request passes the API's validation and its answer decodes.
func TestFinCLIAgainstTheAPI(t *testing.T) {
	e, mock := mockEnv(t, nil)
	b, _ := demoBrowser(t, e)
	write, _ := b.token(t, "cli", "write", 30)
	read, _ := b.token(t, "reports", "read", 30)
	expect(t, b.do("PUT", "/budgets/"+categoryID(t, b, "Rozrywka"), map[string]any{"amountMinor": 30000}), 204, "")
	dir := t.TempDir()
	fin := func(token, stdin string, args ...string) (int, string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := fincli.Run(context.Background(), args, fincli.Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
			Getenv: func(k string) string {
				return map[string]string{"FIN_URL": e.srv.URL, "FIN_TOKEN": token}[k]
			}, ConfigDir: func() (string, error) { return dir, nil }, Version: "test", Sleep: func(time.Duration) { time.Sleep(50 * time.Millisecond) }})
		return code, out.String(), errb.String()
	}

	if code, out, errs := fin(read, "", "--output", "csv", "accounts"); code != 0 || !strings.HasPrefix(out, "NAME,KIND,CURRENCY") ||
		!strings.Contains(out, "Demo,bank,PLN,917,") {
		t.Fatalf("accounts = %d %q %q", code, out, errs)
	}
	code, out, errs := fin(read, "", "--output", "json", "transactions", "--account", "demo", "--search", "kino", "--from", "2024-01-01", "--to", "2025-12-31", "--limit", "3")
	var page struct {
		Rows  []map[string]any `json:"rows"`
		Total int              `json:"total"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &page) != nil || len(page.Rows) != 3 || page.Total < 3 {
		t.Fatalf("transactions = %d %q %q", code, out, errs)
	}
	if code, out, errs := fin(read, "", "overview", "--month", "2025-01"); code != 0 || !strings.HasPrefix(out, "2025-01: income ") || !strings.Contains(out, "Rozrywka") {
		t.Errorf("overview = %d %q %q", code, out, errs)
	}
	for _, cmd := range []string{"whoami", "groups", "shared", "budgets", "goals"} {
		if code, out, errs := fin(read, "", cmd); code != 0 {
			t.Errorf("%s = %d %q %q", cmd, code, out, errs)
		}
	}
	csvFile := filepath.Join(t.TempDir(), "kino.csv")
	if code, _, errs := fin(read, "", "transactions", "--search", "kino", "--export", csvFile); code != 0 {
		t.Errorf("export = %d %q", code, errs)
	}
	if b, _ := os.ReadFile(csvFile); !bytes.Contains(b, []byte("KINO POD GWIAZDAMI")) { //nolint:gosec // test file
		t.Errorf("export file = %.200q", b)
	}

	// a read token can't import; a write token can
	file := filepath.Join(t.TempDir(), "extra.csv")
	if err := os.WriteFile(file, []byte("Data;Kontrahent;Kwota\n2025-03-20;KIOSK;-5,00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := fin(read, "", "import", file, "--account", "Demo", "--commit"); code != 1 || !strings.Contains(errs, "403") {
		t.Errorf("import with a read token = %d %q", code, errs)
	}
	if code, out, errs := fin(write, "", "import", file, "--account", "Demo", "--commit"); code != 0 || !strings.Contains(out, "Imported: 1 new") {
		t.Errorf("import = %d %q %q", code, out, errs)
	}

	// the assistant, streamed
	_ = mock
	if code, out, errs := fin(write, "", "ask", `What did I pay at "kino"?`); code != 0 || !strings.Contains(out, "Found **10** matching transactions") ||
		!strings.Contains(errs, "search_transactions") {
		t.Errorf("ask = %d %q %q", code, out, errs)
	}
}

// categoryID finds a category's id by name.
func categoryID(t *testing.T, b *browser, name string) string {
	t.Helper()
	for _, c := range b.do("GET", "/categories", nil).body2(t) {
		if c["name"] == name {
			return c["id"].(string)
		}
	}
	t.Fatalf("no category %q", name)
	return ""
}
