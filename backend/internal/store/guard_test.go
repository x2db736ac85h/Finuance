package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// dbCallNames are the pgx/database/sql methods that run SQL or open a transaction.
var dbCallNames = map[string]bool{
	"Query": true, "QueryRow": true, "Exec": true, "Begin": true, "BeginTx": true,
	"SendBatch": true, "CopyFrom": true, "BeginFunc": true, "BeginTxFunc": true,
}

// guardAllowed lists packages (relative to backend/) that may run SQL themselves, with the reason.
// Keep it short: user data must go through store.WithUser (PLAN §5.3).
var guardAllowed = map[string]string{
	filepath.Join("internal", "query"): "Phase 7: query/sql.go runs the dynamic filter SQL it builds, inside store.WithUser",
}

// TestNoDatabaseAccessOutsideStore fails when non-test code outside internal/store runs SQL on a
// pool or connection directly, or builds a sqlc querier (db.New) that would bypass WithUser. It
// parses the code instead of grepping, so comments and strings don't count.
func TestNoDatabaseAccessOutsideStore(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if d.IsDir() {
				if rel == filepath.Join("internal", "store") || strings.HasSuffix(rel, filepath.Join("httpapi", "gen")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if _, ok := guardAllowed[filepath.Dir(rel)]; ok {
				return nil
			}
			found++
			for _, v := range dbCalls(t, path) {
				t.Errorf("%s: %s — user data must go through store.WithUser (PLAN §5.3)", rel, v)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found < 10 {
		t.Fatalf("scanned only %d files; the walk is broken", found)
	}
}

func dbCalls(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		switch x, _ := sel.X.(*ast.Ident); {
		case dbCallNames[sel.Sel.Name]:
			out = append(out, posf(pos, sel.Sel.Name))
		case x != nil && x.Name == "db" && sel.Sel.Name == "New":
			out = append(out, posf(pos, "db.New"))
		}
		return true
	})
	return out
}

func posf(p token.Position, what string) string { return what + " at line " + strconv.Itoa(p.Line) }

// TestGuardDetects keeps the guard honest: it must flag the patterns it exists for.
func TestGuardDetects(t *testing.T) {
	dir := t.TempDir()
	src := `package x
func f() {
	pool.Query(ctx, "SELECT 1")
	c.pool.Exec(ctx, "DELETE FROM transactions")
	_ = db.New(pool)
	pgx.BeginFunc(ctx, pool, nil)
	// pool.Query in a comment is fine
	_ = "pool.Exec in a string is fine"
}`
	p := filepath.Join(dir, "x.go")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := dbCalls(t, p)
	want := []string{"Query at line 3", "Exec at line 4", "db.New at line 5", "BeginFunc at line 6"}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Errorf("dbCalls = %v, want %v", got, want)
	}
}
