package legacyimport_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

var clock = platform.FixedClock{T: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}

const backupDir = "../../../testdata/fixtures/legacy-backup"

// backup is the JSON of a legacy backup file (exportBackup).
type backup struct {
	FinuanceBackup int `json:"finuanceBackup"`
	KV             struct {
		Prefs struct {
			Opts legacyimport.ParseOpts `json:"opts"`
		} `json:"prefs"`
		Private json.RawMessage `json:"private"`
		Lock    *struct {
			Salt  blob   `json:"salt"`
			Check sealed `json:"check"`
		} `json:"lock"`
		Chats json.RawMessage `json:"chats"`
	} `json:"kv"`
	Files []struct {
		ID    string                `json:"id"`
		Name  string                `json:"name"`
		Added int64                 `json:"added"`
		Cfg   *legacyimport.FileCfg `json:"cfg"`
		Data  json.RawMessage       `json:"data"`
	} `json:"files"`
}

// blob is a legacy ser() binary value: {"$b": base64}.
type blob []byte

func (b *blob) UnmarshalJSON(raw []byte) error {
	var v struct {
		B string `json:"$b"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	out, err := base64.StdEncoding.DecodeString(v.B)
	*b = out
	return err
}

// sealed is a legacy CRY.enc value: AES-GCM with a 12-byte iv; ct carries the tag (WebCrypto).
type sealed struct {
	IV blob `json:"iv"`
	CT blob `json:"ct"`
}

var errWrongPassphrase = errors.New("wrong passphrase")

// open decrypts a backup the way the wizard does in the browser (PLAN §4.10 step 2): PBKDF2-SHA-256
// with 310,000 iterations over the stored salt, AES-256-GCM, and the check value must decrypt to
// "finuance" or "csv-inspector".
func open(t *testing.T, b *backup, pass string) (cipher.AEAD, error) {
	t.Helper()
	key, err := pbkdf2.Key(sha256.New, pass, b.KV.Lock.Salt, 310000, 32)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		t.Fatal(err)
	}
	check, err := gcm.Open(nil, b.KV.Lock.Check.IV, b.KV.Lock.Check.CT, nil)
	if err != nil || (string(check) != "finuance" && string(check) != "csv-inspector") {
		return nil, errWrongPassphrase
	}
	return gcm, nil
}

// manifestOf builds what the wizard sends for a backup: the demo file into "Demo", the two
// duplicate versions into "Konto", the opening balance to "Konto", chats included.
func manifestOf(t *testing.T, name, pass string) (legacyimport.Manifest, [][]byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(backupDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var b backup
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	var gcm cipher.AEAD
	if b.KV.Lock != nil {
		if gcm, err = open(t, &b, pass); err != nil {
			t.Fatal(err)
		}
	}
	plain := func(v json.RawMessage) []byte {
		var enc struct {
			Enc *sealed `json:"enc"`
		}
		if json.Unmarshal(v, &enc) == nil && enc.Enc != nil {
			out, err := gcm.Open(nil, enc.Enc.IV, enc.Enc.CT, nil)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		return v
	}
	man := legacyimport.Manifest{Version: 1, Source: "finuanceBackup", Opts: b.KV.Prefs.Opts}
	if err := json.Unmarshal(plain(b.KV.Private), &man.Private); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plain(b.KV.Chats), &man.Chats); err != nil {
		t.Fatal(err)
	}
	konto := legacyimport.AccountRef{New: &legacyimport.NewAccount{Name: "Konto"}}
	man.Opening = &konto
	var files [][]byte
	for _, f := range b.Files {
		var data []byte
		var s sealed
		if gcm != nil && json.Unmarshal(f.Data, &s) == nil && len(s.IV) > 0 {
			if data, err = gcm.Open(nil, s.IV, s.CT, nil); err != nil {
				t.Fatal(err)
			}
		} else {
			var bl blob
			if err := json.Unmarshal(f.Data, &bl); err != nil {
				t.Fatal(err)
			}
			data = bl
		}
		acc := konto
		if f.Name == "demo.csv" {
			acc = legacyimport.AccountRef{New: &legacyimport.NewAccount{Name: "Demo"}}
		}
		man.Files = append(man.Files, legacyimport.ManifestFile{LegacyID: f.ID, Name: f.Name, Added: f.Added, Cfg: f.Cfg, Account: acc})
		files = append(files, data)
	}
	return man, files
}

type env struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	l   *ledger.Ledger
	m   *legacyimport.Migrator
	u   uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	st := store.New(tdb.App)
	l := ledger.New(st, clock)
	e := &env{t: t, ctx: context.Background(), st: st, l: l, m: legacyimport.New(st, l, assistant.NewChats(st, clock, 25), clock)}
	e.u = platform.NewID()
	if err := st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: e.u, Email: e.u.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func noEnqueue(context.Context, pgx.Tx, uuid.UUID) error { return nil }

// migrate creates a run and works it inline (the job's body).
func (e *env) migrate(man legacyimport.Manifest, files [][]byte) legacyimport.Run {
	e.t.Helper()
	run, err := e.m.Create(e.ctx, e.u, man, files, noEnqueue)
	if err != nil {
		e.t.Fatal(err)
	}
	if run.Status != legacyimport.StatusQueued || run.Progress.FilesTotal != len(files) {
		e.t.Fatalf("new run: %+v", run)
	}
	if err := e.m.Run(e.ctx, e.u, run.ID); err != nil {
		e.t.Fatal(err)
	}
	run, err = e.m.Get(e.ctx, e.u, run.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	if run.Status != legacyimport.StatusDone || run.Report == nil {
		e.t.Fatalf("run: %+v", run)
	}
	return run
}

func (e *env) accounts() map[string]ledger.Account {
	e.t.Helper()
	all, err := e.l.ListAccounts(e.ctx, e.u)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]ledger.Account{}
	for i := range all {
		out[all[i].Name] = all[i]
	}
	return out
}

func TestMigratePlainAndLockedBackups(t *testing.T) {
	for _, c := range []struct{ file, pass string }{{"backup-plain.json", ""}, {"backup-locked.json", "oracle-test-passphrase"}} {
		t.Run(c.file, func(t *testing.T) {
			e := newEnv(t)
			man, files := manifestOf(t, c.file, c.pass)
			rep := e.migrate(man, files).Report

			for _, f := range rep.Files {
				if f.Status != "imported" || f.Message != "" {
					t.Errorf("file %s: %+v", f.Name, f)
				}
			}
			accs := e.accounts()
			demo, konto := accs["Demo"], accs["Konto"]
			// demo.csv as one file; the two duplicate versions merged as the legacy merged them, plus
			// the transaction the legacy "keep both" decision keeps twice
			if demo.TxCount != 917 || konto.TxCount != 8 {
				t.Errorf("transactions: Demo %d, Konto %d", demo.TxCount, konto.TxCount)
			}
			if konto.OpeningBalanceMinor != 150000 || rep.Opening == nil || rep.Opening.AccountName != "Konto" {
				t.Errorf("opening: %d, %+v", konto.OpeningBalanceMinor, rep.Opening)
			}
			if rep.Labels.Matched != len(man.Private.Tags) || rep.Labels.Unmatched != 0 {
				t.Errorf("labels: %+v", rep.Labels)
			}
			if rep.Overrides.Matched != 2 || rep.Overrides.Unmatched != 0 {
				t.Errorf("overrides: %+v", rep.Overrides)
			}
			if rep.Groups.Created != 2 || rep.Budgets.Set != 3 || rep.Investments != "imported" || rep.Chats.Imported != 2 {
				t.Errorf("report: groups %+v, budgets %+v, investments %s, chats %+v", rep.Groups, rep.Budgets, rep.Investments, rep.Chats)
			}
			stats, err := e.l.GroupStats(e.ctx, e.u)
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			for _, s := range stats {
				counts[s.Name] = s.Count
			}
			if counts["Wakacje nad morzem"] != 5 || counts["Remont"] != 3 {
				t.Errorf("group members: %v", counts)
			}
			budgets, err := e.l.ListBudgets(e.ctx, e.u)
			if err != nil {
				t.Fatal(err)
			}
			if len(budgets) != 3 {
				t.Errorf("budgets: %+v", budgets)
			}
			inv, err := e.l.Investments(e.ctx, e.u)
			if err != nil {
				t.Fatal(err)
			}
			if inv.PPK.ValueMinor != 2540000 || len(inv.Positions) != 1 || inv.Positions[0].ValueMinor != 4650000 || inv.SwrBp != 400 {
				t.Errorf("investments: %+v", inv)
			}

			// running the same backup again, an hour later, creates nothing new
			later := platform.FixedClock{T: clock.T.Add(time.Hour)}
			e.m = legacyimport.New(e.st, e.l, assistant.NewChats(e.st, later, 25), later)
			again := e.migrate(man, files).Report
			for _, f := range again.Files {
				if f.Status != "reused" {
					t.Errorf("second run, file %s: %s", f.Name, f.Status)
				}
			}
			after := e.accounts()
			if len(after) != 2 || after["Demo"].TxCount != 917 || after["Konto"].TxCount != 8 {
				t.Errorf("second run accounts: %+v", after)
			}
			if again.Groups.Created != 0 || again.Groups.Reused != 2 || again.Labels.Matched != len(man.Private.Tags) ||
				again.Chats.Imported != 0 || again.Chats.Skipped != 2 || again.Investments != "kept" || again.Transactions.New != 0 {
				t.Errorf("second run report: %+v", again)
			}
			stats, _ = e.l.GroupStats(e.ctx, e.u)
			if len(stats) != 2 {
				t.Errorf("groups after the second run: %+v", stats)
			}
		})
	}
}

func TestWrongPassphrase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(backupDir, "backup-locked.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b backup
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, &b, "not the passphrase"); !errors.Is(err, errWrongPassphrase) {
		t.Fatalf("wrong passphrase accepted: %v", err)
	}
}

func TestOneRunAtATime(t *testing.T) {
	e := newEnv(t)
	man, files := manifestOf(t, "backup-plain.json", "")
	if _, err := e.m.Create(e.ctx, e.u, man, files, noEnqueue); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Create(e.ctx, e.u, man, files, noEnqueue); !errors.Is(err, legacyimport.ErrRunning) {
		t.Fatalf("second queued run: %v", err)
	}
	// another user is not affected, and cannot see the run
	other := newEnv(t)
	runs, err := other.m.List(other.ctx, other.u)
	if err != nil || len(runs) != 0 {
		t.Fatalf("other user's runs: %v, %v", runs, err)
	}
	mine, _ := e.m.List(e.ctx, e.u)
	if _, err := other.m.Get(other.ctx, other.u, mine[0].ID); !errors.Is(err, legacyimport.ErrNotFound) {
		t.Fatalf("other user's Get: %v", err)
	}
}

func TestFailedFileIsReported(t *testing.T) {
	e := newEnv(t)
	man := legacyimport.Manifest{Version: 1, Source: "csvInspectorBackup", Files: []legacyimport.ManifestFile{
		{LegacyID: "a", Name: "notes.csv", Account: legacyimport.AccountRef{New: &legacyimport.NewAccount{Name: "Notes"}}},
		{LegacyID: "b", Name: "empty.csv", Added: 1, Account: legacyimport.AccountRef{New: &legacyimport.NewAccount{Name: "Notes"}}},
	}}
	rep := e.migrate(man, [][]byte{[]byte("Name;Comment\nAlex;hello\nSam;bye\n"), []byte("\n\n")}).Report
	if len(rep.Files) != 2 || rep.Files[0].Status != "failed" || rep.Files[1].Status != "failed" {
		t.Fatalf("files: %+v", rep.Files)
	}
	if !slices.ContainsFunc(rep.Accounts, func(a legacyimport.AccountReport) bool { return a.Name == "Notes" && a.Created }) {
		t.Errorf("accounts: %+v", rep.Accounts)
	}
	for _, f := range rep.Files {
		if f.Message == "" {
			t.Errorf("%s: no reason", f.Name)
		}
	}
}

func TestManifestValidation(t *testing.T) {
	e := newEnv(t)
	bad := legacyimport.Manifest{Version: 2, Source: "x", Files: []legacyimport.ManifestFile{{LegacyID: "", Name: ""}}}
	_, err := e.m.Create(e.ctx, e.u, bad, [][]byte{nil}, noEnqueue)
	var ve *platform.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) < 4 {
		t.Fatalf("validation: %v", err)
	}
}
