package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The tables behind signing in and administration sit outside row-level security by design
// (sessions are resolved before the user is known, and so on), and the application's database role
// can write them. So nothing at the database layer tells the sign-in code from the rest of the
// process: a mistake anywhere that reached these writes could make an administrator, replace a
// password or start a session for someone else (register row DB-1). Until the rule lives in
// PostgreSQL (Backlog), this test keeps it in the source: the generated queries that write those
// tables may be called only from the packages listed here, each with its reason, and
// store.WithoutUser only from the packages that have work outside one user's scope.

// authTables are the tables the rule protects.
var authTables = []string{"users", "sessions", "access_tokens", "user_second_factors", "recovery_codes", "login_challenges",
	"instance_settings", "themes", "friend_codes"}

// authWriters are the packages (relative to backend/) that may call a query writing one of
// authTables, and which tables.
var authWriters = map[string]struct {
	tables []string
	why    string
}{
	"internal/auth":           {[]string{"users", "sessions", "access_tokens", "user_second_factors", "recovery_codes", "login_challenges"}, "sign-up, sign-in, sessions, tokens, two-step sign-in"},
	"internal/admin":          {[]string{"users", "sessions", "access_tokens", "instance_settings", "themes"}, "administration: accounts, roles, limits, instance settings, themes (ADR 0025)"},
	"internal/friends":        {[]string{"friend_codes"}, "friend codes (ADR 0027)"},
	"internal/assistant/eval": {[]string{"users"}, "the eval suite's demo user (make eval-assistant, never a server)"},
	"internal/assistant":      {[]string{"users"}, "one column of users: when a user's next digest is due (ADR 0033)"},
}

// withoutUserCallers are the packages that may open a transaction outside a user's scope.
var withoutUserCallers = map[string]string{
	"internal/auth":           "accounts and sessions exist before a user's scope does",
	"internal/admin":          "administration counts and manages accounts, never their data (ADR 0025)",
	"internal/advice":         "the advice library is the instance's, not a user's (ADR 0034)",
	"internal/assistant":      "the instance's connections and tuning (ADR 0035); the due digests of every user",
	"internal/assistant/eval": "the eval suite's demo user",
	"internal/friends":        "friend codes are looked up by their hash (ADR 0027)",
	"internal/ledger":         "expired activity log rows of every user",
	"internal/notify":         "old notifications of every user",
}

var (
	queryName  = regexp.MustCompile(`(?m)^-- name: (\w+) :\w+`)
	writesAuth = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(` + strings.Join(authTables, "|") + `)\b`)
)

// protectedQueries reads the sqlc query files: the name of every query that writes one of
// authTables, with the tables it writes.
func protectedQueries(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	files, err := filepath.Glob(filepath.Join("queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no query files: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // the repository's own files
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		heads := queryName.FindAllStringSubmatchIndex(src, -1)
		for i, h := range heads {
			end := len(src)
			if i+1 < len(heads) {
				end = heads[i+1][0]
			}
			for _, m := range writesAuth.FindAllStringSubmatch(src[h[0]:end], -1) {
				name := src[h[2]:h[3]]
				if table := strings.ToLower(m[1]); !contains(out[name], table) {
					out[name] = append(out[name], table)
				}
			}
		}
	}
	return out
}

// queryParams collects the names that functions of a file give to a parameter of type *db.Queries
// (by convention "q"): calls on those names are calls of generated queries.
func queryParams(file *ast.File) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		var params *ast.FieldList
		switch f := n.(type) {
		case *ast.FuncDecl:
			params = f.Type.Params
		case *ast.FuncLit:
			params = f.Type.Params
		default:
			return true
		}
		for _, field := range params.List {
			star, ok := field.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Queries" {
				for _, name := range field.Names {
					names[name.Name] = true
				}
			}
		}
		return true
	})
	return names
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestAuthTablesAreWrittenByTheirOwnersOnly(t *testing.T) {
	protected := protectedQueries(t)
	// the parser must have found the queries this rule is about
	for _, must := range []string{"SetUserAdmin", "SetUserPassword", "CreateSession", "CreateUser", "PutInstanceSettings", "StartSecondFactor", "TakeFriendCode"} {
		if _, ok := protected[must]; !ok {
			t.Errorf("%s is not among the protected queries (%d found): the test no longer sees the query files as it should", must, len(protected))
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seenWriters, seenWithout := map[string]bool{}, map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if d.IsDir() {
				if rel == filepath.Join("internal", "store") { // the store itself and the generated queries
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			pkg := filepath.ToSlash(filepath.Dir(rel))
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			queriers := queryParams(file) // the names of parameters of type *db.Queries
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				if name == "WithoutUser" {
					seenWithout[pkg] = true
					if _, ok := withoutUserCallers[pkg]; !ok {
						t.Errorf("%s calls store.WithoutUser: a package works outside a user's scope only for a reason listed in withoutUserCallers", rel)
					}
				}
				// a generated query: a method called on a *db.Queries value (services have methods of
				// the same names, which are not database writes)
				recv, isIdent := sel.X.(*ast.Ident)
				tables, isProtected := protected[name]
				if !isIdent || !isProtected || !queriers[recv.Name] {
					return true
				}
				seenWriters[pkg] = true
				allowed, known := authWriters[pkg]
				for _, table := range tables {
					if !known || !contains(allowed.tables, table) {
						t.Errorf("%s calls the query %s, which writes %s: only its owners may (authWriters)", rel, name, table)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// a list that names a package which no longer needs it has gone stale
	var stale []string
	for pkg := range authWriters {
		if !seenWriters[pkg] {
			stale = append(stale, pkg+" (authWriters)")
		}
	}
	for pkg := range withoutUserCallers {
		if !seenWithout[pkg] {
			stale = append(stale, pkg+" (withoutUserCallers)")
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("listed but no longer calling: %v", stale)
	}
}
