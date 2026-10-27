package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// plainBackupParts turns the oracle's plain legacy backup into what the wizard sends: the manifest
// (every file into one new account "Legacy", the opening balance there, chats included) and the
// file bytes.
func plainBackupParts(t *testing.T) (map[string]any, [][]byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "legacy-backup", "backup-plain.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		KV struct {
			Prefs struct {
				Opts json.RawMessage `json:"opts"`
			} `json:"prefs"`
			Private json.RawMessage `json:"private"`
			Chats   json.RawMessage `json:"chats"`
		} `json:"kv"`
		Files []struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Added int64           `json:"added"`
			Cfg   json.RawMessage `json:"cfg"`
			Data  struct {
				B string `json:"$b"`
			} `json:"data"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	acc := map[string]any{"new": map[string]any{"name": "Legacy"}}
	var files []any
	var data [][]byte
	for _, f := range b.Files {
		files = append(files, map[string]any{"legacyId": f.ID, "name": f.Name, "added": f.Added, "cfg": f.Cfg, "account": acc})
		d, err := base64.StdEncoding.DecodeString(f.Data.B)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, d)
	}
	man := map[string]any{"version": 1, "source": "finuanceBackup", "opts": b.KV.Prefs.Opts, "files": files,
		"private": b.KV.Private, "chats": b.KV.Chats, "opening": acc, "unknownKeys": []string{"kv.extra"}}
	return man, data
}

func (b *browser) migrate(manifest any, files [][]byte) resp {
	b.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if manifest != nil {
		js, _ := json.Marshal(manifest)
		_ = mw.WriteField("manifest", string(js))
	}
	for i, f := range files {
		fw, _ := mw.CreateFormFile("file", "f"+string(rune('a'+i))+".csv")
		_, _ = fw.Write(f)
	}
	_ = mw.Close()
	return b.raw(http.MethodPost, "/legacy-imports", mw.FormDataContentType(), &body)
}

func (b *browser) awaitMigration(id string) resp {
	b.e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r := b.do("GET", "/legacy-imports/"+id, nil)
		expect(b.e.t, r, 200, "")
		if s := r.body["status"]; s == "done" || s == "failed" {
			return r
		}
		if time.Now().After(deadline) {
			b.e.t.Fatalf("migration still %v", r.body["status"])
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestLegacyMigrationOverHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	man, files := plainBackupParts(t)

	r := b.migrate(man, files)
	expect(t, r, 202, "")
	if r.body["status"] != "queued" {
		t.Fatalf("new run: %s", r.raw)
	}
	id := r.body["id"].(string)
	done := b.awaitMigration(id)
	if done.body["status"] != "done" {
		t.Fatalf("run: %s", done.raw)
	}
	rep := done.body["report"].(map[string]any)
	labels := rep["labels"].(map[string]any)
	if labels["matched"].(float64) != 8 || labels["unmatched"].(float64) != 0 {
		t.Errorf("labels: %v", labels)
	}
	if n := len(rep["files"].([]any)); n != 3 {
		t.Errorf("files: %d", n)
	}
	if keys := rep["unknownKeys"].([]any); len(keys) != 1 || keys[0] != "kv.extra" {
		t.Errorf("unknown keys: %v", keys)
	}
	if done.body["progress"].(map[string]any)["filesDone"].(float64) != 3 {
		t.Errorf("progress: %v", done.body["progress"])
	}
	// the accounts and groups are there
	accs := b.do("GET", "/accounts", nil)
	expect(t, accs, 200, "")
	if !strings.Contains(accs.raw, `"Legacy"`) {
		t.Errorf("accounts: %s", accs.raw)
	}
	groups := b.do("GET", "/groups", nil)
	if !strings.Contains(groups.raw, "Wakacje nad morzem") || !strings.Contains(groups.raw, "Remont") {
		t.Errorf("groups: %s", groups.raw)
	}

	// the run and each file's commit are in the activity log
	if got := actions(t, b); !slices.Contains(got, "legacy.import_started") || slices.Index(got, "import.committed") < slices.Index(got, "legacy.import_started") {
		t.Errorf("activity = %v", got)
	}

	// the list shows the run; another user sees nothing
	list := b.do("GET", "/legacy-imports", nil)
	expect(t, list, 200, "")
	if !strings.Contains(list.raw, id) {
		t.Errorf("list: %s", list.raw)
	}
	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("GET", "/legacy-imports/"+id, nil), 404, "not_found")
	if l := other.do("GET", "/legacy-imports", nil); l.raw != "[]\n" {
		t.Errorf("other user's list: %s", l.raw)
	}

	// again: nothing new
	again := b.awaitMigration(b.migrate(man, files).body["id"].(string))
	tx := again.body["report"].(map[string]any)["transactions"].(map[string]any)
	if tx["new"].(float64) != 0 {
		t.Errorf("second run created transactions: %v", tx)
	}
}

func TestLegacyMigrationRequests(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.MaxUploadBytes = 64 << 10 }, false)
	b := signedIn(t, e, testEmail(t))
	man, files := plainBackupParts(t)

	expect(t, b.migrate(nil, nil), 422, "validation_failed")
	expect(t, b.migrate(map[string]any{"version": 1}, nil), 422, "validation_failed")
	expect(t, b.migrate(man, files[1:2]), 422, "validation_failed") // three files announced, one sent
	// demo.csv is 150 KB, over this server's upload limit
	expect(t, b.migrate(man, files), 413, "payload_too_large")

	small := map[string]any{"version": 1, "source": "csvInspectorBackup", "files": []any{
		map[string]any{"legacyId": "a", "name": "a.csv", "account": map[string]any{"new": map[string]any{"name": "A"}}}}}
	csv := [][]byte{[]byte("Data;Kwota\n01.02.2025;-1,00\n")}
	r := b.migrate(small, csv)
	expect(t, r, 202, "")
	// no worker runs here, so the first run stays queued
	expect(t, b.migrate(small, csv), 409, "migration_running")
	// without a session
	anon := e.browser()
	expect(t, anon.migrate(small, csv), 401, "unauthenticated")
}
