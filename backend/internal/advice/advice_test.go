package advice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

func TestQuery(t *testing.T) {
	for in, want := range map[string]string{
		"How should I start an emergency fund?":  "emergency:* | fund:*",
		"Jak oszczędzać na wakacje? Oszczędzać!": "jak:* | oszczedzac:* | wakacje:*",
		"Schulden zurückzahlen (Kreditkarte)":    "schulden:* | zuruckzahlen:* | kreditkarte:*",
		"a an to it":                             "",
		"Płatność: 50/30/20 rule":                "platnosc:* | rule:*",
	} {
		if got := Query(in); got != want {
			t.Errorf("Query(%q) = %q, want %q", in, got, want)
		}
	}
	if n := strings.Count(Query(strings.Repeat("word ", 3)+"alpha beta gamma delta epsilon zeta eta theta iota kappa lambda omicron sigma"), "|"); n != maxTerms-1 {
		t.Errorf("%d terms", n+1)
	}
}

func TestStarter(t *testing.T) {
	list, err := Starter()
	if err != nil || len(list) != 8 {
		t.Fatalf("%d articles, %v", len(list), err)
	}
	for _, a := range list {
		if a.Language != "en" || a.Status != Draft || len(a.Tags) == 0 || a.Title == "" ||
			!strings.HasSuffix(a.Body, "_General information, not financial advice: your situation may differ. For decisions with tax or legal consequences, ask a qualified adviser._") {
			t.Errorf("%s: %+v", a.Slug, a)
		}
	}
	if _, err := parseStarter("no front matter"); err == nil {
		t.Error("a file without front matter was read")
	}
}

type env struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	s   *Service
}

func newEnv(t *testing.T) *env {
	tdb := testutil.Require(t, testDB)
	if _, err := tdb.Owner.Exec(context.Background(), `DELETE FROM advice_articles`); err != nil { // an instance table
		t.Fatal(err)
	}
	st := store.New(tdb.App)
	return &env{t: t, ctx: context.Background(), st: st, s: New(st, platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)})}
}

func (e *env) user() uuid.UUID {
	e.t.Helper()
	id := platform.NewID()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) add(in Input) Article {
	e.t.Helper()
	a, err := e.s.Create(e.ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// TestSearch: published articles only, the title and tags weigh more than the body, the asked
// language first and any language when it has none, accents and case don't matter.
func TestSearch(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.add(Input{Slug: "emergency-fund", Language: "en", Title: "Building an emergency fund", Tags: []string{"saving"}, Status: Published,
		Body: "Keep three months of spending aside."})
	e.add(Input{Slug: "budgets", Language: "en", Title: "Budgets that work", Status: Published,
		Body: "A budget is not an emergency fund, but it helps you build one."})
	e.add(Input{Slug: "draft", Language: "en", Title: "Emergency fund draft", Status: Draft, Body: "Not published yet."})
	e.add(Input{Slug: "fundusz-awaryjny", Language: "pl", Title: "Fundusz awaryjny", Tags: []string{"oszczędzanie"}, Status: Published,
		Body: "Odłóż pieniądze na nieprzewidziane wydatki."})

	hits, err := e.s.Search(e.ctx, u, "How do I build an EMERGENCY fund?", "en", 0)
	if err != nil || len(hits) != 2 || hits[0].Slug != "emergency-fund" || hits[1].Slug != "budgets" {
		t.Fatalf("hits = %+v %v", hits, err)
	}
	if hits[0].Excerpt != "Keep three months of spending aside." || hits[0].Title != "Building an emergency fund" {
		t.Errorf("hit = %+v", hits[0])
	}
	// accents folded both ways; the Polish article first for a Polish question
	if hits, err := e.s.Search(e.ctx, u, "oszczedzanie na nieprzewidziane", "pl", 1); err != nil || len(hits) != 1 || hits[0].Slug != "fundusz-awaryjny" {
		t.Errorf("pl = %+v %v", hits, err)
	}
	// no German article: any language
	if hits, err := e.s.Search(e.ctx, u, "emergency fund", "de", 0); err != nil || len(hits) == 0 || hits[0].Language != "en" {
		t.Errorf("de = %+v %v", hits, err)
	}
	if hits, err := e.s.Search(e.ctx, u, "to be or not", "en", 0); err != nil || len(hits) != 0 {
		t.Errorf("nothing to search for = %+v %v", hits, err)
	}
	if hits, err := e.s.Search(e.ctx, u, "unpublished draft", "en", 0); err != nil || len(hits) != 0 {
		t.Errorf("draft found = %+v %v", hits, err)
	}
	// a long body is cut at a word
	long := strings.Repeat("saving money every month ", 80)
	e.add(Input{Slug: "long", Language: "en", Title: "Monthly saving", Status: Published, Body: long})
	if hits, _ := e.s.Search(e.ctx, u, "monthly saving", "en", 1); len(hits) != 1 || len([]rune(hits[0].Excerpt)) > ExcerptLen+2 ||
		!strings.HasSuffix(hits[0].Excerpt, " …") {
		t.Errorf("excerpt = %+v", hits)
	}
}

func TestArticles(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.add(Input{Slug: "goals", Language: "en", Title: " Saving for a goal ", Tags: []string{"goals", " goals", ""}, Body: "Text"})
	if a.Status != Draft || a.Title != "Saving for a goal" || len(a.Tags) != 1 || a.Version != 1 {
		t.Errorf("created = %+v", a)
	}
	if _, err := e.s.Published(e.ctx, u, "goals", "en"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("a draft was read: %v", err)
	}
	var ve *platform.ValidationError
	if _, err := e.s.Create(e.ctx, Input{Slug: "goals", Language: "en", Title: "Again"}); !errors.As(err, &ve) || !strings.Contains(err.Error(), "slug") {
		t.Errorf("duplicate slug: %v", err)
	}
	if _, err := e.s.Create(e.ctx, Input{Slug: "Bad Slug", Language: "english", Title: "", Status: "live"}); !errors.As(err, &ve) ||
		!strings.Contains(err.Error(), "language") || !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "title") {
		t.Errorf("invalid: %v", err)
	}
	// the same slug in another language is another article; a reader gets their language, else English
	e.add(Input{Slug: "goals", Language: "pl", Title: "Oszczędzanie na cel", Status: Published, Body: "Tekst"})
	pub, err := e.s.Update(e.ctx, a.ID, a.Version, Input{Slug: "goals", Language: "en", Title: "Saving for a goal", Body: "Text 2", Status: Published})
	if err != nil || pub.Version != 2 || pub.Body != "Text 2" {
		t.Fatalf("updated = %+v %v", pub, err)
	}
	if _, err := e.s.Update(e.ctx, a.ID, a.Version, Input{Slug: "goals", Language: "en", Title: "Stale"}); !errors.Is(err, platform.ErrStaleVersion) {
		t.Errorf("stale edit: %v", err)
	}
	if got, err := e.s.Published(e.ctx, u, "goals", "pl"); err != nil || got.Language != "pl" {
		t.Errorf("pl = %+v %v", got, err)
	}
	if got, err := e.s.Published(e.ctx, u, "goals", "de"); err != nil || got.Language != "en" {
		t.Errorf("de → en = %+v %v", got, err)
	}
	list, err := e.s.List(e.ctx)
	if err != nil || len(list) != 2 || list[0].Body != "" || list[0].BodyLength == 0 {
		t.Errorf("list = %+v %v", list, err)
	}
	if err := e.s.Delete(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Delete(e.ctx, a.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	if _, err := e.s.Update(e.ctx, a.ID, 2, Input{Slug: "goals", Language: "en", Title: "Gone"}); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("update of a deleted article: %v", err)
	}
}

func TestAddStarter(t *testing.T) {
	e := newEnv(t)
	e.add(Input{Slug: "emergency-fund", Language: "en", Title: "Our own emergency fund article", Status: Published})
	n, err := e.s.AddStarter(e.ctx)
	if err != nil || n != 7 {
		t.Fatalf("added %d, %v (one slug was taken)", n, err)
	}
	if n, err := e.s.AddStarter(e.ctx); err != nil || n != 0 {
		t.Errorf("again: %d %v", n, err)
	}
	list, _ := e.s.List(e.ctx)
	drafts := 0
	for _, a := range list {
		if a.Status == Draft {
			drafts++
		}
	}
	if len(list) != 8 || drafts != 7 {
		t.Errorf("%d articles, %d drafts", len(list), drafts)
	}
}
