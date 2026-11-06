package query

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

var testUser = uuid.MustParse("00000000-0000-0000-0000-0000000000aa")

func compile(t *testing.T, c Context) *Compiled {
	t.Helper()
	out, err := Compile(testUser, c)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func filter(col, cond string) Context {
	return Context{Filters: []Filter{{Column: col, Cond: cond}}}
}

// last returns the last predicate (the filter's) and its arguments after the user id.
func last(t *testing.T, c Context) (string, []any) {
	t.Helper()
	out := compile(t, c)
	return out.Where[len(out.Where)-1], out.Args[1:]
}

func TestCompileConditions(t *testing.T) {
	for _, tc := range []struct {
		col, cond string
		want      string // the predicate with placeholders
		args      []any
	}{
		{"amount", ">0", "((t.amount_minor::numeric / 100) > $2::numeric)", []any{"0"}},
		{"amount", "  >  1 000", "((t.amount_minor::numeric / 100) > $2::numeric)", []any{"1000"}},
		{"amount", "=-2 850,00", "((t.amount_minor::numeric / 100) = $2::numeric)", []any{"-2850.00"}},
		{"amount", "<-1.000,00", "((t.amount_minor::numeric / 100) < $2::numeric)", []any{"-1000.00"}},
		{"amount", "-50 .. -100", "((t.amount_minor::numeric / 100) BETWEEN LEAST($2::numeric, $3::numeric) AND GREATEST($2::numeric, $3::numeric))", []any{"-50", "-100"}},
		{"amount", "!>100", "NOT coalesce(((t.amount_minor::numeric / 100) > $2::numeric), false)", []any{"100"}},
		// Bare numbers, empty operators and unparsable operands fall back to text.
		{"amount", "79", "(coalesce(" + columns["amount"].Norm + ", '') LIKE $2)", []any{"%79%"}},
		{"amount", ">abc", "(coalesce(" + columns["amount"].Norm + ", '') LIKE $2)", []any{"%>abc%"}},
		{"amount", "=", "(coalesce(" + columns["amount"].Norm + ", '') = $2)", []any{""}},
		{"bookedOn", "2024", "(t.booked_on >= $2::date AND t.booked_on < $3::date)", []any{"2024-01-01", "2025-01-01"}},
		{"bookedOn", ">2024", "(t.booked_on >= $2::date)", []any{"2025-01-01"}},
		{"bookedOn", ">=2025-03-10", "(t.booked_on >= $2::date)", []any{"2025-03-10"}},
		{"bookedOn", "<2024-06", "(t.booked_on < $2::date)", []any{"2024-06-01"}},
		{"bookedOn", "<=2024-06", "(t.booked_on < $2::date)", []any{"2024-07-01"}},
		{"bookedOn", "03.2025", "(t.booked_on >= $2::date AND t.booked_on < $3::date)", []any{"2025-03-01", "2025-04-01"}},
		{"bookedOn", "2024-12..2024-01", "(t.booked_on >= $2::date AND t.booked_on < $3::date)", []any{"2024-01-01", "2025-01-01"}},
		{"bookedOn", ">garbage", "(coalesce(" + columns["bookedOn"].Norm + ", '') LIKE $2)", []any{"%>garbage%"}},
		{"payee", "KINO", "(coalesce(t.payee_n, '') LIKE $2)", []any{"%kino%"}},
		{"payee", "lipą", "(coalesce(t.payee_n, '') LIKE $2)", []any{"%lipa%"}},
		{"payee", "=KINO POD GWIAZDAMI   WARSZAWA", "(coalesce(t.payee_n, '') = $2)", []any{"kino pod gwiazdami warszawa"}},
		{"payee", "!=", "NOT coalesce((coalesce(t.payee_n, '') = $2), false)", []any{""}},
		{"payee", "!", "NOT coalesce((coalesce(t.payee_n, '') LIKE $2), false)", []any{"%%"}},
		{"payee", "a..b", "(coalesce(t.payee_n, '') LIKE $2)", []any{"%a..b%"}},
		{"category", "=a..b", "(coalesce(c.name_n, '') = $2)", []any{"a..b"}},
		{"payee", `50%_off\`, "(coalesce(t.payee_n, '') LIKE $2)", []any{`%50\%\_off\\%`}},
	} {
		got, args := last(t, filter(tc.col, tc.cond))
		if got != tc.want || !equalArgs(args, tc.args) {
			t.Errorf("%s %q:\n got  %s %v\n want %s %v", tc.col, tc.cond, got, args, tc.want, tc.args)
		}
	}
}

func equalArgs(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompileContext(t *testing.T) {
	acc := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	out := compile(t, Context{AccountIDs: []uuid.UUID{acc}, Search: "  Płatność   BLIK ",
		Filters: []Filter{{Column: "category", Excluded: []string{"Bez kategorii", ""}}, {Column: "payee", Cond: "   "}}})
	want := []string{"t.user_id = $1", "t.account_id = ANY($2::uuid[])", "NOT t.is_transfer",
		"t.search_text LIKE $3", "t.search_text LIKE $4", "coalesce(c.name, '') <> ALL($5::text[])"}
	if strings.Join(out.Where, " AND ") != strings.Join(want, " AND ") {
		t.Errorf("where = %q", out.Where)
	}
	if out.Args[2] != "%platnosc%" || out.Args[3] != "%blik%" || !out.Joins[JoinCategories] || out.Joins[JoinGroups] {
		t.Errorf("args %v joins %v", out.Args, out.Joins)
	}
	if strings.Join(out.Scope, " AND ") != "t.user_id = $1 AND t.account_id = ANY($2::uuid[])" {
		t.Errorf("scope = %q", out.Scope)
	}
	if out := compile(t, Context{IncludeTransfers: true}); len(out.Where) != 1 {
		t.Errorf("includeTransfers still filters: %q", out.Where)
	}
	_, err := Compile(testUser, Context{Filters: []Filter{{Column: "amount"}, {Column: "t.payee; DROP TABLE x"}}})
	var ve *platform.ValidationError
	if !asValidation(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != "context.filters[1].column" {
		t.Errorf("unknown column: %v", err)
	}
}

func asValidation(err error, ve **platform.ValidationError) bool {
	v, ok := err.(*platform.ValidationError) //nolint:errorlint // Compile returns it unwrapped
	*ve = v
	return ok
}

func TestToolFilter(t *testing.T) {
	from, to := platform.MustDate(2024, 6, 1), platform.MustDate(2024, 6, 30)
	minA, maxA, bad := "-100", "1e3", "abc"
	out := compile(t, Context{ToolFilter: &ToolFilter{Query: "kino warszawa", Payee: "KAWIARNIA  PODWÓRKO", Category: "rachunki",
		Group: "Wakacje", DateFrom: &from, DateTo: &to, Type: "expense", MinAmount: &minA, MaxAmount: &maxA}})
	got := strings.Join(out.Where[2:], " AND ")
	for _, want := range []string{"t.search_text LIKE $2", "t.search_text LIKE $3", "t.booked_on >= $4::date", "t.booked_on < $5::date",
		"t.amount_minor < 0", "abs((t.amount_minor::numeric / 100)) >= abs($6::numeric)", "abs((t.amount_minor::numeric / 100)) <= abs($7::numeric)",
		"t.payee_n LIKE $8", "coalesce(c.name_n, '') LIKE $9", "coalesce(g.name_n, '') LIKE $10"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if out.Args[4] != "2024-07-01" || out.Args[7] != "%kawiarnia podworko%" || !out.Joins[JoinGroups] {
		t.Errorf("args %v", out.Args)
	}
	out = compile(t, Context{ToolFilter: &ToolFilter{MinAmount: &bad, Type: "any"}})
	if out.Where[len(out.Where)-1] != "false" {
		t.Errorf("an unparsable amount must match nothing: %q", out.Where)
	}
	for in, want := range map[string]string{"2025-03-10T15:00:00": "2025-03-10", "2025-03-10": "2025-03-10", "garbage": "", "": ""} {
		d := ParseToolDate(in)
		if (d == nil) != (want == "") || (d != nil && d.String() != want) {
			t.Errorf("ParseToolDate(%q) = %v", in, d)
		}
	}
}

var hostile = []string{"'; DROP TABLE transactions; --", `\' OR 1=1 --`, "%", "_", `\`, "$1", "/* x */", "ąę'\"`;"}

// TestHostileInputOnlyInArgs: whatever the user types, the SQL text is built from the registry
// and placeholders only.
func TestHostileInputOnlyInArgs(t *testing.T) {
	for _, h := range hostile {
		for _, col := range ColumnNames() {
			for _, cond := range []string{h, "=" + h, ">" + h, h + ".." + h, "!" + h} {
				c := Context{Search: h, Filters: []Filter{{Column: col, Cond: cond, Excluded: []string{h}}},
					ToolFilter: &ToolFilter{Query: h, Payee: h, Category: h, Group: h, MinAmount: &h}}
				checkSQL(t, compile(t, c))
			}
		}
	}
}

func TestNULIsAValidationError(t *testing.T) {
	for _, c := range []Context{{Search: "a\x00"}, {Filters: []Filter{{Column: "payee", Cond: "\x00"}}},
		{Filters: []Filter{{Column: "payee", Excluded: []string{"x\x00"}}}}, {ToolFilter: &ToolFilter{Payee: "\x00"}}, {Search: "\xff"}} {
		_, err := Compile(testUser, c)
		var ve *platform.ValidationError
		if !asValidation(err, &ve) {
			t.Errorf("%+v: %v, want a validation error", c, err)
		}
	}
}

func FuzzCompile(f *testing.F) {
	for _, h := range hostile {
		f.Add("payee", h, h)
	}
	f.Add("amount", "-50..100", "")
	f.Add("bookedOn", ">=2025-03", "x y")
	f.Fuzz(func(t *testing.T, col, cond, search string) {
		out, err := Compile(testUser, Context{Search: search, Filters: []Filter{{Column: col, Cond: cond, Excluded: []string{cond}}},
			ToolFilter: &ToolFilter{Query: search, Payee: cond, MinAmount: &cond}})
		if err != nil {
			var ve *platform.ValidationError
			if !asValidation(err, &ve) {
				t.Fatalf("not a validation error: %v", err)
			}
			return // unknown column, or text with NUL / invalid UTF-8
		}
		checkSQL(t, out)
	})
}

var (
	placeholderRe = regexp.MustCompile(`\$(\d+)`)
	// The only string literals the compiler writes.
	ourLiterals = strings.NewReplacer("''", "", "'-'", "", "','", "", "'0'", "", "'DD.MM.YYYY'", "",
		// the source column (columns.go sourceText): constants of ours, never user input
		"'import'", "", "'Bank'", "", "'Assistant'", "", "'Manual'", "", "'transaction'", "")
)

func checkSQL(t *testing.T, c *Compiled) {
	t.Helper()
	sql := strings.Join(c.Where, " AND ")
	rest := ourLiterals.Replace(sql)
	for _, bad := range []string{"'", ";", "--", "/*", `\`, "\x00"} {
		if strings.Contains(rest, bad) {
			t.Fatalf("SQL contains %q: %s", bad, sql)
		}
	}
	for _, r := range sql {
		if r > 127 {
			t.Fatalf("non-ASCII in SQL: %s", sql)
		}
	}
	max := 0
	for _, m := range placeholderRe.FindAllStringSubmatch(sql, -1) {
		n := 0
		for _, d := range m[1] {
			n = n*10 + int(d-'0')
		}
		if n > max {
			max = n
		}
	}
	if max != len(c.Args) {
		t.Fatalf("%d placeholders, %d args: %s", max, len(c.Args), sql)
	}
}
